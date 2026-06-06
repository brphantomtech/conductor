package harness

import (
	"fmt"
	"strings"
)

// Prompt section headings fixed by SPEC §16.1 steps 7–8. The formatters emit
// these verbatim so the live turn assembly (Phase 13) inserts them unchanged.
const (
	sectionTechnicalDebt      = "## Known Technical Debt"
	sectionArchitecturalIssue = "## Architectural Issues You Must Fix"
)

// FormatTechnicalDebt renders warning-severity violations as the SPEC §16.1
// "## Known Technical Debt" prompt section. It returns the empty string when
// there are no violations so callers can append it unconditionally without
// emitting an empty heading.
func FormatTechnicalDebt(violations []Violation) string {
	items := filterBySeverity(violations, SeverityWarning)
	if len(items) == 0 {
		return ""
	}
	return renderSection(sectionTechnicalDebt, items)
}

// FormatArchitecturalIssues renders error-severity violations as the SPEC §16.1
// "## Architectural Issues You Must Fix" prompt section. It returns the empty
// string when there are no violations.
func FormatArchitecturalIssues(violations []Violation) string {
	items := filterBySeverity(violations, SeverityError)
	if len(items) == 0 {
		return ""
	}
	return renderSection(sectionArchitecturalIssue, items)
}

// filterBySeverity returns the violations matching sev, preserving order.
func filterBySeverity(violations []Violation, sev Severity) []Violation {
	out := make([]Violation, 0, len(violations))
	for _, v := range violations {
		if v.Severity == sev {
			out = append(out, v)
		}
	}
	return out
}

// renderSection renders a heading followed by one bullet per violation. Each
// bullet leads with the rule name and summary, with the fix hint and affected
// files appended when present.
func renderSection(heading string, violations []Violation) string {
	var b strings.Builder
	b.WriteString(heading)
	b.WriteString("\n\n")
	for _, v := range violations {
		fmt.Fprintf(&b, "- **%s**: %s\n", bulletLabel(v), v.Summary)
		if v.FixHint != "" {
			fmt.Fprintf(&b, "  - Fix: %s\n", v.FixHint)
		}
		if len(v.AffectedFiles) > 0 {
			fmt.Fprintf(&b, "  - Files: %s\n", strings.Join(v.AffectedFiles, ", "))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// bulletLabel returns the violation's name, falling back to its rule id.
func bulletLabel(v Violation) string {
	if v.Name != "" {
		return v.Name
	}
	return v.RuleID
}
