package provider

import (
	"errors"
	"fmt"

	"github.com/conductor-sh/conductor/internal/config"
)

// supportedKinds is the SPEC §7.2 + §4.1.3 union of allowed
// providers.<role>.provider values. Phase 3 only ships adapters for the
// first three; the other three are valid in config and reuse the OpenAI
// adapter once Phase 13 wires their factory branches.
var supportedKinds = map[string]struct{}{
	KindAnthropic:  {},
	KindOpenAI:     {},
	KindOpenRouter: {},
	KindOllama:     {},
	KindLMStudio:   {},
	KindCustom:     {},
}

// supportedCompactions is the SPEC §4.1.3 enum for compaction_strategy.
// Phase 9 implements the non-`none` variants; Phase 3 validates the
// string but does not act on it.
var supportedCompactions = map[string]struct{}{
	"":               {}, // empty means "default" — fall through to defaults.go
	"none":           {},
	"summarize":      {},
	"sliding_window": {},
}

// Validate enforces SPEC §6.4 startup rules for the providers block.
// Returns errors.Join over every problem so the harness CLI can show all
// failures at once.
func Validate(p config.Providers) error {
	var errs []error

	if err := validateOne("default", p.Default); err != nil {
		errs = append(errs, err)
	}
	for role, cfg := range p.Roles {
		if err := validateOne(role, cfg); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// validateOne checks a single ProviderConfig. The label is "default" or
// the role name; it is included in the error message so operators can
// pinpoint the offending block in HARNESS.md.
func validateOne(label string, cfg config.ProviderConfig) error {
	var errs []error

	if cfg.Provider == "" {
		errs = append(errs, fmt.Errorf("provider: %s: provider is required: %w",
			label, ErrUnsupportedProvider))
	} else if _, ok := supportedKinds[cfg.Provider]; !ok {
		errs = append(errs, fmt.Errorf("provider: %s: unsupported provider %q: %w",
			label, cfg.Provider, ErrUnsupportedProvider))
	}

	if cfg.Provider == KindCustom && cfg.BaseURL == "" {
		errs = append(errs, fmt.Errorf("provider: %s: custom requires base_url: %w",
			label, ErrUnsupportedProvider))
	}

	if cfg.Provider != "" && cfg.Provider != KindCustom {
		// Only check api_key for kinds that have a known default endpoint.
		if needsAPIKey(cfg) && cfg.APIKey == "" {
			errs = append(errs, fmt.Errorf("provider: %s: api_key is required for %s: %w",
				label, cfg.Provider, ErrMissingAPIKey))
		}
	}

	if cfg.MaxTokens < 0 {
		errs = append(errs, fmt.Errorf("provider: %s: max_tokens must be non-negative, got %d: %w",
			label, cfg.MaxTokens, ErrUnsupportedProvider))
	}

	if _, ok := supportedCompactions[cfg.CompactionStrategy]; !ok {
		errs = append(errs, fmt.Errorf("provider: %s: unsupported compaction_strategy %q: %w",
			label, cfg.CompactionStrategy, ErrUnsupportedProvider))
	}

	return errors.Join(errs...)
}

// needsAPIKey reports whether the resolved endpoint for this config
// requires authentication. Loopback URLs (the Ollama / LM Studio default
// hosts) do not.
func needsAPIKey(cfg config.ProviderConfig) bool {
	base := cfg.BaseURL
	if base == "" {
		switch cfg.Provider {
		case KindOllama:
			base = "http://localhost:11434"
		case KindLMStudio:
			base = "http://localhost:1234/v1"
		}
	}
	if base == "" {
		// Remote providers without an explicit override (anthropic /
		// openai / openrouter) — always need a key.
		return true
	}
	return requiresAPIKey(base)
}
