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

// defaultAnthropicBaseURL is the Messages API endpoint.
const defaultAnthropicBaseURL = "https://api.anthropic.com/v1"

// anthropicAPIVersion is the version header SPEC §7.2.2 requires.
const anthropicAPIVersion = "2023-06-01"

// anthropicAdapter implements Adapter for the Anthropic Messages API.
type anthropicAdapter struct {
	cfg     config.ProviderConfig
	http    *http.Client
	log     zerolog.Logger
	baseURL string
}

// anthropicSession holds the per-session message history and accumulated
// usage. Anthropic's messages payload is structurally distinct from
// OpenAI's so we model it explicitly.
type anthropicSession struct {
	messages []anthropicMessage
	usage    TokenUsage
	warned   bool
}

// anthropicMessage is one user/assistant turn. content is a polymorphic
// list of typed blocks.
type anthropicMessage struct {
	Role    string             `json:"role"`
	Content []anthropicContent `json:"content"`
}

// anthropicContent mirrors the polymorphic content_block shape. Tests
// only need the text and tool_use variants in Phase 3; tool_result is
// reserved for Phase 13.
type anthropicContent struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
}

func newAnthropicAdapter(cfg config.ProviderConfig, opt options) *anthropicAdapter {
	base := defaultAnthropicBaseURL
	if cfg.BaseURL != "" {
		base = cfg.BaseURL
	}
	return &anthropicAdapter{
		cfg:     cfg,
		http:    opt.httpClient,
		log:     opt.logger,
		baseURL: strings.TrimRight(base, "/"),
	}
}

// CreateSession satisfies Adapter.CreateSession.
func (a *anthropicAdapter) CreateSession(_ context.Context, cfg config.ProviderConfig, workspace string) (*Session, error) {
	if requiresAPIKey(a.baseURL) && cfg.APIKey == "" {
		return nil, fmt.Errorf("provider: anthropic: create session: %w", ErrMissingAPIKey)
	}
	return newSession("anthropic", workspace, &anthropicSession{}), nil
}

// StartTurn satisfies Adapter.StartTurn.
func (a *anthropicAdapter) StartTurn(ctx context.Context, s *Session, prompt string, tools []ToolSpec) (TurnStream, error) {
	return a.turn(ctx, s, prompt, tools)
}

// ContinueTurn satisfies Adapter.ContinueTurn.
func (a *anthropicAdapter) ContinueTurn(ctx context.Context, s *Session, prompt string) (TurnStream, error) {
	return a.turn(ctx, s, prompt, nil)
}

// EndSession satisfies Adapter.EndSession.
func (a *anthropicAdapter) EndSession(_ context.Context, s *Session) error {
	if s == nil {
		return nil
	}
	s.markClosed()
	return nil
}

// GetUsage satisfies Adapter.GetUsage.
func (a *anthropicAdapter) GetUsage(s *Session) TokenUsage {
	if s == nil {
		return TokenUsage{}
	}
	payload, ok := s.loadPayload().(*anthropicSession)
	if !ok || payload == nil {
		return TokenUsage{}
	}
	return payload.usage
}

func (a *anthropicAdapter) turn(ctx context.Context, s *Session, prompt string, tools []ToolSpec) (TurnStream, error) {
	if s == nil {
		return nil, fmt.Errorf("provider: anthropic: turn: nil session: %w", ErrSessionClosed)
	}
	if s.isClosed() {
		return nil, fmt.Errorf("provider: anthropic: turn: %w", ErrSessionClosed)
	}
	if err := ctx.Err(); err != nil {
		return nil, wrapTimeout("anthropic", "turn", err)
	}

	payload, ok := s.loadPayload().(*anthropicSession)
	if !ok || payload == nil {
		return nil, fmt.Errorf("provider: anthropic: turn: session payload missing: %w", ErrSessionClosed)
	}

	payload.messages = append(payload.messages, anthropicMessage{
		Role: "user",
		Content: []anthropicContent{
			{Type: "text", Text: prompt},
		},
	})

	body, err := buildAnthropicBody(a.cfg, payload.messages, tools)
	if err != nil {
		return nil, fmt.Errorf("provider: anthropic: marshal body: %w", err)
	}

	url := a.baseURL + "/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, wrapRequest("anthropic", "build request", err)
	}
	setJSONHeaders(req)
	req.Header.Set("x-api-key", a.cfg.APIKey)
	req.Header.Set("anthropic-version", anthropicAPIVersion)

	resp, err := a.http.Do(req)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return nil, wrapTimeout("anthropic", "do request", err)
		}
		return nil, wrapRequest("anthropic", "do request", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, readErrorBody("anthropic", "do request", resp)
	}

	stream := newBufferedStream()
	go a.pumpStream(ctx, payload, stream, resp.Body)
	return stream, nil
}

// pumpStream owns the SSE reader and translates Anthropic event frames
// into AgentEvents.
func (a *anthropicAdapter) pumpStream(ctx context.Context, sess *anthropicSession, stream *bufferedStream, body io.ReadCloser) {
	defer stream.close()

	reader := newSSEReader(body)
	defer func() { _ = reader.Close() }()

	stream.emit(AgentEvent{Type: EventAssistantStart})

	var (
		assistantBlocks []anthropicContent
		textBlocks      []strings.Builder
		toolUses        []*anthropicToolBuilder
		turnUsage       TokenUsage
		seenEnd         bool
	)

	for {
		frame, err := reader.Next(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				if !seenEnd {
					stream.emit(AgentEvent{Type: EventTurnEnd})
				}
				break
			}
			stream.emit(AgentEvent{Type: EventError, Err: err})
			return
		}

		switch frame.Event {
		case "message_start":
			var msg struct {
				Message struct {
					Usage anthropicUsagePayload `json:"usage"`
				} `json:"message"`
			}
			if err := json.Unmarshal([]byte(frame.Data), &msg); err != nil {
				stream.emit(AgentEvent{Type: EventError, Err: wrapStream("anthropic", "decode message_start", err)})
				return
			}
			turnUsage.Prompt += msg.Message.Usage.InputTokens
			turnUsage.Completion += msg.Message.Usage.OutputTokens

		case "content_block_start":
			var bs struct {
				Index        int `json:"index"`
				ContentBlock struct {
					Type  string          `json:"type"`
					Text  string          `json:"text"`
					ID    string          `json:"id"`
					Name  string          `json:"name"`
					Input json.RawMessage `json:"input"`
				} `json:"content_block"`
			}
			if err := json.Unmarshal([]byte(frame.Data), &bs); err != nil {
				stream.emit(AgentEvent{Type: EventError, Err: wrapStream("anthropic", "decode content_block_start", err)})
				return
			}
			growTo(&textBlocks, bs.Index+1)
			growTo2(&toolUses, bs.Index+1)
			switch bs.ContentBlock.Type {
			case "text":
				textBlocks[bs.Index].WriteString(bs.ContentBlock.Text)
			case "tool_use":
				toolUses[bs.Index] = &anthropicToolBuilder{
					id:   bs.ContentBlock.ID,
					name: bs.ContentBlock.Name,
				}
				// Anthropic always emits an empty `{}` placeholder on
				// content_block_start; the real JSON arrives via
				// input_json_delta. We only seed the buffer when the
				// initial input is non-empty (which only happens for
				// non-streaming responses we may see in tests).
				raw := string(bs.ContentBlock.Input)
				if raw != "" && raw != "null" && raw != "{}" {
					toolUses[bs.Index].input.Write(bs.ContentBlock.Input)
				}
			}

		case "content_block_delta":
			var bd struct {
				Index int `json:"index"`
				Delta struct {
					Type        string `json:"type"`
					Text        string `json:"text"`
					PartialJSON string `json:"partial_json"`
				} `json:"delta"`
			}
			if err := json.Unmarshal([]byte(frame.Data), &bd); err != nil {
				stream.emit(AgentEvent{Type: EventError, Err: wrapStream("anthropic", "decode content_block_delta", err)})
				return
			}
			growTo(&textBlocks, bd.Index+1)
			growTo2(&toolUses, bd.Index+1)
			switch bd.Delta.Type {
			case "text_delta":
				textBlocks[bd.Index].WriteString(bd.Delta.Text)
				stream.emit(AgentEvent{Type: EventAssistantDelta, Text: bd.Delta.Text})
			case "input_json_delta":
				if toolUses[bd.Index] != nil {
					toolUses[bd.Index].input.WriteString(bd.Delta.PartialJSON)
				}
			}

		case "content_block_stop":
			var bs struct {
				Index int `json:"index"`
			}
			if err := json.Unmarshal([]byte(frame.Data), &bs); err != nil {
				stream.emit(AgentEvent{Type: EventError, Err: wrapStream("anthropic", "decode content_block_stop", err)})
				return
			}
			if bs.Index < len(toolUses) && toolUses[bs.Index] != nil {
				tb := toolUses[bs.Index]
				args := json.RawMessage(tb.input.Bytes())
				if len(args) == 0 {
					args = json.RawMessage("{}")
				}
				call := ToolCall{ID: tb.id, Name: tb.name, Arguments: args}
				stream.emit(AgentEvent{Type: EventToolCall, ToolCall: call})
				assistantBlocks = append(assistantBlocks, anthropicContent{
					Type:  "tool_use",
					ID:    tb.id,
					Name:  tb.name,
					Input: append(json.RawMessage{}, args...),
				})
			} else if bs.Index < len(textBlocks) && textBlocks[bs.Index].Len() > 0 {
				assistantBlocks = append(assistantBlocks, anthropicContent{
					Type: "text",
					Text: textBlocks[bs.Index].String(),
				})
			}

		case "message_delta":
			var md struct {
				Usage anthropicUsagePayload `json:"usage"`
			}
			if err := json.Unmarshal([]byte(frame.Data), &md); err != nil {
				stream.emit(AgentEvent{Type: EventError, Err: wrapStream("anthropic", "decode message_delta", err)})
				return
			}
			turnUsage.Completion += md.Usage.OutputTokens

		case "message_stop":
			stream.emit(AgentEvent{Type: EventTurnEnd})
			seenEnd = true

		case "ping", "":
			// Anthropic sends periodic `ping` events; ignore.

		case "error":
			var ev struct {
				Error struct {
					Type    string `json:"type"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(frame.Data), &ev); err != nil {
				stream.emit(AgentEvent{Type: EventError, Err: wrapStream("anthropic", "decode error event", err)})
				return
			}
			stream.emit(AgentEvent{
				Type: EventError,
				Err: fmt.Errorf("provider: anthropic: stream error: %s: %s: %w",
					ev.Error.Type, ev.Error.Message, ErrStreamError),
			})
			return
		}
	}

	if len(assistantBlocks) > 0 {
		sess.messages = append(sess.messages, anthropicMessage{
			Role:    "assistant",
			Content: assistantBlocks,
		})
	}

	if turnUsage.Prompt > 0 || turnUsage.Completion > 0 {
		turnUsage.Total = turnUsage.Prompt + turnUsage.Completion
		sess.usage.Add(turnUsage)
		stream.emit(AgentEvent{Type: EventUsage, Usage: turnUsage})

		if !sess.warned && shouldWarnContext(a.cfg.ContextBudget, sess.usage) {
			sess.warned = true
			stream.emit(AgentEvent{Type: EventContextWarning, Usage: sess.usage})
		}
	}
}

// anthropicToolBuilder accumulates a tool_use block's input JSON across
// `input_json_delta` frames.
type anthropicToolBuilder struct {
	id    string
	name  string
	input bytes.Buffer
}

// anthropicUsagePayload mirrors Anthropic's per-event usage fragments.
type anthropicUsagePayload struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

func buildAnthropicBody(cfg config.ProviderConfig, messages []anthropicMessage, tools []ToolSpec) ([]byte, error) {
	body := map[string]any{
		"model":      cfg.Model,
		"messages":   messages,
		"max_tokens": cfg.MaxTokens,
		"stream":     true,
	}
	if cfg.MaxTokens == 0 {
		body["max_tokens"] = 1024
	}
	if cfg.Temperature != nil {
		body["temperature"] = *cfg.Temperature
	}
	if len(tools) > 0 {
		body["tools"] = anthropicToolsPayload(tools)
	}
	if sys, ok := cfg.ExtraParams["system"].(string); ok && sys != "" {
		body["system"] = sys
	}
	for k, v := range cfg.ExtraParams {
		if k == "system" {
			continue
		}
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

func anthropicToolsPayload(tools []ToolSpec) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		schema := t.Parameters
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out = append(out, map[string]any{
			"name":         t.Name,
			"description":  t.Description,
			"input_schema": schema,
		})
	}
	return out
}

// growTo extends s to length n by zero-value-appending.
func growTo(s *[]strings.Builder, n int) {
	for len(*s) < n {
		*s = append(*s, strings.Builder{})
	}
}

func growTo2(s *[]*anthropicToolBuilder, n int) {
	for len(*s) < n {
		*s = append(*s, nil)
	}
}
