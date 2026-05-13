package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/config"
)

func TestOpenRouter_SetsRequiredHeaders(t *testing.T) {
	body := loadFixture(t, "openai_text.sse")

	var capturedReferer, capturedTitle, capturedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReferer = r.Header.Get("HTTP-Referer")
		capturedTitle = r.Header.Get("X-Title")
		capturedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	cfg := config.ProviderConfig{
		Provider:  KindOpenRouter,
		Model:     "anthropic/claude-sonnet-4.6",
		APIKey:    "or-test",
		BaseURL:   srv.URL,
		MaxTokens: 1024,
	}
	opt := defaultOptions()
	opt.httpClient = srv.Client()
	a := newOpenRouterAdapter(cfg, opt)

	ctx := context.Background()
	sess, err := a.CreateSession(ctx, cfg, "ws-1")
	require.NoError(t, err)
	stream, err := a.StartTurn(ctx, sess, "hi", nil)
	require.NoError(t, err)
	for range stream.Events() {
	}
	_ = stream.Wait()

	require.Equal(t, openRouterTitle, capturedTitle)
	require.NotEmpty(t, capturedReferer)
	require.Equal(t, "Bearer or-test", capturedAuth)
}

func TestOpenRouter_HeadersAreOverridable(t *testing.T) {
	body := loadFixture(t, "openai_text.sse")
	var capturedReferer, capturedTitle string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReferer = r.Header.Get("HTTP-Referer")
		capturedTitle = r.Header.Get("X-Title")
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	cfg := config.ProviderConfig{
		Provider:  KindOpenRouter,
		Model:     "m",
		APIKey:    "or-test",
		BaseURL:   srv.URL,
		MaxTokens: 1024,
		ExtraParams: map[string]any{
			"http_referer": "https://example.dev",
			"x_title":      "Acme",
		},
	}
	opt := defaultOptions()
	opt.httpClient = srv.Client()
	a := newOpenRouterAdapter(cfg, opt)

	ctx := context.Background()
	sess, err := a.CreateSession(ctx, cfg, "ws-1")
	require.NoError(t, err)
	stream, err := a.StartTurn(ctx, sess, "hi", nil)
	require.NoError(t, err)
	for range stream.Events() {
	}
	_ = stream.Wait()

	require.Equal(t, "https://example.dev", capturedReferer)
	require.Equal(t, "Acme", capturedTitle)
}

func TestOpenRouter_ImplementsAdapter(t *testing.T) {
	var _ Adapter = (*openrouterAdapter)(nil)
}
