package harness

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatTechnicalDebt(t *testing.T) {
	violations := []Violation{
		{RuleID: "w1", Name: "long file", Severity: SeverityWarning, Summary: "file too long", FixHint: "split it"},
		{RuleID: "e1", Name: "arch", Severity: SeverityError, Summary: "bad dep"},
	}
	out := FormatTechnicalDebt(violations)
	require.True(t, strings.HasPrefix(out, sectionTechnicalDebt))
	require.Contains(t, out, "long file")
	require.Contains(t, out, "split it")
	require.NotContains(t, out, "arch", "error-severity violation must not appear in debt section")
}

func TestFormatArchitecturalIssues(t *testing.T) {
	violations := []Violation{
		{RuleID: "e1", Name: "arch", Severity: SeverityError, Summary: "bad dep", AffectedFiles: []string{"a.go", "b.go"}},
		{RuleID: "w1", Severity: SeverityWarning, Summary: "debt"},
	}
	out := FormatArchitecturalIssues(violations)
	require.True(t, strings.HasPrefix(out, sectionArchitecturalIssue))
	require.Contains(t, out, "bad dep")
	require.Contains(t, out, "a.go, b.go")
	require.NotContains(t, out, "debt")
}

func TestFormatters_EmptyWhenNoMatch(t *testing.T) {
	violations := []Violation{{Severity: SeverityBlocking, Summary: "x"}}
	require.Empty(t, FormatTechnicalDebt(violations))
	require.Empty(t, FormatArchitecturalIssues(violations))
	require.Empty(t, FormatTechnicalDebt(nil))
}
