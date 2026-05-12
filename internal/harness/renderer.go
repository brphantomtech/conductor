package harness

import (
	"fmt"

	"github.com/osteele/liquid"
)

// Renderer wraps a Liquid engine configured for SPEC §16.2's strict
// semantics: unknown variables and unknown filters both produce errors
// rather than silently rendering as empty strings.
//
// One Renderer is shared across all role templates; per-role compiled
// state lives on Template (returned by Compile).
type Renderer struct {
	engine *liquid.Engine
}

// NewRenderer constructs a Renderer with the SPEC §16.2 strictness rules
// applied. The osteele/liquid engine errors on undefined filters by
// default; StrictVariables() makes it error on undefined variables too,
// which together cover the spec's "unknown variable / unknown filter →
// template_render_error" requirement.
func NewRenderer() *Renderer {
	e := liquid.NewEngine()
	e.StrictVariables()
	return &Renderer{engine: e}
}

// Template is one compiled prompt-template, ready to render with a
// concrete variable map. It is safe to render the same Template
// concurrently because osteele/liquid renders are stateless once the
// AST is built.
type Template struct {
	name     string
	compiled *liquid.Template
}

// Name returns the role name (the literal text after `## ` in the
// HARNESS.md heading, or "coder" for the no-heading default).
func (t *Template) Name() string {
	if t == nil {
		return ""
	}
	return t.name
}

// Compile parses a single role template. Compile-time syntax failures
// surface as ErrTemplateParse so callers (the validator, the CLI) can
// classify them. The name argument is purely diagnostic — it is
// embedded in error context so a multi-role validation pass tells the
// operator which section failed.
func (r *Renderer) Compile(name, source string) (*Template, error) {
	if r == nil || r.engine == nil {
		return nil, fmt.Errorf("harness: renderer not initialized")
	}
	tmpl, perr := r.engine.ParseTemplate([]byte(source))
	if perr != nil {
		return nil, fmt.Errorf("%w: template %q: %s", ErrTemplateParse, name, perr.Error())
	}
	return &Template{name: name, compiled: tmpl}, nil
}

// Render executes the compiled template against vars. Unknown variable
// references and unknown filter calls both surface as ErrTemplateRender;
// the engine's strict-variable mode is the source of the former, while
// unknown filters are an engine default.
func (t *Template) Render(vars map[string]any) (string, error) {
	if t == nil || t.compiled == nil {
		return "", fmt.Errorf("harness: render of uninitialized template")
	}
	if vars == nil {
		vars = map[string]any{}
	}
	out, rerr := t.compiled.Render(vars)
	if rerr != nil {
		return "", fmt.Errorf("%w: template %q: %s", ErrTemplateRender, t.name, rerr.Error())
	}
	return string(out), nil
}

// StandardVariables returns a representative variable map containing
// every name listed in SPEC §16.2. Tests and the validator use it to
// confirm a template renders cleanly with the documented standard
// surface. Values are placeholders chosen so type-sensitive filters
// (date, default, upcase) succeed.
func StandardVariables() map[string]any {
	return map[string]any{
		"issue": map[string]any{
			"id":                   "issue-1",
			"identifier":           "ENG-1",
			"title":                "Sample issue",
			"description":          "",
			"priority":             0,
			"state":                "Todo",
			"url":                  "",
			"labels":               []string{},
			"blocked_by":           []string{},
			"created_at":           "2026-01-01T00:00:00Z",
			"updated_at":           "2026-01-01T00:00:00Z",
			"task_type":            "feature",
			"estimated_complexity": "medium",
		},
		"attempt":           1,
		"agent_role":        "coder",
		"pipeline":          []string{"planner", "coder", "verifier"},
		"pipeline_index":    1,
		"pipeline_length":   3,
		"memory_summary":    "",
		"knowledge_summary": "",
	}
}
