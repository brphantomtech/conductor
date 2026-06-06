package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStatusStaticSnapshotText(t *testing.T) {
	root := t.TempDir()
	makeWorkspace(t, root, "ABC-1", "ABC-1")
	cfg := testWorkspaceConfig(root)
	cfg.Tracker.Kind = "linear"
	cfg.Providers.Default.Provider = "openrouter"

	// Probe storage against a temp working dir so conductor.db lands there.
	t.Chdir(t.TempDir())

	var out bytes.Buffer
	require.NoError(t, runStatus(context.Background(), &out, cfg, outputFormatText))
	s := out.String()
	require.Contains(t, s, "== config ==")
	require.Contains(t, s, "linear")
	require.Contains(t, s, "openrouter")
	require.Contains(t, s, "== workspaces ==")
	require.Contains(t, s, "count: 1")
	require.Contains(t, s, "== storage ==")
	require.Contains(t, s, "== live ==")
	require.Contains(t, s, "Phase 14")
}

func TestStatusStaticSnapshotJSON(t *testing.T) {
	root := t.TempDir()
	makeWorkspace(t, root, "ABC-1", "ABC-1")
	cfg := testWorkspaceConfig(root)
	t.Chdir(t.TempDir())

	var out bytes.Buffer
	require.NoError(t, runStatus(context.Background(), &out, cfg, outputFormatJSON))

	var snap statusSnapshot
	require.NoError(t, json.Unmarshal(out.Bytes(), &snap))
	require.Equal(t, 1, snap.Workspace.Count)
	require.True(t, snap.Storage.Reachable, "storage should be reachable: %s", snap.Storage.Detail)
	require.False(t, snap.Live.Available)
	require.Contains(t, snap.Live.Notice, "Phase 14")
}

func TestStatusBadFormat(t *testing.T) {
	cfg := testWorkspaceConfig(t.TempDir())
	require.Error(t, runStatus(context.Background(), &bytes.Buffer{}, cfg, "yaml"))
}

func TestStatusCommandRegistered(t *testing.T) {
	cmd := newStatusCommand()
	require.Equal(t, "status", cmd.Name())
	require.NotNil(t, cmd.Flags().Lookup("format"))
}
