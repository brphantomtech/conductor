package provider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/config"
)

// loadFixture reads a recorded SSE / JSON fixture from testdata.
func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	p := filepath.Join("testdata", name)
	b, err := os.ReadFile(p)
	require.NoError(t, err)
	return b
}

// sseHandler serves the given fixture body as a streaming SSE response.
// It writes the whole body in one go — httptest's transport flushes
// automatically when the handler returns, which the SSE reader handles
// the same as a real streaming response.
func sseHandler(body []byte, status int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}
}

// newOpenAITestAdapter constructs an openaiAdapter pointed at the given
// httptest server with deterministic defaults.
func newOpenAITestAdapter(t *testing.T, srv *httptest.Server, cfgMutators ...func(*config.ProviderConfig)) *openaiAdapter {
	t.Helper()
	cfg := config.ProviderConfig{
		Provider:  KindOpenAI,
		Model:     "gpt-4o",
		APIKey:    "sk-test",
		MaxTokens: 1024,
	}
	for _, m := range cfgMutators {
		m(&cfg)
	}
	opt := defaultOptions()
	opt.httpClient = srv.Client()
	return newOpenAIAdapter("openai", srv.URL, openaiBearerAuth, nil, cfg, opt)
}

func TestOpenAI_StreamsAssistantText(t *testing.T) {
	body := loadFixture(t, "openai_text.sse")
	srv := httptest.NewServer(sseHandler(body, 200))
	defer srv.Close()

	a := newOpenAITestAdapter(t, srv)
	ctx := context.Background()

	sess, err := a.CreateSession(ctx, a.cfg, "ws-1")
	require.NoError(t, err)

	stream, err := a.StartTurn(ctx, sess, "hi", nil)
	require.NoError(t, err)

	var events []EventType
	for ev := range stream.Events() {
		events = append(events, ev.Type)
	}
	res := stream.Wait()
	require.NoError(t, res.Err)
	require.Equal(t, "Hello world", res.Text)
	require.Equal(t, TokenUsage{Prompt: 10, Completion: 20, Total: 30}, res.Usage)
	require.Equal(t, TokenUsage{Prompt: 10, Completion: 20, Total: 30}, a.GetUsage(sess))

	require.Contains(t, events, EventAssistantStart)
	require.Contains(t, events, EventAssistantDelta)
	require.Contains(t, events, EventTurnEnd)
}

func TestOpenAI_StreamsToolCalls(t *testing.T) {
	body := loadFixture(t, "openai_tool_use.sse")
	srv := httptest.NewServer(sseHandler(body, 200))
	defer srv.Close()

	a := newOpenAITestAdapter(t, srv)
	ctx := context.Background()

	sess, err := a.CreateSession(ctx, a.cfg, "ws-1")
	require.NoError(t, err)
	stream, err := a.StartTurn(ctx, sess, "use a tool", nil)
	require.NoError(t, err)

	for range stream.Events() {
	}
	res := stream.Wait()
	require.NoError(t, res.Err)
	require.Len(t, res.ToolCalls, 1)
	require.Equal(t, "call_abc", res.ToolCalls[0].ID)
	require.Equal(t, "lookup", res.ToolCalls[0].Name)
	require.JSONEq(t, `{"query":"docs"}`, string(res.ToolCalls[0].Arguments))
}

func TestOpenAI_CumulativeUsageAcrossTurns(t *testing.T) {
	body := loadFixture(t, "openai_text.sse")
	srv := httptest.NewServer(sseHandler(body, 200))
	defer srv.Close()

	a := newOpenAITestAdapter(t, srv)
	ctx := context.Background()
	sess, err := a.CreateSession(ctx, a.cfg, "ws-1")
	require.NoError(t, err)

	for i := 0; i < 2; i++ {
		stream, err := a.StartTurn(ctx, sess, "hi", nil)
		require.NoError(t, err)
		for range stream.Events() {
		}
		_ = stream.Wait()
	}

	require.Equal(t, TokenUsage{Prompt: 20, Completion: 40, Total: 60}, a.GetUsage(sess))
}

func TestOpenAI_ContextWarningAt80Pct(t *testing.T) {
	body := loadFixture(t, "openai_high_usage.sse")
	srv := httptest.NewServer(sseHandler(body, 200))
	defer srv.Close()

	a := newOpenAITestAdapter(t, srv, func(cfg *config.ProviderConfig) {
		cfg.ContextBudget = 1000
	})
	ctx := context.Background()

	sess, err := a.CreateSession(ctx, a.cfg, "ws-1")
	require.NoError(t, err)

	stream, err := a.StartTurn(ctx, sess, "use the tokens", nil)
	require.NoError(t, err)

	saw := 0
	for ev := range stream.Events() {
		if ev.Type == EventContextWarning {
			saw++
		}
	}
	_ = stream.Wait()
	require.Equal(t, 1, saw, "warning must fire exactly once when budget is crossed")

	// Second turn must not re-emit.
	srv2 := httptest.NewServer(sseHandler(body, 200))
	defer srv2.Close()
	a.baseURL = srv2.URL

	stream2, err := a.StartTurn(ctx, sess, "more", nil)
	require.NoError(t, err)
	saw = 0
	for ev := range stream2.Events() {
		if ev.Type == EventContextWarning {
			saw++
		}
	}
	require.Equal(t, 0, saw)
}

func TestOpenAI_NoWarningWhenBudgetIsZero(t *testing.T) {
	body := loadFixture(t, "openai_high_usage.sse")
	srv := httptest.NewServer(sseHandler(body, 200))
	defer srv.Close()

	a := newOpenAITestAdapter(t, srv) // budget defaults to 0
	ctx := context.Background()

	sess, err := a.CreateSession(ctx, a.cfg, "ws-1")
	require.NoError(t, err)
	stream, err := a.StartTurn(ctx, sess, "hi", nil)
	require.NoError(t, err)
	for ev := range stream.Events() {
		require.NotEqual(t, EventContextWarning, ev.Type)
	}
	_ = stream.Wait()
}

func TestOpenAI_HTTP401IsErrRequestFailed(t *testing.T) {
	body := loadFixture(t, "openai_error_401.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(401)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	a := newOpenAITestAdapter(t, srv)
	ctx := context.Background()

	sess, err := a.CreateSession(ctx, a.cfg, "ws-1")
	require.NoError(t, err)

	_, err = a.StartTurn(ctx, sess, "hi", nil)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrRequestFailed))
	require.Contains(t, err.Error(), "invalid_api_key")
}

func TestOpenAI_CreateSessionRejectsEmptyAPIKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	defer srv.Close()
	// override the base URL so it's no longer loopback.
	cfg := config.ProviderConfig{Provider: KindOpenAI, Model: "x", APIKey: ""}
	opt := defaultOptions()
	opt.httpClient = srv.Client()
	a := newOpenAIAdapter("openai", "https://api.openai.com/v1", openaiBearerAuth, nil, cfg, opt)
	_, err := a.CreateSession(context.Background(), cfg, "ws-1")
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrMissingAPIKey))
}

func TestOpenAI_SessionRejectsTurnAfterClose(t *testing.T) {
	srv := httptest.NewServer(sseHandler(loadFixture(t, "openai_text.sse"), 200))
	defer srv.Close()
	a := newOpenAITestAdapter(t, srv)
	ctx := context.Background()
	sess, err := a.CreateSession(ctx, a.cfg, "ws-1")
	require.NoError(t, err)
	require.NoError(t, a.EndSession(ctx, sess))
	_, err = a.StartTurn(ctx, sess, "hi", nil)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrSessionClosed))
}

func TestOpenAI_CancelledContextSurfaceErrResponseTimeout(t *testing.T) {
	srv := httptest.NewServer(sseHandler(loadFixture(t, "openai_text.sse"), 200))
	defer srv.Close()
	a := newOpenAITestAdapter(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	sess, err := a.CreateSession(ctx, a.cfg, "ws-1")
	require.NoError(t, err)
	cancel()
	_, err = a.StartTurn(ctx, sess, "hi", nil)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrResponseTimeout))
}

// TestOpenAI_DeadlineSurfaceErrResponseTimeout exercises the timeout path
// via a real (very short) deadline rather than a pre-cancelled context.
func TestOpenAI_DeadlineSurfaceErrResponseTimeout(t *testing.T) {
	// Handler that intentionally sleeps past the deadline.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	a := newOpenAITestAdapter(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	sess, err := a.CreateSession(context.Background(), a.cfg, "ws-1")
	require.NoError(t, err)
	_, err = a.StartTurn(ctx, sess, "hi", nil)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrResponseTimeout), "got %v", err)
}
