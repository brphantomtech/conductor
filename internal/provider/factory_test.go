package provider

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/config"
)

func TestNew_ReturnsAnthropicAdapterForAnthropicKind(t *testing.T) {
	a, err := New(config.ProviderConfig{Provider: KindAnthropic, Model: "x", APIKey: "k", MaxTokens: 1024})
	require.NoError(t, err)
	require.NotNil(t, a)
	_, ok := a.(*anthropicAdapter)
	require.True(t, ok, "expected *anthropicAdapter, got %T", a)
}

func TestNew_ReturnsOpenAIAdapterForOpenAIKind(t *testing.T) {
	a, err := New(config.ProviderConfig{Provider: KindOpenAI, Model: "x", APIKey: "k", MaxTokens: 1024})
	require.NoError(t, err)
	_, ok := a.(*openaiAdapter)
	require.True(t, ok, "expected *openaiAdapter, got %T", a)
}

func TestNew_ReturnsOpenRouterAdapterForOpenRouterKind(t *testing.T) {
	a, err := New(config.ProviderConfig{Provider: KindOpenRouter, Model: "x", APIKey: "k", MaxTokens: 1024})
	require.NoError(t, err)
	_, ok := a.(*openrouterAdapter)
	require.True(t, ok, "expected *openrouterAdapter, got %T", a)
}

func TestNew_UnsupportedProviderIsReported(t *testing.T) {
	_, err := New(config.ProviderConfig{Provider: "vendor-from-the-future", Model: "x", APIKey: "k"})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrUnsupportedProvider))
}

func TestNew_OllamaIsNotWiredInPhase3(t *testing.T) {
	// Ollama is a valid config (Validate accepts it) but Phase 3 does not
	// yet construct an adapter for it — the factory should reject the
	// call so callers learn at construction time rather than at the first
	// turn.
	_, err := New(config.ProviderConfig{Provider: KindOllama, Model: "x"})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrUnsupportedProvider))
}
