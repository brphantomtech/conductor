package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/harness"
)

const goodHarness = `---
project:
  id: demo
tracker:
  kind: linear
  api_key: secret
  project_slug: team
providers:
  default:
    provider: anthropic
    api_key: ank
routing:
  pipeline: [planner, coder, verifier]
---

## planner

p

## coder

c

## verifier

v
`

func writeTempHarness(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "HARNESS.md")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	return p
}

func TestRunHarnessValidate_MissingFile(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist.md")

	var stdout, stderr bytes.Buffer
	err := runHarnessValidate(&stdout, &stderr, missing)
	require.Error(t, err)
	require.True(t, errors.Is(err, harness.ErrMissingHarnessFile))
	require.Contains(t, stderr.String(), "missing_harness_file")
}

func TestRunHarnessValidate_ValidFile(t *testing.T) {
	path := writeTempHarness(t, goodHarness)

	var stdout, stderr bytes.Buffer
	err := runHarnessValidate(&stdout, &stderr, path)
	require.NoError(t, err)
	out := stdout.String()
	require.True(t, strings.HasPrefix(out, "OK:"), "expected stdout to begin with OK, got %q", out)
	require.Contains(t, out, "templates [coder, planner, verifier]")
	require.Empty(t, stderr.String())
}

func TestRunHarnessValidate_MultiErrorFilePrintsAllProblems(t *testing.T) {
	// No project.id, no api_key, no provider, pipeline references missing role.
	body := `---
tracker:
  kind: linear
  project_slug: team
providers:
  default:
    provider: ""
routing:
  pipeline: [planner, coder, verifier, reviewer]
---

## planner

p

## coder

c

## verifier

v
`
	path := writeTempHarness(t, body)

	var stdout, stderr bytes.Buffer
	err := runHarnessValidate(&stdout, &stderr, path)
	require.Error(t, err)
	out := stderr.String()

	for _, want := range []string{
		"project_id_missing",
		"tracker_api_key_missing",
		"provider_unsupported",
		"pipeline_role_missing_template",
	} {
		require.Contains(t, out, want, "expected stderr to mention %q, got:\n%s", want, out)
	}
}

func TestRunHarnessValidate_BadTemplateSyntaxClassifiedAsTemplateParse(t *testing.T) {
	body := `---
project:
  id: demo
tracker:
  kind: linear
  api_key: secret
  project_slug: team
providers:
  default:
    provider: anthropic
    api_key: ank
routing:
  pipeline: [planner, coder, verifier]
---

## planner

{{ }}

## coder

c

## verifier

v
`
	path := writeTempHarness(t, body)

	var stdout, stderr bytes.Buffer
	err := runHarnessValidate(&stdout, &stderr, path)
	require.Error(t, err)
	require.Contains(t, stderr.String(), "template_parse_error")
}

func TestFlatten_JoinedErrors(t *testing.T) {
	joined := errors.Join(errors.New("a"), errors.New("b"), errors.Join(errors.New("c"), errors.New("d")))
	out := flatten(joined)
	require.Len(t, out, 4)
}
