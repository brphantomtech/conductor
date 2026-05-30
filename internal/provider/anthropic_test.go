package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/config"
)

func newAnthropicTestAdapter(t *testing.T, srv *httptest.Server, cfgMutators ...func(*config.ProviderConfig)) *anthropicAdapter {
	t.Helper()
	cfg := config.ProviderConfig{
		Provider:  KindAnthropic,
		Model:     "claude-sonnet-4-6",
		APIKey:    "sk-test",
		MaxTokens: 1024,
		BaseURL:   srv.URL,
	}
	for _, m := range cfgMutators {
		m(&cfg)
	}
	opt := defaultOptions()
	opt.httpClient = srv.Client()
	return newAnthropicAdapter(cfg, opt)
}

func TestAnthropic_StreamsAssistantText(t *testing.T) {
	body := loadFixture(t, "anthropic_text.sse")
	var capturedHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeader = r.Header.Get("anthropic-version")
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	a := newAnthropicTestAdapter(t, srv)
	ctx := context.Background()

	sess, err := a.CreateSession(ctx, a.cfg, "ws-1")
	require.NoError(t, err)

	stream, err := a.StartTurn(ctx, sess, "hi", nil)
	require.NoError(t, err)

	deltas := 0
	sawEnd := false
	for ev := range stream.Events() {
		switch ev.Type {
		case EventAssistantDelta:
			deltas++
		case EventTurnEnd:
			sawEnd = true
		}
	}
	res := stream.Wait()
	require.NoError(t, res.Err)
	require.Equal(t, "Hello world", res.Text)
	require.Equal(t, 3, deltas)
	require.True(t, sawEnd)
	require.Equal(t, anthropicAPIVersion, capturedHeader)

	// Anthropic streams "input_tokens" only in message_start; subsequent
	// message_delta carries the *delta* output_tokens. Our adapter sums
	// the input on message_start and the delta output_tokens.
	usage := a.GetUsage(sess)
	require.Equal(t, 12, usage.Prompt)
	require.Equal(t, 19, usage.Completion) // 1 from start + 18 from delta
}

func TestAnthropic_StreamsToolCall(t *testing.T) {
	body := loadFixture(t, "anthropic_tool_use.sse")
	srv := httptest.NewServer(sseHandler(body, 200))
	defer srv.Close()
	a := newAnthropicTestAdapter(t, srv)
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
	require.Equal(t, "toolu_01abcd", res.ToolCalls[0].ID)
	require.Equal(t, "lookup", res.ToolCalls[0].Name)
	require.JSONEq(t, `{"query":"docs"}`, string(res.ToolCalls[0].Arguments))
}

func TestAnthropic_ContextWarningAt80Pct(t *testing.T) {
	body := loadFixture(t, "anthropic_high_usage.sse")
	srv := httptest.NewServer(sseHandler(body, 200))
	defer srv.Close()
	a := newAnthropicTestAdapter(t, srv, func(cfg *config.ProviderConfig) {
		cfg.ContextBudget = 1000
	})
	ctx := context.Background()
	sess, err := a.CreateSession(ctx, a.cfg, "ws-1")
	require.NoError(t, err)
	stream, err := a.StartTurn(ctx, sess, "hi", nil)
	require.NoError(t, err)
	saw := 0
	for ev := range stream.Events() {
		if ev.Type == EventContextWarning {
			saw++
		}
	}
	_ = stream.Wait()
	require.Equal(t, 1, saw)
}

func TestAnthropic_HTTP401IsErrRequestFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"error":{"type":"authentication_error","message":"invalid x-api-key"}}`))
	}))
	defer srv.Close()
	a := newAnthropicTestAdapter(t, srv)
	ctx := context.Background()
	sess, err := a.CreateSession(ctx, a.cfg, "ws-1")
	require.NoError(t, err)
	_, err = a.StartTurn(ctx, sess, "hi", nil)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrRequestFailed)
	require.Contains(t, err.Error(), "authentication_error")
}

func TestAnthropic_MalformedSSESurfaceErrStreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("event: content_block_delta\ndata: this is not json\n\n"))
	}))
	defer srv.Close()
	a := newAnthropicTestAdapter(t, srv)
	ctx := context.Background()
	sess, err := a.CreateSession(ctx, a.cfg, "ws-1")
	require.NoError(t, err)
	stream, err := a.StartTurn(ctx, sess, "hi", nil)
	require.NoError(t, err)
	for range stream.Events() {
	}
	res := stream.Wait()
	require.Error(t, res.Err)
	require.ErrorIs(t, res.Err, ErrStreamError)
}

func TestAnthropic_AdapterIsAdapter(t *testing.T) {
	// Compile-time-style assertion through a function value.
	var a Adapter = &anthropicAdapter{}
	require.NotNil(t, a)
}
