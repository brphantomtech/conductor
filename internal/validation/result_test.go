package validation

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStatusValid(t *testing.T) {
	tests := []struct {
		name string
		in   Status
		want bool
	}{
		{"passed", StatusPassed, true},
		{"failed", StatusFailed, true},
		{"timeout", StatusTimeout, true},
		{"unknown", Status("bogus"), false},
		{"empty", Status(""), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.in.Valid())
		})
	}
}

func TestStatusJSONRoundTrip(t *testing.T) {
	for _, s := range []Status{StatusPassed, StatusFailed, StatusTimeout} {
		data, err := json.Marshal(s)
		require.NoError(t, err)
		var got Status
		require.NoError(t, json.Unmarshal(data, &got))
		require.Equal(t, s, got)
		require.True(t, got.Valid())
	}
}

func TestFailureReason(t *testing.T) {
	require.Equal(t, "validation_pipeline_failed:lint", FailureReason("lint"))
	require.Equal(t, "validation_pipeline_failed:", FailureReason(""))
}

func TestResultJSONShape(t *testing.T) {
	res := ValidationPipelineResult{
		TurnIndex: 3,
		Checks: []ValidationResult{{
			CheckID:    "unit_tests",
			Name:       "Unit Tests",
			Severity:   "error",
			Status:     StatusPassed,
			ExitCode:   0,
			Output:     "ok",
			DurationMS: 1234,
		}},
	}
	data, err := json.Marshal(res)
	require.NoError(t, err)

	// Round-trips back to the same value.
	var got ValidationPipelineResult
	require.NoError(t, json.Unmarshal(data, &got))
	require.Equal(t, res, got)

	// Field names match the persisted schema.
	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))
	require.Contains(t, raw, "turn_index")
	require.Contains(t, raw, "checks")
	check := raw["checks"].([]any)[0].(map[string]any)
	for _, k := range []string{"check_id", "name", "severity", "status", "exit_code", "output", "duration_ms"} {
		require.Contains(t, check, k)
	}
}
