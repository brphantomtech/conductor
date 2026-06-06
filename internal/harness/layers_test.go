package harness

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTranslateLayerViolations(t *testing.T) {
	in := []LayerViolationInput{
		{FromPath: "internal/harness/x.go", FromLayer: "domain", ToPath: "internal/api/y.go", ToLayer: "api"},
	}
	got := TranslateLayerViolations(in)
	require.Len(t, got, 1)
	v := got[0]
	require.Equal(t, dependencyCategory, v.Category)
	require.Equal(t, SeverityError, v.Severity)
	require.Equal(t, "layer-dependency:domain->api", v.RuleID)
	require.Contains(t, v.Summary, "domain")
	require.Contains(t, v.Summary, "api")
	require.ElementsMatch(t, []string{"internal/harness/x.go", "internal/api/y.go"}, v.AffectedFiles)
}

func TestTranslateLayerViolations_Empty(t *testing.T) {
	require.Empty(t, TranslateLayerViolations(nil))
}

func TestDedupeFiles(t *testing.T) {
	require.Equal(t, []string{"a", "b"}, dedupeFiles("a", "", "b", "a"))
}
