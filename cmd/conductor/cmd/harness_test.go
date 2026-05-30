package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// validHarness satisfies the parser, the config decoder, and the validator
// against the default routing pipeline {planner, coder, verifier}.
const validHarness = `---
project:
  id: demo
tracker:
  kind: linear
  api_key: token-xyz
  project_slug: demo-team
providers:
  default:
    provider: openrouter
    api_key: sk-test
---

## planner

Plan {{ issue.identifier }}.

## coder

Implement {{ issue.title }}.

## verifier

Verify {{ issue.identifier }}.
`

func writeHarnessFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "HARNESS.md")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func runValidate(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	root := NewRootCommand()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"harness", "validate"}, args...))
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func TestHarnessValidate_ValidFileExitsZero(t *testing.T) {
	t.Parallel()

	path := writeHarnessFile(t, validHarness)
	stdout, _, err := runValidate(t, path)
	require.NoError(t, err)
	require.Contains(t, stdout, "OK ")
	require.Contains(t, stdout, "coder")
}

func TestHarnessValidate_MissingFileExitsNonZero(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "nope.md")
	stdout, _, err := runValidate(t, missing)
	require.Error(t, err)
	// Structured error is printed to stdout with the SPEC sentinel string.
	require.Contains(t, stdout, "missing_harness_file")
}

func TestHarnessValidate_ValidationFailureExitsNonZero(t *testing.T) {
	t.Parallel()

	// Parses and decodes, but the default pipeline expects planner+verifier
	// templates that this harness omits.
	path := writeHarnessFile(t, `---
project:
  id: demo
tracker:
  kind: linear
  api_key: t
  project_slug: x
providers:
  default:
    provider: openrouter
    api_key: sk
---

## coder

Implement.
`)
	stdout, _, err := runValidate(t, path)
	require.Error(t, err)
	require.Contains(t, stdout, "FAIL ")
	require.Contains(t, stdout, "planner")
}

func TestHarnessValidate_JSONOutput(t *testing.T) {
	t.Parallel()

	path := writeHarnessFile(t, validHarness)
	stdout, _, err := runValidate(t, path, "--format", "json")
	require.NoError(t, err)

	var rep struct {
		OK    bool     `json:"ok"`
		Roles []string `json:"roles"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &rep))
	require.True(t, rep.OK)
	require.ElementsMatch(t, []string{"planner", "coder", "verifier"}, rep.Roles)
}

func TestHarnessValidate_EnvVarFallback(t *testing.T) {
	path := writeHarnessFile(t, validHarness)
	t.Setenv("CONDUCTOR_HARNESS_PATH", path)

	// No positional arg and no --harness flag: resolution must fall back to
	// $CONDUCTOR_HARNESS_PATH per SPEC §5.1.
	stdout, _, err := runValidate(t)
	require.NoError(t, err)
	require.Contains(t, stdout, "OK ")
}
