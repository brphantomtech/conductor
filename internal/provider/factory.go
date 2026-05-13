package provider

import (
	"fmt"

	"github.com/conductor-sh/conductor/internal/config"
)

// Supported provider kinds. The list mirrors SPEC §7.2 and feeds both the
// New factory and Validate. Phase 3 wires three concrete adapters; the
// other three (ollama, lm_studio, custom) reuse the OpenAI implementation
// once Phase 13 adds their factory branches. Adding them here without an
// adapter would break the spec's "supported list" guarantee, so they are
// gated on a real implementation.
const (
	KindAnthropic  = "anthropic"
	KindOpenAI     = "openai"
	KindOpenRouter = "openrouter"
	KindOllama     = "ollama"
	KindLMStudio   = "lm_studio"
	KindCustom     = "custom"
)

// New constructs the concrete Adapter for the given provider kind.
// Returns ErrUnsupportedProvider for unknown values. The variadic opts
// override the default HTTP client, logger, and clock.
func New(cfg config.ProviderConfig, opts ...Option) (Adapter, error) {
	o := applyOptions(opts)
	switch cfg.Provider {
	case KindAnthropic:
		return newAnthropicAdapter(cfg, o), nil
	case KindOpenAI:
		return newOpenAIAdapter("openai", defaultOpenAIBaseURL, openaiBearerAuth, nil, cfg, o), nil
	case KindOpenRouter:
		return newOpenRouterAdapter(cfg, o), nil
	default:
		return nil, fmt.Errorf("provider: new %q: %w", cfg.Provider, ErrUnsupportedProvider)
	}
}

// compile-time assertions for the three Phase-3 adapters.
var (
	_ Adapter = (*anthropicAdapter)(nil)
	_ Adapter = (*openaiAdapter)(nil)
)
