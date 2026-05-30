package harness_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/harness"
)

func TestParseBytes_FullDocument(t *testing.T) {
	t.Parallel()

	src := []byte(`---
project:
  id: demo
tracker:
  kind: linear
  api_key: $LINEAR_TOKEN
---

## planner

Plan the work for {{ issue.identifier }}.

## coder

Implement the plan.

## verifier

Verify {{ issue.identifier }} passes its acceptance criteria.
`)

	def, err := harness.ParseBytes(src)
	require.NoError(t, err)
	require.NotNil(t, def)

	require.Equal(t, "demo", def.FrontMatter["project"].(map[string]any)["id"])
	require.Equal(t, "linear", def.FrontMatter["tracker"].(map[string]any)["kind"])

	require.Equal(t, "Plan the work for {{ issue.identifier }}.", def.PromptTemplates["planner"])
	require.Equal(t, "Implement the plan.", def.PromptTemplates["coder"])
	require.Contains(t, def.PromptTemplates["verifier"], "{{ issue.identifier }}")
}

func TestParseBytes_NoFrontMatter(t *testing.T) {
	t.Parallel()

	src := []byte(`Implement {{ issue.title }}.

This is a single-role harness.
`)

	def, err := harness.ParseBytes(src)
	require.NoError(t, err)
	require.Empty(t, def.FrontMatter)
	require.Contains(t, def.PromptTemplates, harness.DefaultRole,
		"body without `## ` headings must default to the coder role (SPEC §5.2)")
	require.Contains(t, def.PromptTemplates[harness.DefaultRole], "{{ issue.title }}")
}

func TestParseBytes_FrontMatterOnly(t *testing.T) {
	t.Parallel()

	src := []byte(`---
project:
  id: demo
---
`)

	def, err := harness.ParseBytes(src)
	require.NoError(t, err)
	require.Equal(t, "demo", def.FrontMatter["project"].(map[string]any)["id"])
	require.Empty(t, def.PromptTemplates,
		"empty body must produce no prompt templates rather than a stub coder entry")
}

func TestParseBytes_UnclosedFrontMatter(t *testing.T) {
	t.Parallel()

	src := []byte(`---
project:
  id: demo

## coder

body without a closing fence
`)

	_, err := harness.ParseBytes(src)
	require.Error(t, err)
	require.True(t, errors.Is(err, harness.ErrHarnessParse),
		"unclosed front matter must surface as ErrHarnessParse, got %v", err)
}

func TestParseBytes_NonMapFrontMatter(t *testing.T) {
	t.Parallel()

	src := []byte(`---
- this
- is
- a
- list
---

## coder

body
`)

	_, err := harness.ParseBytes(src)
	require.Error(t, err)
	require.True(t, errors.Is(err, harness.ErrHarnessFrontMatterShape),
		"non-map root must surface as ErrHarnessFrontMatterShape, got %v", err)
}

func TestParseBytes_MalformedYAML(t *testing.T) {
	t.Parallel()

	src := []byte("---\nproject:\n  id: \"unterminated\n---\n\n## coder\nbody\n")

	_, err := harness.ParseBytes(src)
	require.Error(t, err)
	require.True(t, errors.Is(err, harness.ErrHarnessParse),
		"yaml syntax error must surface as ErrHarnessParse, got %v", err)
}

func TestParseBytes_NestedHeadingsStayInsideSection(t *testing.T) {
	t.Parallel()

	src := []byte(`## planner

Outline:

### Step 1

Plan A.

### Step 2

Plan B.

## coder

Build it.
`)

	def, err := harness.ParseBytes(src)
	require.NoError(t, err)
	require.Contains(t, def.PromptTemplates["planner"], "### Step 1",
		"### sub-headings must stay within the parent ## role section")
	require.Contains(t, def.PromptTemplates["planner"], "### Step 2")
	require.Equal(t, "Build it.", def.PromptTemplates["coder"])
}

func TestParseBytes_RoleNameLowercased(t *testing.T) {
	t.Parallel()

	src := []byte(`## Planner

Plan.

## VERIFIER

Verify.
`)

	def, err := harness.ParseBytes(src)
	require.NoError(t, err)
	require.Contains(t, def.PromptTemplates, "planner",
		"role names must be lowercased per SPEC §4.2 normalization rules")
	require.Contains(t, def.PromptTemplates, "verifier")
}

func TestParseBytes_EmptyFrontMatterBlock(t *testing.T) {
	t.Parallel()

	src := []byte(`---
---

## coder

body
`)

	def, err := harness.ParseBytes(src)
	require.NoError(t, err)
	require.NotNil(t, def.FrontMatter,
		"empty front matter must decode to an empty (non-nil) map")
	require.Empty(t, def.FrontMatter)
}

func TestParseFile_RoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "HARNESS.md")
	require.NoError(t, os.WriteFile(path, []byte(`---
project: { id: demo }
---

## coder

Hello.
`), 0o600))

	def, err := harness.ParseFile(path)
	require.NoError(t, err)
	require.Equal(t, path, def.Source,
		"ParseFile must populate Source so reload paths can compare against it")
	require.Equal(t, "Hello.", def.PromptTemplates["coder"])
}

func TestParseFile_Missing(t *testing.T) {
	t.Parallel()

	_, err := harness.ParseFile(filepath.Join(t.TempDir(), "does-not-exist.md"))
	require.Error(t, err)
	require.True(t, errors.Is(err, harness.ErrMissingHarnessFile),
		"missing file must classify as ErrMissingHarnessFile, got %v", err)
}
