package cmd

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// runCmd executes a freshly built command tree with args and returns combined
// stdout/stderr and the error. It exercises the RunE closures and config
// loading the unit tests bypass.
func runCmd(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	t.Chdir(dir)
	root := &cobra.Command{Use: "conductor", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(newWorkspaceCommand())
	root.AddCommand(newInitCommand())
	root.AddCommand(newStatusCommand())
	root.AddCommand(newDispatchCommand())
	root.AddCommand(newCancelCommand())
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return buf.String(), err
}

func TestSurfaceInitThenListThenStatus(t *testing.T) {
	dir := t.TempDir()

	// init local scaffolds HARNESS.md in cwd.
	out, err := runCmd(t, dir, "init", "--profile", "local")
	require.NoError(t, err)
	require.Contains(t, out, "wrote HARNESS.md")
	require.FileExists(t, filepath.Join(dir, "HARNESS.md"))

	// The scaffolded harness references unset secrets, so the workspace/status
	// commands fall back to defaults config when load fails — they must still run.
	t.Setenv("LINEAR_API_KEY", "lin_test")
	t.Setenv("OPENROUTER_API_KEY", "sk-or-test")

	// list runs (no workspaces under the default temp root yet — just must not error).
	_, err = runCmd(t, dir, "workspace", "list")
	require.NoError(t, err)

	// status prints the static snapshot.
	out, err = runCmd(t, dir, "status")
	require.NoError(t, err)
	require.Contains(t, out, "== config ==")
	require.Contains(t, out, "Phase 14")
}

func TestSurfaceDispatchReportsRequirement(t *testing.T) {
	dir := t.TempDir()
	out, err := runCmd(t, dir, "dispatch", "ABC-1")
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrRequiresRunningService))
	require.Contains(t, out, "not performed")
}

func TestSurfaceCancelReportsRequirement(t *testing.T) {
	dir := t.TempDir()
	out, err := runCmd(t, dir, "cancel", "ABC-1")
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrRequiresRunningService))
	require.Contains(t, out, "cancel ABC-1")
}
