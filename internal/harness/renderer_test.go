package harness

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenderer_KnownVariablesRender(t *testing.T) {
	r := NewRenderer()
	tmpl, err := r.Compile("planner", "Issue {{ issue.identifier }}: {{ issue.title }}")
	require.NoError(t, err)

	vars := map[string]any{
		"issue": map[string]any{
			"identifier": "ENG-1",
			"title":      "Hello",
		},
	}
	out, err := tmpl.Render(vars)
	require.NoError(t, err)
	require.Equal(t, "Issue ENG-1: Hello", out)
}

func TestRenderer_UnknownVariableFailsRender(t *testing.T) {
	r := NewRenderer()
	tmpl, err := r.Compile("planner", "{{ unknown_variable }}")
	require.NoError(t, err)

	_, err = tmpl.Render(map[string]any{})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrTemplateRender), "expected ErrTemplateRender, got %v", err)
}

func TestRenderer_UnknownFilterFailsRenderOrParse(t *testing.T) {
	r := NewRenderer()
	tmpl, compileErr := r.Compile("planner", "{{ \"x\" | not_a_real_filter }}")
	if compileErr != nil {
		// Some engine versions raise this at parse time, which the
		// spec permits.
		require.True(t, errors.Is(compileErr, ErrTemplateParse))
		return
	}
	_, renderErr := tmpl.Render(map[string]any{})
	require.Error(t, renderErr)
	require.True(t,
		errors.Is(renderErr, ErrTemplateRender) || errors.Is(renderErr, ErrTemplateParse),
		"expected ErrTemplateRender or ErrTemplateParse, got %v", renderErr,
	)
}

func TestRenderer_SyntaxErrorClassifiedAsParse(t *testing.T) {
	r := NewRenderer()
	// Liquid raises parse errors for empty expressions ({{ }}) and for
	// unterminated block tags. A lone "{{" with no closing braces is
	// tolerated and rendered as literal text, so we exercise the
	// empty-expression form here.
	_, err := r.Compile("planner", "{{ }}")
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrTemplateParse), "expected ErrTemplateParse, got %v", err)
}

func TestRenderer_StandardFiltersAvailable(t *testing.T) {
	r := NewRenderer()
	tmpl, err := r.Compile("planner", "{{ identifier | downcase }}")
	require.NoError(t, err)

	out, err := tmpl.Render(map[string]any{"identifier": "ENG-42"})
	require.NoError(t, err)
	require.Equal(t, "eng-42", out)
}

func TestRenderer_StandardVariablesRender(t *testing.T) {
	r := NewRenderer()
	src := "Role={{ agent_role }} attempt={{ attempt }} pipeline_length={{ pipeline_length }}"
	tmpl, err := r.Compile("planner", src)
	require.NoError(t, err)

	out, err := tmpl.Render(StandardVariables())
	require.NoError(t, err)
	require.Equal(t, "Role=coder attempt=1 pipeline_length=3", out)
}

func TestTemplate_NameIsRetained(t *testing.T) {
	r := NewRenderer()
	tmpl, err := r.Compile("verifier", "ok")
	require.NoError(t, err)
	require.Equal(t, "verifier", tmpl.Name())
}
