package harness

import (
	"errors"
	"fmt"

	"github.com/conductor-sh/conductor/internal/config"
)

// ValidationIssue is one non-fatal finding emitted by Validate. Issues are
// surfaced both individually (via errors.Join in the returned error) and as
// a slice on ValidationResult so the CLI can render a structured report.
type ValidationIssue struct {
	// Sentinel is one of the SPEC §23.1 sentinels (ErrTemplateParse,
	// ErrTemplateRender, ErrHarnessParse, ErrHarnessFrontMatterShape).
	Sentinel error

	// Role is the agent role the issue is associated with, if any.
	// Validation findings that span the whole document leave this empty.
	Role string

	// Message is the human-readable description that appears in CLI
	// output and audit events.
	Message string
}

// Error implements the error interface so a ValidationIssue can be
// composed with errors.Join.
func (v ValidationIssue) Error() string {
	if v.Role == "" {
		return fmt.Sprintf("%s: %s", v.Sentinel.Error(), v.Message)
	}
	return fmt.Sprintf("%s: role %q: %s", v.Sentinel.Error(), v.Role, v.Message)
}

// Unwrap exposes the sentinel so errors.Is finds the SPEC classification.
func (v ValidationIssue) Unwrap() error { return v.Sentinel }

// ValidationResult is the structured finding set Validate returns alongside
// its joined error. Callers that want to render per-role issues (the CLI's
// `harness validate` subcommand, the dashboard's harness editor) iterate
// Issues; callers that want a single Go error use the returned error.
type ValidationResult struct {
	Issues []ValidationIssue
}

// HasErrors reports whether the result contains at least one issue.
func (r ValidationResult) HasErrors() bool { return len(r.Issues) > 0 }

// Validate cross-checks a parsed Definition against the typed Config decoded
// from its front matter. It implements the harness-side of SPEC §6.4
// startup validation:
//
//   - Every prompt template must parse as valid Liquid.
//   - Every prompt template must render against the SPEC §16.2 mock
//     bindings without unknown-variable / unknown-filter errors.
//   - Every role referenced by routing.pipeline and routing.rules[].pipeline
//     must have a matching prompt template (SPEC §6.4 final bullet).
//
// Validate does NOT re-run config.Validate; the orchestrator chains the two
// because each addresses a different layer (typed-config shape vs.
// harness-template integrity).
func Validate(def *Definition, cfg config.Config) (ValidationResult, error) {
	if def == nil {
		return ValidationResult{}, fmt.Errorf("%w: definition is nil", ErrHarnessParse)
	}

	var result ValidationResult

	for role, src := range def.PromptTemplates {
		if perr := CheckTemplate(src); perr != nil {
			result.Issues = append(result.Issues, ValidationIssue{
				Sentinel: ErrTemplateParse,
				Role:     role,
				Message:  perr.Error(),
			})
			continue
		}
		// Dry-render against mock bindings so unknown filters and
		// undefined variables are caught before any agent runs.
		if _, rerr := Render(src, MockBindings()); rerr != nil {
			sentinel := ErrTemplateRender
			if errors.Is(rerr, ErrTemplateParse) {
				sentinel = ErrTemplateParse
			}
			result.Issues = append(result.Issues, ValidationIssue{
				Sentinel: sentinel,
				Role:     role,
				Message:  rerr.Error(),
			})
		}
	}

	missing := missingPipelineTemplates(def.PromptTemplates, cfg.Routing)
	for _, role := range missing {
		result.Issues = append(result.Issues, ValidationIssue{
			Sentinel: ErrHarnessParse,
			Role:     role,
			Message: fmt.Sprintf(
				"role %q is referenced by routing.pipeline but has no `## %s` section in HARNESS.md",
				role, role,
			),
		})
	}

	if !result.HasErrors() {
		return result, nil
	}
	errs := make([]error, len(result.Issues))
	for i, iss := range result.Issues {
		errs[i] = iss
	}
	return result, errors.Join(errs...)
}

// missingPipelineTemplates returns the roles referenced by routing.pipeline
// or any routing.rules[].pipeline that have no corresponding prompt
// template. Order is deterministic: pipeline first, then rules in the
// order they appear, with duplicates removed.
func missingPipelineTemplates(templates map[string]string, routing config.Routing) []string {
	seen := map[string]struct{}{}
	for role := range templates {
		seen[role] = struct{}{}
	}

	var missing []string
	missingSet := map[string]struct{}{}
	check := func(role string) {
		if role == "" {
			return
		}
		if _, ok := seen[role]; ok {
			return
		}
		if _, dup := missingSet[role]; dup {
			return
		}
		missingSet[role] = struct{}{}
		missing = append(missing, role)
	}

	for _, role := range routing.Pipeline {
		check(role)
	}
	for _, rule := range routing.Rules {
		for _, role := range rule.Pipeline {
			check(role)
		}
	}
	return missing
}
