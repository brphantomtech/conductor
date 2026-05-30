package harness

import (
	"fmt"
	"strings"
	"sync"

	"github.com/osteele/liquid"
)

// SPEC §16.2 — Liquid Template Variables.
//
// The harness package owns the canonical engine because every consumer (the
// orchestrator's prompt assembler, validation pipelines, hooks that allow
// branch-name templating) must share the same filter set and the same
// strictness contract — undefined variables and unknown filters are
// `template_render_error` everywhere.

// AllowedTemplateVariables is the SPEC §16.2 root-variable allowlist used
// by both render-time strict mode and the static-analysis check the
// validator runs before any prompt is assembled.
//
// Adding a new variable must be a SPEC change (and a corresponding bump
// here) so a typo in HARNESS.md cannot quietly resolve to nil at runtime.
var AllowedTemplateVariables = []string{
	"issue",
	"attempt",
	"agent_role",
	"pipeline",
	"pipeline_index",
	"pipeline_length",
	"memory_summary",
	"knowledge_summary",
}

// engineOnce builds the single global Liquid Engine. Engines are
// expensive to construct (they walk a non-trivial filter registry) and the
// docs explicitly mark the type as safe for concurrent Parse/Render.
var (
	engineOnce sync.Once
	engine     *liquid.Engine
)

// Engine returns the package-wide Liquid engine, configured for strict
// variables and the default Shopify filter set. Render uses it directly;
// callers that want to inspect the parsed template (e.g. for static
// analysis) can use it too.
func Engine() *liquid.Engine {
	engineOnce.Do(func() {
		e := liquid.NewEngine()
		// SPEC §16.2: undefined variables MUST surface as
		// template_render_error rather than silently rendering as the
		// empty string.
		e.StrictVariables()
		engine = e
	})
	return engine
}

// Render compiles and executes a Liquid template against vars.
//
// Errors classified per SPEC §23.1:
//
//   - ErrTemplateParse — source has a Liquid syntax error or references
//     an unregistered tag.
//   - ErrTemplateRender — render-time failure: undefined variable,
//     unknown filter, or filter-runtime error.
//
// The returned error always wraps the underlying liquid SourceError so
// callers may type-assert to it via errors.As to surface line numbers in
// CLI output.
func Render(source string, vars map[string]any) (string, error) {
	tmpl, perr := Engine().ParseString(source)
	if perr != nil {
		return "", fmt.Errorf("%w: %w", ErrTemplateParse, perr)
	}
	out, rerr := tmpl.RenderString(liquid.Bindings(vars))
	if rerr != nil {
		return "", classifyRenderError(rerr)
	}
	return out, nil
}

// CheckTemplate parses source and reports template_parse_error without
// attempting to render. This is the static check the validator runs at
// startup so syntax errors fail loudly before any agent dispatch.
func CheckTemplate(source string) error {
	if _, perr := Engine().ParseString(source); perr != nil {
		return fmt.Errorf("%w: %w", ErrTemplateParse, perr)
	}
	return nil
}

// classifyRenderError converts a liquid render error into the SPEC
// sentinel set. The library reports both undefined-variable and
// unknown-filter errors as render-time failures; SPEC §23.1 lumps them
// together as template_render_error so we map both cases to that sentinel.
func classifyRenderError(err error) error {
	return fmt.Errorf("%w: %w", ErrTemplateRender, err)
}

// IsAllowedRootVariable reports whether name is in the SPEC §16.2
// root-variable allowlist. Used by the validator's static analysis.
func IsAllowedRootVariable(name string) bool {
	for _, v := range AllowedTemplateVariables {
		if v == name {
			return true
		}
	}
	return false
}

// MockBindings returns a Liquid binding map populated with stub values for
// every SPEC §16.2 variable. The validator uses this when it dry-renders a
// template at startup to surface unknown-filter errors before any agent
// runs against the harness.
//
// The mock issue mirrors the SPEC §4.1.1 field set so templates that walk
// `issue.labels`, `issue.blocked_by[0].identifier`, etc., resolve cleanly.
func MockBindings() map[string]any {
	return map[string]any{
		"issue": map[string]any{
			"id":                   "mock-id",
			"identifier":           "MOCK-1",
			"title":                "Mock issue",
			"description":          "",
			"priority":             0,
			"state":                "Todo",
			"url":                  "",
			"labels":               []string{},
			"blocked_by":           []map[string]any{},
			"created_at":           "",
			"updated_at":           "",
			"task_type":            "",
			"estimated_complexity": "",
		},
		"attempt":           0,
		"agent_role":        DefaultRole,
		"pipeline":          []string{DefaultRole},
		"pipeline_index":    0,
		"pipeline_length":   1,
		"memory_summary":    "",
		"knowledge_summary": "",
	}
}

// FormatRenderLocation renders a `path:line` style suffix from a Liquid
// SourceError so error messages emitted by the CLI stay editor-jumpable.
// Returns an empty string when err is not a SourceError.
func FormatRenderLocation(err error) string {
	// SourceError is the library's typed boundary; classifyRenderError
	// keeps it inside the wrapped chain so a direct type assertion is the
	// right way to recover it for source-location formatting.
	se, ok := err.(liquid.SourceError) //nolint:errorlint // see comment above.
	if !ok {
		return ""
	}
	path := se.Path()
	if path == "" {
		path = "<harness>"
	}
	return strings.TrimSpace(fmt.Sprintf("%s:%d", path, se.LineNumber()))
}
