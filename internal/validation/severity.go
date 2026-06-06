package validation

import "strings"

// severityRank orders the SPEC §4.1.8 severities so a fail_on_severity
// threshold can be compared with ">=". Unknown severities rank below warning so
// a misconfigured check never spuriously fails a turn above the threshold.
func severityRank(sev string) int {
	switch strings.ToLower(strings.TrimSpace(sev)) {
	case "warning":
		return 1
	case "error":
		return 2
	case "blocking":
		return 3
	default:
		return 0
	}
}

// failTurn implements the SPEC §15.3 severity decision as a pure function. When
// failOnSeverity is set, the first check at or above the threshold that failed
// or timed out marks the turn failed with reason
// "validation_pipeline_failed:<check_id>". An unset (or unrecognized) threshold
// never fails the turn.
func failTurn(results []ValidationResult, failOnSeverity string) (failed bool, reason string) {
	threshold := severityRank(failOnSeverity)
	if threshold == 0 {
		return false, ""
	}
	for _, r := range results {
		if r.Status != StatusFailed && r.Status != StatusTimeout {
			continue
		}
		if severityRank(r.Severity) >= threshold {
			return true, FailureReason(r.CheckID)
		}
	}
	return false, ""
}

// FailTurn reports whether the pipeline result fails the turn under the
// configured fail_on_severity threshold, returning the SPEC §15.3 failure
// reason when it does. It is the exported entry point the orchestrator's
// turn-failure path consumes; the decision itself is pure.
func (r ValidationPipelineResult) FailTurn(failOnSeverity string) (failed bool, reason string) {
	return failTurn(r.Checks, failOnSeverity)
}
