package harness

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/config"
)

// validCfg returns a Config that passes Validate. Tests start from this
// and zero specific fields to trigger one rule at a time.
func validCfg() config.Config {
	return config.Config{
		Project: config.Project{ID: "demo"},
		Tracker: config.Tracker{
			Kind:        "linear",
			APIKey:      "secret",
			ProjectSlug: "team",
		},
		Providers: config.Providers{
			Default: config.ProviderConfig{
				Provider: "anthropic",
				APIKey:   "k",
			},
		},
		Knowledge: config.Knowledge{StoreBackend: "sqlite_vec"},
		Memory:    config.Memory{StoreBackend: "sqlite"},
		Routing:   config.Routing{Pipeline: []string{"planner", "coder", "verifier"}},
	}
}

// validDef returns a Definition with templates matching validCfg's
// pipeline.
func validDef() *Definition {
	return &Definition{
		FrontMatter: map[string]any{},
		PromptTemplates: map[string]string{
			"planner":  "{{ issue.identifier }} plan",
			"coder":    "code",
			"verifier": "verify",
		},
	}
}

func TestValidate_CleanCaseReturnsNil(t *testing.T) {
	require.NoError(t, Validate(validDef(), validCfg()))
}

func TestValidate_MissingProjectIDIsReported(t *testing.T) {
	cfg := validCfg()
	cfg.Project.ID = ""

	err := Validate(validDef(), cfg)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrProjectIDMissing))
}

func TestValidate_MissingTrackerAPIKeyIsReported(t *testing.T) {
	cfg := validCfg()
	cfg.Tracker.APIKey = ""

	err := Validate(validDef(), cfg)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrTrackerAPIKeyMissing))
}

func TestValidate_UnsupportedTrackerKindIsReported(t *testing.T) {
	cfg := validCfg()
	cfg.Tracker.Kind = "redmine"

	err := Validate(validDef(), cfg)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrTrackerKindUnsupported))
}

func TestValidate_UnsupportedProviderIsReported(t *testing.T) {
	cfg := validCfg()
	cfg.Providers.Default.Provider = "claude2"

	err := Validate(validDef(), cfg)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrProviderUnsupported))
}

func TestValidate_MissingProviderAPIKeyForHostedProviderIsReported(t *testing.T) {
	cfg := validCfg()
	cfg.Providers.Default.APIKey = ""

	err := Validate(validDef(), cfg)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrProviderAPIKeyMissing))
}

func TestValidate_OllamaWithoutAPIKeyIsAccepted(t *testing.T) {
	cfg := validCfg()
	cfg.Providers.Default.Provider = "ollama"
	cfg.Providers.Default.APIKey = ""

	require.NoError(t, Validate(validDef(), cfg))
}

func TestValidate_MultipleProblemsAreJoined(t *testing.T) {
	cfg := validCfg()
	cfg.Tracker.APIKey = ""
	cfg.Providers.Default.Provider = "made_up_thing"

	err := Validate(validDef(), cfg)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrTrackerAPIKeyMissing))
	require.True(t, errors.Is(err, ErrProviderUnsupported))
}

func TestValidate_PipelineRoleWithoutTemplateIsReported(t *testing.T) {
	cfg := validCfg()
	cfg.Routing.Pipeline = []string{"planner", "coder", "verifier", "reviewer"}

	err := Validate(validDef(), cfg)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrPipelineRoleMissingTemplate))
}

func TestValidate_RoutingRulePipelineRoleWithoutTemplateIsReported(t *testing.T) {
	cfg := validCfg()
	cfg.Routing.Rules = []config.RoutingRule{
		{
			When:     config.RoutingMatch{TaskType: "bug"},
			Pipeline: []string{"planner", "coder", "reviewer"},
		},
	}

	err := Validate(validDef(), cfg)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrPipelineRoleMissingTemplate))
}

func TestValidate_TrackerNeedsProjectRef(t *testing.T) {
	cfg := validCfg()
	cfg.Tracker.ProjectSlug = ""
	cfg.Tracker.ProjectID = ""

	err := Validate(validDef(), cfg)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrTrackerProjectRefMissing))
}

func TestValidate_BadTemplateIsReported(t *testing.T) {
	cfg := validCfg()
	def := validDef()
	// Empty expression is rejected at parse time by osteele/liquid.
	def.PromptTemplates["coder"] = "{{ }}"

	err := Validate(def, cfg)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrTemplateParse))
}

func TestValidate_UnsupportedKnowledgeBackend(t *testing.T) {
	cfg := validCfg()
	cfg.Knowledge.StoreBackend = "chromadb"

	err := Validate(validDef(), cfg)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrKnowledgeBackendUnsupported))
}

func TestValidate_UnsupportedMemoryBackend(t *testing.T) {
	cfg := validCfg()
	cfg.Memory.StoreBackend = "mongodb"

	err := Validate(validDef(), cfg)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrMemoryBackendUnsupported))
}

func TestValidate_UnsupportedDocStoreBackend(t *testing.T) {
	cfg := validCfg()
	cfg.Docs.Stores = []config.DocStoreConfig{{ID: "x", Backend: "dropbox"}}

	err := Validate(validDef(), cfg)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrDocStoreBackendUnsupported))
}

func TestValidate_NilDefinitionIsAnError(t *testing.T) {
	err := Validate(nil, validCfg())
	require.Error(t, err)
}
