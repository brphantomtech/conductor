package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// validationHarness configures one passing and one failing check using
// platform-portable trivial shell commands so the smoke test never depends on a
// real toolchain.
func validationHarness() string {
	pass := "exit 0"
	fail := "exit 1"
	body := `---
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
validation:
  enabled: true
  checks:
    - id: ok_check
      name: OK Check
      command: "` + pass + `"
      severity: error
      timeout_ms: 5000
    - id: bad_check
      name: Bad Check
      command: "` + fail + `"
      severity: error
      timeout_ms: 5000
---

## coder

Implement {{ issue.title }}.
`
	return body
}

func writeValidationHarness(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "HARNESS.md")
	require.NoError(t, os.WriteFile(path, []byte(validationHarness()), 0o600))
	return path
}

func TestValidationRunReportsClassificationsAndPersists(t *testing.T) {
	harnessPath := writeValidationHarness(t)
	wsDir := t.TempDir()

	flags := &validationRunFlags{
		harness:   harnessPath,
		turnIndex: 2,
		format:    outputFormatJSON,
	}
	var out bytes.Buffer
	require.NoError(t, runValidationRun(context.Background(), &out, flags, wsDir))

	var rep validationRunReport
	require.NoError(t, json.Unmarshal(out.Bytes(), &rep))
	require.Equal(t, 2, rep.TurnIndex)
	require.Len(t, rep.Checks, 2)
	require.Equal(t, "passed", string(rep.Checks[0].Status))
	require.Equal(t, "failed", string(rep.Checks[1].Status))

	// Results persisted at the turn path.
	persisted := filepath.Join(wsDir, ".conductor", "validation", "2.json")
	require.FileExists(t, persisted)
}

func TestValidationRunTextFormat(t *testing.T) {
	harnessPath := writeValidationHarness(t)
	flags := &validationRunFlags{harness: harnessPath, format: outputFormatText}
	var out bytes.Buffer
	require.NoError(t, runValidationRun(context.Background(), &out, flags, t.TempDir()))
	require.Contains(t, out.String(), "PASS OK Check")
	require.Contains(t, out.String(), "FAIL Bad Check")
}

func TestValidationRunBadFormat(t *testing.T) {
	flags := &validationRunFlags{format: "yaml"}
	err := runValidationRun(context.Background(), &bytes.Buffer{}, flags, t.TempDir())
	require.Error(t, err)
}

func TestValidationCommandRegistered(t *testing.T) {
	cmd := newValidationCommand()
	require.Equal(t, "validation", cmd.Name())
	sub, _, err := cmd.Find([]string{"run"})
	require.NoError(t, err)
	require.Equal(t, "run", sub.Name())
}
