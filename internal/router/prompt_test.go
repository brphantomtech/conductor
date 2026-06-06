package router

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/config"
	"github.com/conductor-sh/conductor/internal/harness"
)

func newPromptRouter(templates map[string]string) *Router {
	return New(
		WithConfig(func() config.Config { return config.Defaults() }),
		WithTemplates(func() map[string]string { return templates }),
	)
}

func TestBuildPrompt_SectionOrderAbsentOmitted(t *testing.T) {
	r := newPromptRouter(nil)
	out, err := r.BuildPrompt(PromptInputs{
		RoleTemplate:       "Role: {{ agent_role }}",
		Vars:               templateVars(issue("i1", "A-1", "x"), []string{"coder"}, 0, 1),
		PreviousRole:       "planner",
		PreviousRoleOutput: "plan body",
	})
	require.NoError(t, err)

	require.Contains(t, out, "Role: coder")
	require.Contains(t, out, "## Output from Previous Role (planner)")
	require.Contains(t, out, "plan body")
	// Absent sections omitted.
	require.NotContains(t, out, "## Codebase Context")
	require.NotContains(t, out, "## Relevant Documentation")
	require.NotContains(t, out, "## Relevant Memory")
	require.NotContains(t, out, "## Known Technical Debt")
	require.NotContains(t, out, "## Architectural Issues")

	// Role template precedes the previous-role section.
	require.Less(t, strings.Index(out, "Role: coder"), strings.Index(out, "## Output from Previous Role"))
}

func TestBuildPrompt_OverBudgetTruncatesBottomUp(t *testing.T) {
	big := strings.Repeat("x", 200)
	r := newPromptRouter(nil)
	out, err := r.BuildPrompt(PromptInputs{
		RoleTemplate:        "ROLE-TEMPLATE",
		Vars:                map[string]any{},
		PreviousRole:        "planner",
		PreviousRoleOutput:  "PREV-" + big,
		KnownDebt:           "DEBT-" + big,
		ArchitecturalIssues: "ARCH-" + big,
		ContextBudget:       300, // budget*0.7 = 210
	})
	require.NoError(t, err)

	require.Contains(t, out, "ROLE-TEMPLATE", "role template always retained")
	// Lowest-priority sections (8 then 7) dropped first.
	require.NotContains(t, out, "ARCH-", "architectural issues (section 8) dropped first")
	require.NotContains(t, out, "DEBT-", "known debt (section 7) dropped next")
}

func TestBuildPrompt_RoleTemplateNeverTruncated(t *testing.T) {
	r := newPromptRouter(nil)
	out, err := r.BuildPrompt(PromptInputs{
		RoleTemplate:        strings.Repeat("R", 500),
		Vars:                map[string]any{},
		ArchitecturalIssues: strings.Repeat("A", 500),
		ContextBudget:       10, // tiny budget; everything but role template must go
	})
	require.NoError(t, err)
	require.Contains(t, out, strings.Repeat("R", 500))
	require.NotContains(t, out, "## Architectural Issues")
}

func TestBuildPrompt_UnknownVariableFails(t *testing.T) {
	r := newPromptRouter(nil)
	_, err := r.BuildPrompt(PromptInputs{
		RoleTemplate: "{{ does_not_exist }}",
		Vars:         map[string]any{},
	})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrPromptRenderFailed)
	require.ErrorIs(t, err, harness.ErrTemplateRender)
}

func TestContinuationTemplate_HarnessSectionPreferred(t *testing.T) {
	r := newPromptRouter(map[string]string{continuationRole: "CUSTOM CONTINUATION"})
	require.Equal(t, "CUSTOM CONTINUATION", r.continuationTemplate())
}

func TestContinuationTemplate_BuiltinFallback(t *testing.T) {
	r := newPromptRouter(nil)
	require.Equal(t, builtinContinuationPrompt, r.continuationTemplate())

	rEmpty := newPromptRouter(map[string]string{continuationRole: "   "})
	require.Equal(t, builtinContinuationPrompt, rEmpty.continuationTemplate())
}

func TestBuildPrompt_RendersValidationSection(t *testing.T) {
	r := newPromptRouter(nil)
	out, err := r.BuildPrompt(PromptInputs{
		RoleTemplate:      "ROLE",
		Vars:              map[string]any{},
		ValidationResults: "lint failed",
	})
	require.NoError(t, err)
	require.Contains(t, out, "## Validation Results (Previous Turn)")
	require.Contains(t, out, "lint failed")
}

func TestBuildPrompt_RenderError(t *testing.T) {
	r := New(WithRenderer(func(string, map[string]any) (string, error) {
		return "", errors.New("render boom")
	}))
	_, err := r.BuildPrompt(PromptInputs{RoleTemplate: "x"})
	require.ErrorIs(t, err, ErrPromptRenderFailed)
}
