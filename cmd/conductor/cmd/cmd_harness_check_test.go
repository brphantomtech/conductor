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

// harnessCheckHarness configures one passing and one failing rule using
// platform-portable trivial shell commands so the smoke test never depends on a
// real toolchain.
func harnessCheckHarness() string {
	return `---
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
harness_rules:
  - id: ok_rule
    name: OK Rule
    severity: warning
    check: "exit 0"
  - id: bad_rule
    name: Bad Rule
    category: complexity
    severity: error
    check: "exit 1"
    fix_hint: split the file
---

## coder

Implement {{ issue.title }}.
`
}

func writeHarnessCheckHarness(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "HARNESS.md")
	require.NoError(t, os.WriteFile(path, []byte(harnessCheckHarness()), 0o600))
	return path
}

func TestHarnessCheck_ReportsViolationsGroupedBySeverity(t *testing.T) {
	harnessPath := writeHarnessCheckHarness(t)
	flags := &harnessCheckFlags{harness: harnessPath, format: outputFormatJSON}

	var out bytes.Buffer
	require.NoError(t, runHarnessCheck(context.Background(), &out, flags, t.TempDir()))

	var rep harnessCheckReport
	require.NoError(t, json.Unmarshal(out.Bytes(), &rep))
	require.Len(t, rep.Violations, 1, "only the failing rule is a violation")
	require.Equal(t, "bad_rule", rep.Violations[0].RuleID)
	require.Equal(t, []string{"Bad Rule"}, rep.BySeverity["error"])
}

func TestHarnessCheck_TextFormat(t *testing.T) {
	harnessPath := writeHarnessCheckHarness(t)
	flags := &harnessCheckFlags{harness: harnessPath, format: outputFormatText}
	var out bytes.Buffer
	require.NoError(t, runHarnessCheck(context.Background(), &out, flags, t.TempDir()))
	require.Contains(t, out.String(), "error (1)")
	require.Contains(t, out.String(), "Bad Rule")
}

func TestHarnessCheck_BadFormat(t *testing.T) {
	flags := &harnessCheckFlags{format: "yaml"}
	err := runHarnessCheck(context.Background(), &bytes.Buffer{}, flags, t.TempDir())
	require.Error(t, err)
}

func TestHarnessCheck_CommandRegistered(t *testing.T) {
	cmd := newHarnessCheckCommand()
	require.Equal(t, "check", cmd.Name())
}
