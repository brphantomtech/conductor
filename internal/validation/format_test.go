package validation

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestFormatContextEmpty(t *testing.T) {
	require.Equal(t, "", FormatContext(nil, 4096))
	require.Equal(t, "", FormatContext([]ValidationResult{}, 4096))
}

func TestFormatContextLayout(t *testing.T) {
	results := []ValidationResult{
		{CheckID: "unit_tests", Name: "Unit Tests", Status: StatusPassed},
		{CheckID: "lint", Name: "Lint", Status: StatusFailed, Output: "src/a.ts:1 error"},
		{CheckID: "type_check", Name: "Type Check", Status: StatusTimeout, Output: "ran out of time"},
	}
	out := FormatContext(results, 4096)

	require.True(t, strings.HasPrefix(out, "## Validation Results (Previous Turn)"))
	require.Contains(t, out, "✅ Unit Tests — passed")
	require.Contains(t, out, "❌ Lint — failed")
	require.Contains(t, out, "⚠️ Type Check — timeout")

	// Failure-detail sections for the failed and timed-out checks only.
	require.Contains(t, out, "### Lint failures")
	require.Contains(t, out, "src/a.ts:1 error")
	require.Contains(t, out, "### Type Check failures")
	require.NotContains(t, out, "### Unit Tests failures")
}

func TestFormatContextLabelFallsBackToID(t *testing.T) {
	out := FormatContext([]ValidationResult{
		{CheckID: "vet", Status: StatusFailed, Output: "boom"},
	}, 4096)
	require.Contains(t, out, "❌ vet — failed")
	require.Contains(t, out, "### vet failures")
}

func TestFormatContextTruncation(t *testing.T) {
	long := strings.Repeat("x", 10000)
	out := FormatContext([]ValidationResult{
		{CheckID: "lint", Name: "Lint", Status: StatusFailed, Output: long},
	}, 200)
	require.LessOrEqual(t, len(out), 200)
	require.Contains(t, out, "[... truncated to 200 bytes]")
}

func TestFormatContextTruncationRuneSafe(t *testing.T) {
	// Output full of multi-byte glyphs; truncation must not split a rune.
	out := FormatContext([]ValidationResult{
		{CheckID: "lint", Status: StatusFailed, Output: strings.Repeat("✅", 500)},
	}, 120)
	require.LessOrEqual(t, len(out), 120)
	require.True(t, utf8.ValidString(out), "truncated output must remain valid UTF-8")
}

func TestFormatContextNoBudget(t *testing.T) {
	results := []ValidationResult{{CheckID: "lint", Status: StatusFailed, Output: "detail"}}
	out := FormatContext(results, 0)
	require.Contains(t, out, "detail")
	require.NotContains(t, out, "truncated")
}

func TestFormatContextMethod(t *testing.T) {
	r := ValidationPipelineResult{Checks: []ValidationResult{{CheckID: "x", Status: StatusPassed}}}
	require.Equal(t, FormatContext(r.Checks, 4096), r.FormatContext(4096))
}
