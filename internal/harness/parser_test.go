package harness

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseBytes_FullDocument(t *testing.T) {
	src := strings.Join([]string{
		"---",
		"project:",
		"  id: demo",
		"  name: Demo",
		"tracker:",
		"  kind: linear",
		"---",
		"",
		"## planner",
		"",
		"Plan body line 1.",
		"Plan body line 2.",
		"",
		"## coder",
		"",
		"Coder body.",
		"",
		"## verifier",
		"",
		"Verify body.",
		"",
		"## reviewer",
		"",
		"Reviewer body.",
		"",
	}, "\n")

	def, err := parseBytes([]byte(src), "test.md")
	require.NoError(t, err)

	require.NotNil(t, def.FrontMatter)
	project, ok := def.FrontMatter["project"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "demo", project["id"])

	require.Contains(t, def.PromptTemplates, "planner")
	require.Contains(t, def.PromptTemplates, "coder")
	require.Contains(t, def.PromptTemplates, "verifier")
	require.Contains(t, def.PromptTemplates, "reviewer")
	require.Contains(t, def.PromptTemplates["planner"], "Plan body line 1.")
	require.Contains(t, def.PromptTemplates["coder"], "Coder body.")
}

func TestParseBytes_NoFrontMatter(t *testing.T) {
	src := "## coder\n\nThe body of the coder role.\n"

	def, err := parseBytes([]byte(src), "")
	require.NoError(t, err)
	require.Empty(t, def.FrontMatter)
	require.Equal(t, "The body of the coder role.", def.PromptTemplates["coder"])
}

func TestParseBytes_BodyWithoutHeadingsBecomesCoder(t *testing.T) {
	src := "Just some text\nacross two lines\n"

	def, err := parseBytes([]byte(src), "")
	require.NoError(t, err)
	require.Equal(t, "Just some text\nacross two lines", def.PromptTemplates[DefaultRole])
	// And no other roles materialized.
	require.Len(t, def.PromptTemplates, 1)
}

func TestParseBytes_NonMapFrontMatterReportsShapeError(t *testing.T) {
	src := strings.Join([]string{
		"---",
		"- one",
		"- two",
		"---",
		"## coder",
		"",
		"Body.",
	}, "\n")

	_, err := parseBytes([]byte(src), "")
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrHarnessFrontMatterShape), "expected ErrHarnessFrontMatterShape, got %v", err)
}

func TestParseBytes_MissingClosingDelimiterIsParseError(t *testing.T) {
	src := "---\nproject:\n  id: foo\n## coder\nbody\n"

	_, err := parseBytes([]byte(src), "")
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrHarnessParse), "expected ErrHarnessParse, got %v", err)
}

func TestParseBytes_UnknownFrontMatterKeysAreKept(t *testing.T) {
	src := strings.Join([]string{
		"---",
		"project:",
		"  id: demo",
		"experimental_setting: true",
		"---",
		"## coder",
		"body",
	}, "\n")

	def, err := parseBytes([]byte(src), "")
	require.NoError(t, err)
	require.Equal(t, true, def.FrontMatter["experimental_setting"])
}

func TestParseBytes_EmptyFrontMatterBlock(t *testing.T) {
	src := "---\n---\n## coder\nbody\n"

	def, err := parseBytes([]byte(src), "")
	require.NoError(t, err)
	require.NotNil(t, def.FrontMatter)
	require.Empty(t, def.FrontMatter)
	require.Equal(t, "body", def.PromptTemplates["coder"])
}

func TestParseBytes_BOMAndBlankPrefixToleratedOnlyForBOM(t *testing.T) {
	src := "\ufeff" + "---\nproject:\n  id: demo\n---\n## coder\nbody"

	def, err := parseBytes([]byte(src), "")
	require.NoError(t, err)
	project, ok := def.FrontMatter["project"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "demo", project["id"])
}

func TestParseBytes_RoleHeadingWithTrailingComment(t *testing.T) {
	src := strings.Join([]string{
		"---",
		"project:",
		"  id: demo",
		"---",
		"## planner (the planning step)",
		"plan body",
		"## coder",
		"code body",
	}, "\n")

	def, err := parseBytes([]byte(src), "")
	require.NoError(t, err)
	require.Equal(t, "plan body", def.PromptTemplates["planner"])
	require.Equal(t, "code body", def.PromptTemplates["coder"])
}

func TestParseBytes_LevelThreeHeadingDoesNotSplit(t *testing.T) {
	src := strings.Join([]string{
		"## coder",
		"intro",
		"### subsection",
		"more body",
	}, "\n")

	def, err := parseBytes([]byte(src), "")
	require.NoError(t, err)
	require.Contains(t, def.PromptTemplates["coder"], "intro")
	require.Contains(t, def.PromptTemplates["coder"], "### subsection")
	require.Contains(t, def.PromptTemplates["coder"], "more body")
}

func TestParse_ReadsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "HARNESS.md")
	body := "---\nproject:\n  id: ondisk\n---\n## coder\nbody"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))

	def, err := Parse(path)
	require.NoError(t, err)
	require.Equal(t, path, def.SourcePath)
	project, ok := def.FrontMatter["project"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "ondisk", project["id"])
}

func TestParse_MissingFileWrapsParseError(t *testing.T) {
	_, err := Parse(filepath.Join(t.TempDir(), "nope.md"))
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrHarnessParse))
}
