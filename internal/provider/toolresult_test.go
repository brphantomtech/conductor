package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/config"
)

// sequencedServer serves a list of SSE bodies in order, capturing each request
// body so a test can assert the wire shape of the follow-up turn.
func sequencedServer(t *testing.T, bodies ...[]byte) (*httptest.Server, *[][]byte) {
	t.Helper()
	var captured [][]byte
	idx := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		captured = append(captured, b)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		body := []byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
		if idx < len(bodies) {
			body = bodies[idx]
		}
		idx++
		_, _ = w.Write(body)
	}))
	return srv, &captured
}

func drain(stream TurnStream) {
	for range stream.Events() {
	}
}

func TestAnthropic_ContinueWithToolResults_SendsToolResultBlock(t *testing.T) {
	srv, captured := sequencedServer(t,
		loadFixture(t, "anthropic_tool_use.sse"),
		loadFixture(t, "anthropic_text.sse"),
	)
	defer srv.Close()

	a := newAnthropicTestAdapter(t, srv)
	ctx := context.Background()
	sess, err := a.CreateSession(ctx, a.cfg, "ws-1")
	require.NoError(t, err)

	// First turn: the model emits a tool_use call.
	stream, err := a.StartTurn(ctx, sess, "look it up", nil)
	require.NoError(t, err)
	drain(stream)

	// Continue with the tool result.
	res := []ToolResult{{
		CallID:  "toolu_01abcd",
		Name:    "lookup",
		Content: json.RawMessage(`{"hits":3}`),
	}}
	stream2, err := a.ContinueWithToolResults(ctx, sess, res)
	require.NoError(t, err)
	drain(stream2)

	require.Len(t, *captured, 2)
	var body2 struct {
		Messages []struct {
			Role    string `json:"role"`
			Content []struct {
				Type      string          `json:"type"`
				ToolUseID string          `json:"tool_use_id"`
				Content   json.RawMessage `json:"content"`
			} `json:"content"`
		} `json:"messages"`
	}
	require.NoError(t, json.Unmarshal((*captured)[1], &body2))
	// Last message must be a user message carrying a tool_result block.
	last := body2.Messages[len(body2.Messages)-1]
	require.Equal(t, "user", last.Role)
	require.Len(t, last.Content, 1)
	require.Equal(t, "tool_result", last.Content[0].Type)
	require.Equal(t, "toolu_01abcd", last.Content[0].ToolUseID)
	require.JSONEq(t, `{"hits":3}`, string(last.Content[0].Content))
}

func TestAnthropic_ContinueWithToolResults_NoPriorCallErrors(t *testing.T) {
	srv, _ := sequencedServer(t, loadFixture(t, "anthropic_text.sse"))
	defer srv.Close()

	a := newAnthropicTestAdapter(t, srv)
	ctx := context.Background()
	sess, err := a.CreateSession(ctx, a.cfg, "ws-1")
	require.NoError(t, err)

	// A plain text turn — no tool call emitted.
	stream, err := a.StartTurn(ctx, sess, "hi", nil)
	require.NoError(t, err)
	drain(stream)

	_, err = a.ContinueWithToolResults(ctx, sess, []ToolResult{{CallID: "x", Content: json.RawMessage(`"y"`)}})
	require.ErrorIs(t, err, ErrNoToolCall)
}

func newOpenAIToolTestAdapter(t *testing.T, srv *httptest.Server) *openaiAdapter {
	t.Helper()
	cfg := config.ProviderConfig{
		Provider:  KindOpenAI,
		Model:     "gpt-4o",
		APIKey:    "sk-test",
		MaxTokens: 1024,
		BaseURL:   srv.URL,
	}
	opt := defaultOptions()
	opt.httpClient = srv.Client()
	return newOpenAIAdapter(KindOpenAI, srv.URL, openaiBearerAuth, nil, cfg, opt)
}

func TestOpenAI_ContinueWithToolResults_SendsToolMessage(t *testing.T) {
	srv, captured := sequencedServer(t,
		loadFixture(t, "openai_tool_use.sse"),
		loadFixture(t, "openai_text.sse"),
	)
	defer srv.Close()

	a := newOpenAIToolTestAdapter(t, srv)
	ctx := context.Background()
	sess, err := a.CreateSession(ctx, a.cfg, "ws-1")
	require.NoError(t, err)

	stream, err := a.StartTurn(ctx, sess, "look it up", nil)
	require.NoError(t, err)
	var callID string
	for ev := range stream.Events() {
		if ev.Type == EventToolCall {
			callID = ev.ToolCall.ID
		}
	}
	require.NotEmpty(t, callID)

	res := []ToolResult{{CallID: callID, Name: "lookup", Content: json.RawMessage(`{"hits":3}`)}}
	stream2, err := a.ContinueWithToolResults(ctx, sess, res)
	require.NoError(t, err)
	drain(stream2)

	require.Len(t, *captured, 2)
	var body2 struct {
		Messages []struct {
			Role       string          `json:"role"`
			ToolCallID string          `json:"tool_call_id"`
			Content    json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	require.NoError(t, json.Unmarshal((*captured)[1], &body2))
	last := body2.Messages[len(body2.Messages)-1]
	require.Equal(t, "tool", last.Role)
	require.Equal(t, callID, last.ToolCallID)
	require.JSONEq(t, `{"hits":3}`, string(last.Content))
}

func TestOpenAI_ContinueWithToolResults_NoPriorCallErrors(t *testing.T) {
	srv, _ := sequencedServer(t, loadFixture(t, "openai_text.sse"))
	defer srv.Close()

	a := newOpenAIToolTestAdapter(t, srv)
	ctx := context.Background()
	sess, err := a.CreateSession(ctx, a.cfg, "ws-1")
	require.NoError(t, err)

	stream, err := a.StartTurn(ctx, sess, "hi", nil)
	require.NoError(t, err)
	drain(stream)

	_, err = a.ContinueWithToolResults(ctx, sess, []ToolResult{{CallID: "x", Content: json.RawMessage(`"y"`)}})
	require.ErrorIs(t, err, ErrNoToolCall)
}

// TestOpenRouter_SharesToolResultPath asserts the openrouter wrapper forwards to
// the shared OpenAI-compatible tool-result path.
func TestOpenRouter_SharesToolResultPath(t *testing.T) {
	srv, captured := sequencedServer(t,
		loadFixture(t, "openai_tool_use.sse"),
		loadFixture(t, "openai_text.sse"),
	)
	defer srv.Close()

	cfg := config.ProviderConfig{Provider: KindOpenRouter, Model: "x/y", APIKey: "sk", BaseURL: srv.URL}
	opt := defaultOptions()
	opt.httpClient = srv.Client()
	a := newOpenRouterAdapter(cfg, opt)

	ctx := context.Background()
	sess, err := a.CreateSession(ctx, cfg, "ws-1")
	require.NoError(t, err)
	stream, err := a.StartTurn(ctx, sess, "go", nil)
	require.NoError(t, err)
	var callID string
	for ev := range stream.Events() {
		if ev.Type == EventToolCall {
			callID = ev.ToolCall.ID
		}
	}
	require.NotEmpty(t, callID)

	stream2, err := a.ContinueWithToolResults(ctx, sess,
		[]ToolResult{{CallID: callID, Content: json.RawMessage(`{"ok":1}`)}})
	require.NoError(t, err)
	drain(stream2)
	require.Len(t, *captured, 2)
}
