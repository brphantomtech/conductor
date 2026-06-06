package harness

// Severity classifies a HarnessRule violation (SPEC §4.1.8). The string values
// match the SPEC `severity` field verbatim so they flow into config, audit
// payloads, and the prompt formatters unchanged.
type Severity string

// Severity levels per SPEC §4.1.8 / §11.2.
const (
	SeverityWarning  Severity = "warning"
	SeverityError    Severity = "error"
	SeverityBlocking Severity = "blocking"
)

// Violation is one detected HarnessRule breach (SPEC §11.2). It carries the
// originating rule's identity plus the fix_hint and any affected files so the
// pre-dispatch formatters, the scheduled GC issue creator, and the audit trail
// all read a single shape.
type Violation struct {
	// RuleID is the HarnessRule.id the violation came from. GC dedup keys on it.
	RuleID string `json:"rule_id"`
	// Name is the human-readable HarnessRule.name.
	Name string `json:"name"`
	// Category is the HarnessRule.category (e.g. "dependency", "complexity").
	Category string `json:"category"`
	// Severity is the rule's severity classification.
	Severity Severity `json:"severity"`
	// Summary is a short description of what failed (rule check output excerpt
	// or layer-violation summary).
	Summary string `json:"summary"`
	// FixHint is the rule's fix_hint, surfaced to agents and GC issues.
	FixHint string `json:"fix_hint,omitempty"`
	// AffectedFiles lists the files implicated by the violation, when known.
	AffectedFiles []string `json:"affected_files,omitempty"`
}

// classifySeverity maps a config severity string to a typed Severity, defaulting
// to SeverityWarning when the value is unrecognized so an unconfigured rule can
// never silently escalate to a blocking halt.
func classifySeverity(s string) Severity {
	switch Severity(s) {
	case SeverityWarning, SeverityError, SeverityBlocking:
		return Severity(s)
	default:
		return SeverityWarning
	}
}
