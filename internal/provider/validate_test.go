package provider

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/config"
)

func TestValidate_CleanAnthropic(t *testing.T) {
	p := config.Providers{
		Default: config.ProviderConfig{
			Provider:  KindAnthropic,
			Model:     "claude-sonnet-4-6",
			APIKey:    "k",
			MaxTokens: 8192,
		},
	}
	require.NoError(t, Validate(p))
}

func TestValidate_MissingAPIKey(t *testing.T) {
	p := config.Providers{
		Default: config.ProviderConfig{
			Provider:  KindAnthropic,
			Model:     "x",
			MaxTokens: 1024,
		},
	}
	err := Validate(p)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrMissingAPIKey))
}

func TestValidate_UnsupportedProvider(t *testing.T) {
	p := config.Providers{
		Default: config.ProviderConfig{Provider: "made-up", Model: "x", APIKey: "k"},
	}
	err := Validate(p)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrUnsupportedProvider))
}

func TestValidate_CustomWithoutBaseURL(t *testing.T) {
	p := config.Providers{
		Default: config.ProviderConfig{Provider: KindCustom, Model: "x", APIKey: "k", MaxTokens: 1024},
	}
	err := Validate(p)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrUnsupportedProvider))
	require.Contains(t, err.Error(), "base_url")
}

func TestValidate_CustomWithBaseURLPasses(t *testing.T) {
	p := config.Providers{
		Default: config.ProviderConfig{Provider: KindCustom, Model: "x", APIKey: "k", BaseURL: "https://example.com/v1", MaxTokens: 1024},
	}
	require.NoError(t, Validate(p))
}

func TestValidate_OllamaLoopbackAllowsEmptyAPIKey(t *testing.T) {
	p := config.Providers{
		Default: config.ProviderConfig{Provider: KindOllama, Model: "llama3", MaxTokens: 1024},
	}
	require.NoError(t, Validate(p))
}

func TestValidate_NegativeMaxTokens(t *testing.T) {
	p := config.Providers{
		Default: config.ProviderConfig{Provider: KindAnthropic, Model: "x", APIKey: "k", MaxTokens: -1},
	}
	err := Validate(p)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrUnsupportedProvider))
	require.Contains(t, err.Error(), "max_tokens")
}

func TestValidate_InvalidCompactionStrategy(t *testing.T) {
	p := config.Providers{
		Default: config.ProviderConfig{
			Provider:           KindAnthropic,
			Model:              "x",
			APIKey:             "k",
			MaxTokens:          1024,
			CompactionStrategy: "magic",
		},
	}
	err := Validate(p)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrUnsupportedProvider))
	require.Contains(t, err.Error(), "compaction_strategy")
}

func TestValidate_RoleOverrideIsValidatedIndependently(t *testing.T) {
	p := config.Providers{
		Default: config.ProviderConfig{
			Provider:  KindAnthropic,
			Model:     "x",
			APIKey:    "k",
			MaxTokens: 1024,
		},
		Roles: map[string]config.ProviderConfig{
			"coder": {Provider: KindOpenAI, Model: "x", APIKey: "", MaxTokens: 1024},
		},
	}
	err := Validate(p)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrMissingAPIKey))
}

func TestValidate_MissingDefaultProviderKind(t *testing.T) {
	p := config.Providers{}
	err := Validate(p)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrUnsupportedProvider))
}
