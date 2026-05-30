package harness_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/harness"
)

func TestRender_StandardVariables(t *testing.T) {
	t.Parallel()

	out, err := harness.Render(
		"Implement {{ issue.identifier }}: {{ issue.title }}.",
		map[string]any{
			"issue": map[string]any{
				"identifier": "ABC-123",
				"title":      "Add dark mode",
			},
		},
	)
	require.NoError(t, err)
	require.Equal(t, "Implement ABC-123: Add dark mode.", out)
}

func TestRender_BuiltinFilter(t *testing.T) {
	t.Parallel()

	out, err := harness.Render(
		"branch: {{ issue.identifier | downcase }}",
		map[string]any{
			"issue": map[string]any{"identifier": "ABC-123"},
		},
	)
	require.NoError(t, err)
	require.Equal(t, "branch: abc-123", out,
		"Liquid's downcase filter must be available — branch_template defaults rely on it (SPEC §5.3.4)")
}

func TestRender_UnknownVariableErrors(t *testing.T) {
	t.Parallel()

	_, err := harness.Render(
		"Hello {{ does_not_exist }}",
		map[string]any{
			"issue": map[string]any{"identifier": "ABC-123"},
		},
	)
	require.Error(t, err)
	require.True(t, errors.Is(err, harness.ErrTemplateRender),
		"undefined variable must classify as ErrTemplateRender (SPEC §16.2), got %v", err)
}

func TestRender_UnknownFilterErrors(t *testing.T) {
	t.Parallel()

	_, err := harness.Render(
		"{{ issue.identifier | totally_made_up }}",
		map[string]any{
			"issue": map[string]any{"identifier": "ABC-123"},
		},
	)
	require.Error(t, err)
	require.True(t,
		errors.Is(err, harness.ErrTemplateRender) ||
			errors.Is(err, harness.ErrTemplateParse),
		"unknown filter must classify as a render or parse error (SPEC §16.2), got %v", err)
}

func TestRender_SyntaxError(t *testing.T) {
	t.Parallel()

	_, err := harness.Render("{% if foo %}no end tag", map[string]any{})
	require.Error(t, err)
	require.True(t, errors.Is(err, harness.ErrTemplateParse),
		"unterminated tag must classify as ErrTemplateParse, got %v", err)
}

func TestCheckTemplate_AcceptsValidSource(t *testing.T) {
	t.Parallel()
	require.NoError(t, harness.CheckTemplate("Hello {{ issue.identifier }}!"))
}

func TestCheckTemplate_RejectsInvalidSource(t *testing.T) {
	t.Parallel()
	err := harness.CheckTemplate("{% wrong_tag %}")
	require.Error(t, err)
	require.True(t, errors.Is(err, harness.ErrTemplateParse))
}

func TestMockBindings_RendersAllowlistedRefs(t *testing.T) {
	t.Parallel()

	src := "issue={{ issue.identifier }} attempt={{ attempt }} role={{ agent_role }} " +
		"pipeline_len={{ pipeline_length }} mem={{ memory_summary }} kn={{ knowledge_summary }}"

	out, err := harness.Render(src, harness.MockBindings())
	require.NoError(t, err,
		"every SPEC §16.2 root variable must resolve against MockBindings")
	require.Contains(t, out, "issue=MOCK-1")
	require.Contains(t, out, "role=coder")
	require.Contains(t, out, "pipeline_len=1")
}

func TestIsAllowedRootVariable(t *testing.T) {
	t.Parallel()
	for _, v := range harness.AllowedTemplateVariables {
		require.True(t, harness.IsAllowedRootVariable(v),
			"%q must be reported as allowed", v)
	}
	require.False(t, harness.IsAllowedRootVariable("nope"))
}
