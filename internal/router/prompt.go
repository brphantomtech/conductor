package router

import (
	"fmt"
	"strings"

	"github.com/conductor-sh/conductor/internal/tracker"
)

// classificationPrompt is the SPEC §12.2 classification template. It is a
// Liquid template rendered against classificationVars.
const classificationPrompt = `Classify the following issue into exactly one task type:
feature, bug, refactor, investigation, docs, gc_task, unknown.

Issue: {{ issue.title }}
Description: {{ issue.description }}
Labels: {{ issue.labels | join: ", " }}

Respond with only the task type, no explanation.`

// builtinContinuationPrompt is the SPEC §16.3 fallback continuation template,
// used when HARNESS.md has no `## continuation` section.
const builtinContinuationPrompt = `Continue working on {{ issue.identifier }}: {{ issue.title }}.
This is continuation turn {{ attempt }}.
Review your previous work and the validation results, then continue.`

// continuationRole is the HARNESS.md section name carrying the continuation
// template (SPEC §16.3).
const continuationRole = "continuation"

// PromptInputs is the full input set for BuildPrompt. The role template plus
// the SPEC §16.1 section sources flow through this struct so later phases
// (knowledge/docs/memory/enforcer) can populate their slots without changing
// the assembly loop. Empty section sources are omitted from the output.
type PromptInputs struct {
	// RoleTemplate is the Liquid source for the current role (section 1).
	RoleTemplate string
	// Vars is the SPEC §16.2 Liquid variable map used to render RoleTemplate.
	Vars map[string]any

	// ValidationResults is section 2 (Validation Pipeline output).
	ValidationResults string
	// PreviousRole and PreviousRoleOutput form section 3.
	PreviousRole       string
	PreviousRoleOutput string
	// CodebaseContext is section 4 (Knowledge Engine; Phase 10).
	CodebaseContext string
	// Documentation is section 5 (Doc Store Manager; Phase 11).
	Documentation string
	// Memory is section 6 (Memory Manager; Phase 9).
	Memory string
	// KnownDebt is section 7 (warning-severity harness violations; Phase 12).
	KnownDebt string
	// ArchitecturalIssues is section 8 (error-severity violations; Phase 12).
	ArchitecturalIssues string

	// ContextBudget is the role provider's context_budget; truncation triggers
	// at ContextBudget*0.7. A non-positive value disables truncation.
	ContextBudget int
}

// promptSection is one assembled section with its priority for truncation. A
// higher truncateOrder is dropped first (sections 7–8 before 4–6).
type promptSection struct {
	body          string
	truncateOrder int
}

// BuildPrompt assembles a turn prompt in the SPEC §16.1 order: role template,
// validation results, previous-role output, codebase context, documentation,
// memory, known debt, architectural issues. Sections whose source is empty are
// omitted. If the result exceeds ContextBudget*0.7, lower-priority sections are
// dropped from the bottom first while the role template is retained. An unknown
// Liquid variable or filter in the role template yields ErrPromptRenderFailed.
func (r *Router) BuildPrompt(in PromptInputs) (string, error) {
	rendered, err := r.render(in.RoleTemplate, in.Vars)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrPromptRenderFailed, err)
	}

	// Section 1 (role template) is never truncated: truncateOrder 0.
	sections := []promptSection{{body: rendered, truncateOrder: 0}}

	add := func(header, body string, order int) {
		if strings.TrimSpace(body) == "" {
			return
		}
		text := body
		if header != "" {
			text = header + "\n\n" + body
		}
		sections = append(sections, promptSection{body: text, truncateOrder: order})
	}

	// truncateOrder mirrors the SPEC §16.1 section index: bottom-up truncation
	// drops the highest-numbered (lowest-priority) section first, so section 8
	// (architectural issues) is dropped before section 7 (known debt), and so
	// on. The role template (section 1, order 0) is never dropped.
	add("## Validation Results (Previous Turn)", in.ValidationResults, 2)
	if prev := strings.TrimSpace(in.PreviousRoleOutput); prev != "" {
		add(fmt.Sprintf("## Output from Previous Role (%s)", in.PreviousRole), in.PreviousRoleOutput, 3)
	}
	add("## Codebase Context", in.CodebaseContext, 4)
	add("## Relevant Documentation", in.Documentation, 5)
	add("## Relevant Memory", in.Memory, 6)
	add("## Known Technical Debt", in.KnownDebt, 7)
	add("## Architectural Issues You Must Fix", in.ArchitecturalIssues, 8)

	return assemble(sections, in.ContextBudget), nil
}

// assemble joins sections in order and applies bottom-up truncation. The
// budget is ContextBudget*0.7 (SPEC §16.1). Truncation drops whole sections
// starting from the highest truncateOrder (lowest priority) while always
// keeping the role template (truncateOrder 0).
func assemble(sections []promptSection, contextBudget int) string {
	join := func(secs []promptSection) string {
		parts := make([]string, len(secs))
		for i, s := range secs {
			parts[i] = s.body
		}
		return strings.Join(parts, "\n\n")
	}

	if contextBudget <= 0 {
		return join(sections)
	}
	limit := contextBudget * 7 / 10

	out := join(sections)
	for len(out) > limit {
		// Find the section with the highest truncateOrder (>0) and drop it.
		dropIdx := -1
		maxOrder := 0
		for i, s := range sections {
			if s.truncateOrder > maxOrder {
				maxOrder = s.truncateOrder
				dropIdx = i
			}
		}
		if dropIdx < 0 {
			break // only the role template remains; cannot truncate further.
		}
		sections = append(sections[:dropIdx], sections[dropIdx+1:]...)
		out = join(sections)
	}
	return out
}

// continuationTemplate returns the continuation prompt source for a role
// (SPEC §16.3): the HARNESS.md `## continuation` section when present,
// otherwise the built-in continuation prompt. The full original prompt is
// never re-sent on continuation turns — callers use this template instead.
func (r *Router) continuationTemplate() string {
	if src, ok := r.templateFn()[continuationRole]; ok && strings.TrimSpace(src) != "" {
		return src
	}
	return builtinContinuationPrompt
}

// templateVars builds the SPEC §16.2 root-variable map for a role turn. Every
// allowed variable is supplied (with placeholders for the not-yet-wired
// memory/knowledge summaries) so the strict Liquid renderer never faults on a
// documented reference.
func templateVars(iss tracker.Issue, pipeline []string, pipelineIndex, attempt int) map[string]any {
	role := ""
	if pipelineIndex >= 0 && pipelineIndex < len(pipeline) {
		role = pipeline[pipelineIndex]
	}
	return map[string]any{
		"issue":             issueVars(iss),
		"attempt":           attempt,
		"agent_role":        role,
		"pipeline":          pipeline,
		"pipeline_index":    pipelineIndex,
		"pipeline_length":   len(pipeline),
		"memory_summary":    "",
		"knowledge_summary": "",
	}
}

// classificationVars builds the minimal variable map the classification prompt
// references (SPEC §12.2).
func classificationVars(iss tracker.Issue) map[string]any {
	return map[string]any{"issue": issueVars(iss)}
}

// issueVars projects a tracker issue into the SPEC §16.2 template-visible
// subset. Nullable fields render as their zero value.
func issueVars(iss tracker.Issue) map[string]any {
	return map[string]any{
		"id":                   iss.ID,
		"identifier":           iss.Identifier,
		"title":                iss.Title,
		"description":          deref(iss.Description),
		"priority":             derefInt(iss.Priority),
		"state":                iss.State,
		"url":                  deref(iss.URL),
		"labels":               iss.Labels,
		"blocked_by":           iss.BlockedBy,
		"created_at":           "",
		"updated_at":           "",
		"task_type":            deref(iss.TaskType),
		"estimated_complexity": deref(iss.EstimatedComplexity),
	}
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}
