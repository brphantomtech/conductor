package provider

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/config"
)

// fakeSummarizer records its inputs and returns a fixed summary so the
// summarize compaction path never makes a live call.
type fakeSummarizer struct {
	called     int
	transcript string
	summary    string
}

func (f *fakeSummarizer) summarize(_ context.Context, _ config.ProviderConfig, transcript string) (string, error) {
	f.called++
	f.transcript = transcript
	if f.summary == "" {
		return "SUMMARY", nil
	}
	return f.summary, nil
}

func newOpenAICompactionAdapter(t *testing.T, srv *httptest.Server, sum summarizer, mut func(*config.ProviderConfig)) *openaiAdapter {
	t.Helper()
	cfg := config.ProviderConfig{Provider: KindOpenAI, Model: "gpt-4o", APIKey: "sk-test", MaxTokens: 1024}
	if mut != nil {
		mut(&cfg)
	}
	opt := defaultOptions()
	opt.httpClient = srv.Client()
	opt.summarizer = sum
	return newOpenAIAdapter("openai", srv.URL, openaiBearerAuth, nil, cfg, opt)
}

func TestOpenAI_SummarizeCompactsAndRestartsContext(t *testing.T) {
	body := loadFixture(t, "openai_high_usage.sse")
	srv := httptest.NewServer(sseHandler(body, 200))
	defer srv.Close()

	sum := &fakeSummarizer{summary: "compacted summary"}
	a := newOpenAICompactionAdapter(t, srv, sum, func(cfg *config.ProviderConfig) {
		cfg.ContextBudget = 900 // usage 900 → 100% ≥ 95%
		cfg.CompactionStrategy = compactionSummarize
	})
	ctx := context.Background()

	sess, err := a.CreateSession(ctx, a.cfg, "ws-1")
	require.NoError(t, err)
	stream, err := a.StartTurn(ctx, sess, "use the tokens", nil)
	require.NoError(t, err)

	saw := 0
	for ev := range stream.Events() {
		if ev.Type == EventContextCompacted {
			saw++
		}
	}
	_ = stream.Wait()
	require.Equal(t, 1, saw, "summarize must emit ContextCompacted once at 95%")
	require.Equal(t, 1, sum.called)

	// Context restarted with the summary as the single system message.
	payload := sess.loadPayload().(*openaiSession)
	require.Len(t, payload.messages, 1)
	require.Equal(t, "system", payload.messages[0].Role)
	require.Contains(t, string(payload.messages[0].Content), "compacted summary")
	// Usage reset toward ~60% of the budget.
	require.Less(t, payload.usage.Total, 900)
}

func TestOpenAI_SlidingWindowDropsOldestPairs(t *testing.T) {
	body := loadFixture(t, "openai_high_usage.sse")
	srv := httptest.NewServer(sseHandler(body, 200))
	defer srv.Close()

	a := newOpenAICompactionAdapter(t, srv, &fakeSummarizer{}, func(cfg *config.ProviderConfig) {
		cfg.ContextBudget = 900
		cfg.CompactionStrategy = compactionSlidingWindow
	})
	ctx := context.Background()

	sess, err := a.CreateSession(ctx, a.cfg, "ws-1")
	require.NoError(t, err)

	// Pre-load several user/assistant messages so the window has something
	// to drop.
	payload := sess.loadPayload().(*openaiSession)
	for i := 0; i < 10; i++ {
		payload.messages = append(payload.messages, openaiMessage{Role: "user", Content: []byte(`"old"`)})
	}
	before := len(payload.messages)

	stream, err := a.StartTurn(ctx, sess, "trigger", nil)
	require.NoError(t, err)
	saw := 0
	for ev := range stream.Events() {
		if ev.Type == EventContextSlid {
			saw++
		}
	}
	_ = stream.Wait()
	require.Equal(t, 1, saw, "sliding_window must emit ContextSlid once at 95%")
	require.Less(t, len(payload.messages), before, "oldest pairs dropped")
}

func TestOpenAI_NoneStrategyEmitsLimitApproaching(t *testing.T) {
	body := loadFixture(t, "openai_high_usage.sse")
	srv := httptest.NewServer(sseHandler(body, 200))
	defer srv.Close()

	a := newOpenAICompactionAdapter(t, srv, &fakeSummarizer{}, func(cfg *config.ProviderConfig) {
		cfg.ContextBudget = 900
		cfg.CompactionStrategy = compactionNone
	})
	ctx := context.Background()
	sess, err := a.CreateSession(ctx, a.cfg, "ws-1")
	require.NoError(t, err)
	stream, err := a.StartTurn(ctx, sess, "trigger", nil)
	require.NoError(t, err)
	saw := 0
	for ev := range stream.Events() {
		if ev.Type == EventContextLimitApproaching {
			saw++
		}
	}
	_ = stream.Wait()
	require.Equal(t, 1, saw)
}

func TestOpenAI_NoCompactionWhenBudgetZero(t *testing.T) {
	body := loadFixture(t, "openai_high_usage.sse")
	srv := httptest.NewServer(sseHandler(body, 200))
	defer srv.Close()

	sum := &fakeSummarizer{}
	a := newOpenAICompactionAdapter(t, srv, sum, nil) // budget 0
	ctx := context.Background()
	sess, err := a.CreateSession(ctx, a.cfg, "ws-1")
	require.NoError(t, err)
	stream, err := a.StartTurn(ctx, sess, "hi", nil)
	require.NoError(t, err)
	for ev := range stream.Events() {
		require.NotContains(t, []EventType{EventContextCompacted, EventContextSlid, EventContextLimitApproaching}, ev.Type)
	}
	_ = stream.Wait()
	require.Equal(t, 0, sum.called)
}

func TestAnthropic_SummarizeCompacts(t *testing.T) {
	body := loadFixture(t, "anthropic_high_usage.sse")
	srv := httptest.NewServer(sseHandler(body, 200))
	defer srv.Close()

	sum := &fakeSummarizer{summary: "anthropic summary"}
	cfg := config.ProviderConfig{Provider: KindAnthropic, Model: "claude", APIKey: "sk-test", BaseURL: srv.URL, MaxTokens: 1024, ContextBudget: 900, CompactionStrategy: compactionSummarize}
	opt := defaultOptions()
	opt.httpClient = srv.Client()
	opt.summarizer = sum
	a := newAnthropicAdapter(cfg, opt)
	ctx := context.Background()

	sess, err := a.CreateSession(ctx, cfg, "ws-1")
	require.NoError(t, err)
	stream, err := a.StartTurn(ctx, sess, "use tokens", nil)
	require.NoError(t, err)
	saw := 0
	for ev := range stream.Events() {
		if ev.Type == EventContextCompacted {
			saw++
		}
	}
	_ = stream.Wait()
	require.Equal(t, 1, saw)
	require.Equal(t, 1, sum.called)
	payload := sess.loadPayload().(*anthropicSession)
	require.Equal(t, "anthropic summary", payload.system)
	require.Empty(t, payload.messages)
}

func TestShouldCompactThreshold(t *testing.T) {
	require.False(t, shouldCompact(0, TokenUsage{Total: 10000}))
	require.False(t, shouldCompact(1000, TokenUsage{Total: 940})) // 94%
	require.True(t, shouldCompact(1000, TokenUsage{Total: 950}))  // 95%
	require.True(t, shouldCompact(1000, TokenUsage{Total: 1200})) // over
}
