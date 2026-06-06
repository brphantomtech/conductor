package validation

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFailTurn(t *testing.T) {
	errFail := ValidationResult{CheckID: "lint", Severity: "error", Status: StatusFailed}
	warnFail := ValidationResult{CheckID: "fmt", Severity: "warning", Status: StatusFailed}
	errTimeout := ValidationResult{CheckID: "tests", Severity: "error", Status: StatusTimeout}
	errPass := ValidationResult{CheckID: "vet", Severity: "error", Status: StatusPassed}
	blockFail := ValidationResult{CheckID: "build", Severity: "blocking", Status: StatusFailed}

	tests := []struct {
		name       string
		results    []ValidationResult
		threshold  string
		wantFailed bool
		wantReason string
	}{
		{"at-threshold error fails", []ValidationResult{errFail}, "error", true, "validation_pipeline_failed:lint"},
		{"timeout counts as failure", []ValidationResult{errTimeout}, "error", true, "validation_pipeline_failed:tests"},
		{"below-threshold warning does not fail", []ValidationResult{warnFail}, "error", false, ""},
		{"above-threshold blocking fails", []ValidationResult{blockFail}, "error", true, "validation_pipeline_failed:build"},
		{"passing check never fails", []ValidationResult{errPass}, "error", false, ""},
		{"unset threshold never fails", []ValidationResult{errFail, blockFail}, "", false, ""},
		{"unknown threshold never fails", []ValidationResult{errFail}, "bogus", false, ""},
		{"warning threshold catches warning", []ValidationResult{warnFail}, "warning", true, "validation_pipeline_failed:fmt"},
		{"first failing at/above threshold wins", []ValidationResult{errPass, warnFail, errFail, blockFail}, "error", true, "validation_pipeline_failed:lint"},
		{"empty results", nil, "error", false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failed, reason := failTurn(tt.results, tt.threshold)
			require.Equal(t, tt.wantFailed, failed)
			require.Equal(t, tt.wantReason, reason)

			// Method form delegates to the same decision.
			mFailed, mReason := ValidationPipelineResult{Checks: tt.results}.FailTurn(tt.threshold)
			require.Equal(t, tt.wantFailed, mFailed)
			require.Equal(t, tt.wantReason, mReason)
		})
	}
}
