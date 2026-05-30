package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/rs/zerolog"

	"github.com/conductor-sh/conductor/internal/config"
)

// defaultOpenAIBaseURL is the production endpoint. The openrouter adapter
// overrides it to https://openrouter.ai/api/v1.
const defaultOpenAIBaseURL = "https://api.openai.com/v1"

// openaiAdapter implements Adapter for the OpenAI Chat Completions API
// and any wire-compatible derivative (OpenRouter, Ollama's /v1 endpoint,
// LM Studio, vLLM, …). Variations between the derivatives live in the
// `baseURL`, `authHeader`, and `extraHeaders` fields rather than in
// separate types.
type openaiAdapter struct {
	cfg          config.ProviderConfig
	http         *http.Client
	log          zerolog.Logger
	providerName string
	baseURL      string
	authHeader   func(req *http.Request, apiKey string)
	extraHeaders map[string]string
}

// openaiSession is the per-session state carried as Session.payload.
type openaiSession struct {
	messages []openaiMessage
	usage    TokenUsage
	warned   bool
}

// openaiMessage mirrors the wire schema. content is left as json.RawMessage
// so we can hold either a string or a structured tool_result array.
type openaiMessage struct {
	Role       string             `json:"role"`
	Content    json.RawMessage    `json:"content,omitempty"`
	Name       string             `json:"name,omitempty"`
	ToolCalls  []openaiToolCall   `json:"tool_calls,omitempty"`
	ToolCallID string             `json:"tool_call_id,omitempty"`
}

type openaiToolCall struct {
	ID       string             `json:"id"`
	Type     string             `json:"type"`
	Function openaiToolFunction `json:"function"`
}

type openaiToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// newOpenAIAdapter is the package-internal constructor every flavour
// (openai, openrouter, …) routes through.
func newOpenAIAdapter(
	providerName string,
	baseURL string,
	authHeader func(*http.Request, string),
	extraHeaders map[string]string,
	cfg config.ProviderConfig,
	opt options,
) *openaiAdapter {
	if baseURL == "" {
		baseURL = defaultOpenAIBaseURL
	}
	if cfg.BaseURL != "" {
		baseURL = cfg.BaseURL
	}
	return &openaiAdapter{
		cfg:          cfg,
		http:         opt.httpClient,
		log:          opt.logger,
		providerName: providerName,
		baseURL:      strings.TrimRight(baseURL, "/"),
		authHeader:   authHeader,
		extraHeaders: extraHeaders,
	}
}

// CreateSession satisfies Adapter.CreateSession. The api_key check fires
// here so callers see ErrMissingAPIKey before the first turn, mirroring
// SPEC §6.4 startup behaviour for late-bound callers.
func (a *openaiAdapter) CreateSession(_ context.Context, cfg config.ProviderConfig, workspace string) (*Session, error) {
	if requiresAPIKey(a.baseURL) && cfg.APIKey == "" {
		return nil, fmt.Errorf("provider: %s: create session: %w", a.providerName, ErrMissingAPIKey)
	}
	payload := &openaiSession{}
	return newSession(a.providerName, workspace, payload), nil
}

// StartTurn satisfies Adapter.StartTurn.
func (a *openaiAdapter) StartTurn(ctx context.Context, s *Session, prompt string, tools []ToolSpec) (TurnStream, error) {
	return a.turn(ctx, s, prompt, tools)
}

// ContinueTurn satisfies Adapter.ContinueTurn.
func (a *openaiAdapter) ContinueTurn(ctx context.Context, s *Session, prompt string) (TurnStream, error) {
	return a.turn(ctx, s, prompt, nil)
}

// EndSession satisfies Adapter.EndSession.
func (a *openaiAdapter) EndSession(_ context.Context, s *Session) error {
	if s == nil {
		return nil
	}
	s.markClosed()
	return nil
}

// GetUsage satisfies Adapter.GetUsage.
func (a *openaiAdapter) GetUsage(s *Session) TokenUsage {
	if s == nil {
		return TokenUsage{}
	}
	payload, ok := s.loadPayload().(*openaiSession)
	if !ok || payload == nil {
		return TokenUsage{}
	}
	return payload.usage
}

// turn is the shared turn entry point. It validates session state, builds
// the request body, opens the HTTP stream, and hands the body to a
// background goroutine that publishes AgentEvents on the returned stream.
func (a *openaiAdapter) turn(ctx context.Context, s *Session, prompt string, tools []ToolSpec) (TurnStream, error) {
	if s == nil {
		return nil, fmt.Errorf("provider: %s: turn: nil session: %w", a.providerName, ErrSessionClosed)
	}
	if s.isClosed() {
		return nil, fmt.Errorf("provider: %s: turn: %w", a.providerName, ErrSessionClosed)
	}
	if err := ctx.Err(); err != nil {
		return nil, wrapTimeout(a.providerName, "turn", err)
	}

	payload, ok := s.loadPayload().(*openaiSession)
	if !ok || payload == nil {
		return nil, fmt.Errorf("provider: %s: turn: session payload missing: %w", a.providerName, ErrSessionClosed)
	}

	userContent, _ := json.Marshal(prompt)
	payload.messages = append(payload.messages, openaiMessage{
		Role:    "user",
		Content: userContent,
	})

	body, err := buildOpenAIBody(a.cfg, payload.messages, tools)
	if err != nil {
		return nil, fmt.Errorf("provider: %s: marshal body: %w", a.providerName, err)
	}

	url := a.baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, wrapRequest(a.providerName, "build request", err)
	}
	setJSONHeaders(req)
	if a.authHeader != nil {
		a.authHeader(req, a.cfg.APIKey)
	}
	for k, v := range a.extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := a.http.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return nil, wrapTimeout(a.providerName, "do request", err)
		}
		return nil, wrapRequest(a.providerName, "do request", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, readErrorBody(a.providerName, "do request", resp)
	}

	stream := newBufferedStream()
	go a.pumpStream(ctx, payload, stream, resp.Body)
	return stream, nil
}

// pumpStream owns the lifetime of the SSE reader and translates frames
// into AgentEvents on the stream's channel. It closes the stream on
// completion or error.
func (a *openaiAdapter) pumpStream(ctx context.Context, sess *openaiSession, stream *bufferedStream, body io.ReadCloser) {
	defer stream.close()

	reader := newSSEReader(body)
	defer func() { _ = reader.Close() }()

	stream.emit(AgentEvent{Type: EventAssistantStart})

	var (
		assistantContent strings.Builder
		toolBuilders     = newOpenAIToolAccumulator()
		turnUsage        TokenUsage
		emittedEnd       bool
	)

	for {
		frame, err := reader.Next(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				if !emittedEnd {
					stream.emit(AgentEvent{Type: EventTurnEnd})
				}
				break
			}
			stream.emit(AgentEvent{Type: EventError, Err: err})
			return
		}

		// OpenAI uses unnamed frames; the `[DONE]` sentinel terminates.
		if strings.TrimSpace(frame.Data) == "[DONE]" {
			stream.emit(AgentEvent{Type: EventTurnEnd})
			emittedEnd = true
			continue
		}

		chunk, err := parseOpenAIChunk([]byte(frame.Data))
		if err != nil {
			stream.emit(AgentEvent{Type: EventError, Err: wrapStream(a.providerName, "decode chunk", err)})
			return
		}

		for _, ch := range chunk.Choices {
			if ch.Delta.Content != "" {
				assistantContent.WriteString(ch.Delta.Content)
				stream.emit(AgentEvent{Type: EventAssistantDelta, Text: ch.Delta.Content})
			}
			for _, tc := range ch.Delta.ToolCalls {
				toolBuilders.add(tc)
			}
			if ch.FinishReason != "" {
				for _, call := range toolBuilders.flush() {
					stream.emit(AgentEvent{Type: EventToolCall, ToolCall: call})
				}
			}
		}

		if chunk.Usage != nil {
			turnUsage = TokenUsage{
				Prompt:     chunk.Usage.PromptTokens,
				Completion: chunk.Usage.CompletionTokens,
				Total:      chunk.Usage.TotalTokens,
			}
		}
	}

	// Drain any tool calls that finished without a finish_reason flush.
	for _, call := range toolBuilders.flush() {
		stream.emit(AgentEvent{Type: EventToolCall, ToolCall: call})
	}

	if assistantContent.Len() > 0 || len(toolBuilders.calls) > 0 {
		ac := openaiMessage{Role: "assistant"}
		if assistantContent.Len() > 0 {
			b, _ := json.Marshal(assistantContent.String())
			ac.Content = b
		}
		for _, call := range toolBuilders.completed {
			ac.ToolCalls = append(ac.ToolCalls, openaiToolCall{
				ID:   call.ID,
				Type: "function",
				Function: openaiToolFunction{
					Name:      call.Name,
					Arguments: string(call.Arguments),
				},
			})
		}
		sess.messages = append(sess.messages, ac)
	}

	if turnUsage.Total > 0 || turnUsage.Prompt > 0 || turnUsage.Completion > 0 {
		sess.usage.Add(turnUsage)
		stream.emit(AgentEvent{Type: EventUsage, Usage: turnUsage})

		if !sess.warned && shouldWarnContext(a.cfg.ContextBudget, sess.usage) {
			sess.warned = true
			stream.emit(AgentEvent{Type: EventContextWarning, Usage: sess.usage})
		}
	}
}

// requiresAPIKey reports whether the resolved base URL points at a
// remote provider that needs auth. Loopback addresses (Ollama, LM Studio
// in default config) do not.
func requiresAPIKey(baseURL string) bool {
	lc := strings.ToLower(baseURL)
	if strings.HasPrefix(lc, "http://localhost") ||
		strings.HasPrefix(lc, "http://127.0.0.1") ||
		strings.HasPrefix(lc, "http://[::1]") ||
		strings.HasPrefix(lc, "https://localhost") ||
		strings.HasPrefix(lc, "https://127.0.0.1") ||
		strings.HasPrefix(lc, "https://[::1]") {
		return false
	}
	return true
}

// shouldWarnContext reports whether the cumulative usage has crossed 80%
// of the configured budget. Budget 0 means "no budget" — never warn.
func shouldWarnContext(budget int, usage TokenUsage) bool {
	if budget <= 0 {
		return false
	}
	return usage.Total*100 >= budget*80
}

// buildOpenAIBody assembles the chat-completions request body.
func buildOpenAIBody(cfg config.ProviderConfig, messages []openaiMessage, tools []ToolSpec) ([]byte, error) {
	body := map[string]any{
		"model":          cfg.Model,
		"messages":       messages,
		"stream":         true,
		"stream_options": map[string]any{"include_usage": true},
	}
	if cfg.MaxTokens > 0 {
		body["max_tokens"] = cfg.MaxTokens
	}
	if cfg.Temperature != nil {
		body["temperature"] = *cfg.Temperature
	}
	if len(tools) > 0 {
		body["tools"] = openaiToolsPayload(tools)
	}
	for k, v := range cfg.ExtraParams {
		if _, exists := body[k]; !exists {
			body[k] = v
		}
	}

	out, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal body: %w", err)
	}
	return out, nil
}

func openaiToolsPayload(tools []ToolSpec) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		params := t.Parameters
		if len(params) == 0 {
			params = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  params,
			},
		})
	}
	return out
}

// openaiChunk is the streaming chunk schema.
type openaiChunk struct {
	Choices []openaiChunkChoice `json:"choices"`
	Usage   *openaiChunkUsage   `json:"usage"`
}

type openaiChunkChoice struct {
	Index        int                `json:"index"`
	Delta        openaiChunkDelta   `json:"delta"`
	FinishReason string             `json:"finish_reason"`
}

type openaiChunkDelta struct {
	Role      string                `json:"role"`
	Content   string                `json:"content"`
	ToolCalls []openaiDeltaToolCall `json:"tool_calls"`
}

type openaiDeltaToolCall struct {
	Index    int                      `json:"index"`
	ID       string                   `json:"id"`
	Type     string                   `json:"type"`
	Function *openaiDeltaToolFunction `json:"function"`
}

type openaiDeltaToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openaiChunkUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func parseOpenAIChunk(data []byte) (openaiChunk, error) {
	var c openaiChunk
	if err := json.Unmarshal(data, &c); err != nil {
		return openaiChunk{}, fmt.Errorf("unmarshal chunk: %w", err)
	}
	return c, nil
}

// openaiToolAccumulator reassembles tool_calls deltas. OpenAI streams the
// arguments field as concatenated fragments under the same `index`; the
// accumulator gathers them into a single ToolCall.
type openaiToolAccumulator struct {
	calls     map[int]*ToolCall
	order     []int
	completed []ToolCall
}

func newOpenAIToolAccumulator() *openaiToolAccumulator {
	return &openaiToolAccumulator{calls: map[int]*ToolCall{}}
}

func (a *openaiToolAccumulator) add(delta openaiDeltaToolCall) {
	c, ok := a.calls[delta.Index]
	if !ok {
		c = &ToolCall{Arguments: json.RawMessage{}}
		a.calls[delta.Index] = c
		a.order = append(a.order, delta.Index)
	}
	if delta.ID != "" {
		c.ID = delta.ID
	}
	if delta.Function != nil {
		if delta.Function.Name != "" {
			c.Name = delta.Function.Name
		}
		if delta.Function.Arguments != "" {
			c.Arguments = append(c.Arguments, []byte(delta.Function.Arguments)...)
		}
	}
}

func (a *openaiToolAccumulator) flush() []ToolCall {
	out := make([]ToolCall, 0, len(a.order))
	for _, i := range a.order {
		call := *a.calls[i]
		if len(call.Arguments) == 0 {
			call.Arguments = json.RawMessage("{}")
		}
		out = append(out, call)
		a.completed = append(a.completed, call)
	}
	a.calls = map[int]*ToolCall{}
	a.order = nil
	return out
}

// openaiBearerAuth is the default auth-header injector. Stand-alone so
// the openrouter adapter can compose it without a struct method.
func openaiBearerAuth(req *http.Request, apiKey string) {
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
}
