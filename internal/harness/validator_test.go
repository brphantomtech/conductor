package harness_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/config"
	"github.com/conductor-sh/conductor/internal/harness"
)

func newCfg(pipeline []string, rules ...config.RoutingRule) config.Config {
	cfg := config.Defaults()
	cfg.Routing.Pipeline = pipeline
	cfg.Routing.Rules = rules
	return cfg
}

func TestValidate_PassesCleanDefinition(t *testing.T) {
	t.Parallel()

	def := &harness.Definition{
		PromptTemplates: map[string]string{
			"planner":  "Plan {{ issue.identifier }}.",
			"coder":    "Implement {{ issue.title }}.",
			"verifier": "Verify {{ issue.identifier }}.",
		},
	}
	cfg := newCfg([]string{"planner", "coder", "verifier"})

	res, err := harness.Validate(def, cfg)
	require.NoError(t, err)
	require.False(t, res.HasErrors())
}

func TestValidate_DetectsMissingPipelineRole(t *testing.T) {
	t.Parallel()

	def := &harness.Definition{
		PromptTemplates: map[string]string{
			"coder": "Implement {{ issue.title }}.",
		},
	}
	cfg := newCfg([]string{"planner", "coder"})

	res, err := harness.Validate(def, cfg)
	require.Error(t, err)
	require.True(t, res.HasErrors())
	require.True(t, errors.Is(err, harness.ErrHarnessParse),
		"missing pipeline role must classify as ErrHarnessParse, got %v", err)

	roles := []string{}
	for _, iss := range res.Issues {
		roles = append(roles, iss.Role)
	}
	require.Contains(t, roles, "planner",
		"validator must report the missing role explicitly")
}

func TestValidate_DetectsMissingRoleFromRoutingRule(t *testing.T) {
	t.Parallel()

	def := &harness.Definition{
		PromptTemplates: map[string]string{
			"coder": "Implement {{ issue.title }}.",
		},
	}
	cfg := newCfg(
		[]string{"coder"},
		config.RoutingRule{Pipeline: []string{"gc_agent"}},
	)

	res, err := harness.Validate(def, cfg)
	require.Error(t, err)
	require.True(t, errors.Is(err, harness.ErrHarnessParse))

	roles := map[string]bool{}
	for _, iss := range res.Issues {
		roles[iss.Role] = true
	}
	require.True(t, roles["gc_agent"],
		"roles referenced via routing.rules[].pipeline must also be checked")
}

func TestValidate_FailsOnTemplateSyntaxError(t *testing.T) {
	t.Parallel()

	def := &harness.Definition{
		PromptTemplates: map[string]string{
			"coder": "{% if foo %}no end tag",
		},
	}
	cfg := newCfg([]string{"coder"})

	res, err := harness.Validate(def, cfg)
	require.Error(t, err)
	require.True(t, errors.Is(err, harness.ErrTemplateParse),
		"unterminated tag must classify as ErrTemplateParse, got %v", err)
	require.NotEmpty(t, res.Issues)
}

func TestValidate_FailsOnUnknownVariable(t *testing.T) {
	t.Parallel()

	def := &harness.Definition{
		PromptTemplates: map[string]string{
			"coder": "Look at {{ totally_unknown_variable }}",
		},
	}
	cfg := newCfg([]string{"coder"})

	res, err := harness.Validate(def, cfg)
	require.Error(t, err)
	require.True(t, errors.Is(err, harness.ErrTemplateRender),
		"unknown variable surfaced via mock-binding render must classify as ErrTemplateRender, got %v", err)
	require.NotEmpty(t, res.Issues)
}

func TestValidate_NilDefinitionFailsFast(t *testing.T) {
	t.Parallel()

	_, err := harness.Validate(nil, config.Defaults())
	require.Error(t, err)
	require.True(t, errors.Is(err, harness.ErrHarnessParse))
}

func TestValidate_DeduplicatesMissingRoleAcrossRules(t *testing.T) {
	t.Parallel()

	def := &harness.Definition{PromptTemplates: map[string]string{"coder": "Hi."}}
	cfg := newCfg(
		[]string{"coder", "ghost"},
		config.RoutingRule{Pipeline: []string{"ghost", "ghost"}},
		config.RoutingRule{Pipeline: []string{"ghost"}},
	)

	res, err := harness.Validate(def, cfg)
	require.Error(t, err)

	count := 0
	for _, iss := range res.Issues {
		if iss.Role == "ghost" {
			count++
		}
	}
	require.Equal(t, 1, count,
		"ghost role must be reported exactly once even when referenced multiple times")
}
