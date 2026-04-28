package config

import (
	"errors"
	"strings"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

func TestLoad_Defaults(t *testing.T) {
	t.Parallel()
	cfg, err := Load(LoadOptions{})
	require.NoError(t, err)

	// Spot-check the defaults that downstream phases rely on.
	require.Equal(t, 30_000, cfg.Polling.IntervalMS)
	require.Equal(t, 8080, cfg.Server.Port)
	require.Equal(t, "127.0.0.1", cfg.Server.Host)
	require.True(t, cfg.Server.EnableAPI)
	require.True(t, cfg.Server.EnableDashboard)
	require.Equal(t, []string{"planner", "coder", "verifier"}, cfg.Routing.Pipeline)
	require.Equal(t, "sqlite_vec", cfg.Knowledge.StoreBackend)
	require.Equal(t, "sqlite", cfg.Memory.StoreBackend)
}

func TestLoad_FrontMatterOverridesDefaults(t *testing.T) {
	t.Parallel()
	cfg, err := Load(LoadOptions{
		FrontMatter: map[string]any{
			"project": map[string]any{
				"id":   "demo",
				"name": "Demo Project",
			},
			"polling": map[string]any{
				"interval_ms": 5000,
			},
			"server": map[string]any{
				"port": 9090,
			},
		},
	})
	require.NoError(t, err)
	require.Equal(t, "demo", cfg.Project.ID)
	require.Equal(t, 5000, cfg.Polling.IntervalMS)
	require.Equal(t, 9090, cfg.Server.Port)
	require.Equal(t, "127.0.0.1", cfg.Server.Host, "untouched default survives")
}

func TestLoad_EnvOverridesFrontMatter(t *testing.T) {
	t.Setenv("CONDUCTOR_SERVER_PORT", "7777")

	cfg, err := Load(LoadOptions{
		FrontMatter: map[string]any{
			"server": map[string]any{"port": 9090},
		},
	})
	require.NoError(t, err)
	require.Equal(t, 7777, cfg.Server.Port,
		"env var must override front matter (SPEC §6.1)")
}

func TestLoad_FlagOverridesEnv(t *testing.T) {
	t.Setenv("CONDUCTOR_SERVER_PORT", "7777")

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.Int("port", 0, "server port override")
	require.NoError(t, flags.Parse([]string{"--port", "5050"}))

	cfg, err := Load(LoadOptions{
		Flags: flags,
		FrontMatter: map[string]any{
			"server": map[string]any{"port": 9090},
		},
	})
	require.NoError(t, err)
	require.Equal(t, 5050, cfg.Server.Port,
		"CLI flag must override env (SPEC §6.1)")
}

func TestLoad_ExpandsTrackerAPIKey(t *testing.T) {
	t.Setenv("LINEAR_TOKEN", "secret-token")

	cfg, err := Load(LoadOptions{
		FrontMatter: map[string]any{
			"tracker": map[string]any{
				"kind":    "linear",
				"api_key": "$LINEAR_TOKEN",
			},
		},
	})
	require.NoError(t, err)
	require.Equal(t, "secret-token", cfg.Tracker.APIKey)
}

func TestLoad_UnsetVariableFails(t *testing.T) {
	_, err := Load(LoadOptions{
		FrontMatter: map[string]any{
			"tracker": map[string]any{
				"api_key": "$NOT_A_REAL_VAR_AT_ALL",
			},
		},
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrUnsetVariable),
		"missing $VAR must surface as ErrUnsetVariable, got %v", err)
}

func TestValidate_ReportsAllRequiredFields(t *testing.T) {
	t.Parallel()
	cfg := Defaults() // missing project/tracker/providers
	err := Validate(cfg)
	require.Error(t, err)

	msg := err.Error()
	for _, want := range []string{
		"project.id",
		"tracker.kind",
		"tracker.api_key",
		"providers.default.provider",
	} {
		require.True(t, strings.Contains(msg, want),
			"validation error must mention %q (got: %s)", want, msg)
	}
}

func TestValidate_AcceptsCompleteConfig(t *testing.T) {
	t.Parallel()
	cfg := Defaults()
	cfg.Project.ID = "demo"
	cfg.Tracker.Kind = "linear"
	cfg.Tracker.APIKey = "secret"
	cfg.Tracker.ProjectSlug = "demo-team"
	cfg.Providers.Default.Provider = "openrouter"
	cfg.Providers.Default.APIKey = "sk-test"

	require.NoError(t, Validate(cfg))
}

func TestValidate_LocalProviderDoesNotRequireKey(t *testing.T) {
	t.Parallel()
	cfg := Defaults()
	cfg.Project.ID = "demo"
	cfg.Tracker.Kind = "linear"
	cfg.Tracker.APIKey = "secret"
	cfg.Tracker.ProjectSlug = "demo-team"
	cfg.Providers.Default.Provider = "ollama"
	cfg.Providers.Default.APIKey = "" // intentionally empty for local

	require.NoError(t, Validate(cfg))
}

func TestValidate_RejectsUnsupportedTracker(t *testing.T) {
	t.Parallel()
	cfg := Defaults()
	cfg.Project.ID = "demo"
	cfg.Tracker.Kind = "asana"
	cfg.Tracker.APIKey = "secret"
	cfg.Providers.Default.Provider = "openrouter"
	cfg.Providers.Default.APIKey = "sk-test"

	err := Validate(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "tracker.kind")
}
