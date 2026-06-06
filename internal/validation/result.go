package validation

import "fmt"

// Status is the classification of a single check's outcome (SPEC §15.2 step 3).
type Status string

// The three check classifications defined by SPEC §15.2. The string values are
// persisted verbatim in .conductor/validation/<turn_index>.json and surfaced in
// the ValidationCheckRun audit payload — do not change them without a migration.
const (
	StatusPassed  Status = "passed"
	StatusFailed  Status = "failed"
	StatusTimeout Status = "timeout"
)

// Valid reports whether s is one of the three defined classifications.
func (s Status) Valid() bool {
	switch s {
	case StatusPassed, StatusFailed, StatusTimeout:
		return true
	default:
		return false
	}
}

// failureReasonPrefix is the SPEC §15.3 turn-failure reason stem. The full
// reason is "validation_pipeline_failed:<check_id>".
const failureReasonPrefix = "validation_pipeline_failed"

// FailureReason formats the SPEC §15.3 turn-failure reason for a check id.
func FailureReason(checkID string) string {
	return fmt.Sprintf("%s:%s", failureReasonPrefix, checkID)
}

// ValidationResult is the outcome of one check (SPEC §4.1.12). It is both the
// in-memory element of RunAttempt.validation_results and the on-disk record
// persisted per turn.
//
//revive:disable-next-line:exported // SPEC §4.1.12 names this entity ValidationResult — keep the canonical name.
type ValidationResult struct {
	// CheckID is the configured check id (validation.checks[].id).
	CheckID string `json:"check_id"`
	// Name is the human-readable check name (validation.checks[].name).
	Name string `json:"name"`
	// Severity is the configured check severity (validation.checks[].severity).
	Severity string `json:"severity"`
	// Status is the classification: passed, failed, or timeout.
	Status Status `json:"status"`
	// ExitCode is the process exit code; -1 when the check timed out or never
	// produced an exit status.
	ExitCode int `json:"exit_code"`
	// Output is the combined stdout+stderr, truncated to output_max_bytes.
	Output string `json:"output"`
	// DurationMS is the wall-clock execution time in milliseconds.
	DurationMS int64 `json:"duration_ms"`
}

// ValidationPipelineResult is the ordered per-check result set for one turn
// (SPEC §15.2). It populates RunAttempt.validation_results and is marshaled to
// .conductor/validation/<turn_index>.json.
//
//revive:disable-next-line:exported // mirrors the SPEC §15 pipeline result naming.
type ValidationPipelineResult struct {
	// TurnIndex is the 0-based turn this result set belongs to.
	TurnIndex int `json:"turn_index"`
	// Checks is the per-check results in execution order.
	Checks []ValidationResult `json:"checks"`
}
