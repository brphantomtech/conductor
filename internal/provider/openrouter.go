package provider

import (
	"context"
	"strings"

	"github.com/conductor-sh/conductor/internal/config"
)

// defaultOpenRouterBaseURL is the production endpoint specified in
// SPEC §7.2.1.
const defaultOpenRouterBaseURL = "https://openrouter.ai/api/v1"

// openRouterReferer is the value of the HTTP-Referer header OpenRouter
// requires per SPEC §7.2.1. It identifies the client to the gateway for
// usage analytics; the literal value is not security-sensitive.
const openRouterReferer = "https://conductor.dev"

// openRouterTitle is the X-Title header OpenRouter records for the
// integration. SPEC §7.2.1 hard-codes "Conductor".
const openRouterTitle = "Conductor"

// openrouterAdapter is a thin wrapper over openaiAdapter that points at
// the OpenRouter base URL and sets the two SPEC §7.2.1 headers on every
// request.
//
// All five Adapter methods forward straight through; there is no
// behavioral difference between this and the plain OpenAI adapter beyond
// the URL and headers.
type openrouterAdapter struct {
	inner *openaiAdapter
}

func newOpenRouterAdapter(cfg config.ProviderConfig, opt options) *openrouterAdapter {
	base := defaultOpenRouterBaseURL
	if cfg.BaseURL != "" {
		base = cfg.BaseURL
	}
	headers := map[string]string{
		"HTTP-Referer": openRouterReferer,
		"X-Title":      openRouterTitle,
	}
	// Allow overriding the referer via extra_params for operators who
	// want their own analytics label.
	if v, ok := cfg.ExtraParams["http_referer"].(string); ok && strings.TrimSpace(v) != "" {
		headers["HTTP-Referer"] = v
	}
	if v, ok := cfg.ExtraParams["x_title"].(string); ok && strings.TrimSpace(v) != "" {
		headers["X-Title"] = v
	}

	inner := newOpenAIAdapter(
		"openrouter",
		base,
		openaiBearerAuth,
		headers,
		cfg,
		opt,
	)
	return &openrouterAdapter{inner: inner}
}

// CreateSession forwards to the embedded openaiAdapter.
func (a *openrouterAdapter) CreateSession(ctx context.Context, cfg config.ProviderConfig, workspace string) (*Session, error) {
	return a.inner.CreateSession(ctx, cfg, workspace)
}

// StartTurn forwards to the embedded openaiAdapter.
func (a *openrouterAdapter) StartTurn(ctx context.Context, s *Session, prompt string, tools []ToolSpec) (TurnStream, error) {
	return a.inner.StartTurn(ctx, s, prompt, tools)
}

// ContinueTurn forwards to the embedded openaiAdapter.
func (a *openrouterAdapter) ContinueTurn(ctx context.Context, s *Session, prompt string) (TurnStream, error) {
	return a.inner.ContinueTurn(ctx, s, prompt)
}

// EndSession forwards to the embedded openaiAdapter.
func (a *openrouterAdapter) EndSession(ctx context.Context, s *Session) error {
	return a.inner.EndSession(ctx, s)
}

// GetUsage forwards to the embedded openaiAdapter.
func (a *openrouterAdapter) GetUsage(s *Session) TokenUsage {
	return a.inner.GetUsage(s)
}

// ensure compile-time conformance.
var _ Adapter = (*openrouterAdapter)(nil)
