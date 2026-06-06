package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/config"
)

// makeWorkspace materializes a fake workspace under root: a key directory with a
// .conductor/meta.json and an optional repo subdirectory. It mirrors the layout
// the workspace.Manager creates so the CLI enumerates it the same way.
func makeWorkspace(t *testing.T, root, key, identifier string, repos ...string) {
	t.Helper()
	conductor := filepath.Join(root, key, ".conductor")
	require.NoError(t, os.MkdirAll(conductor, 0o755))
	meta := map[string]any{
		"key":              key,
		"issue_id":         "id-" + key,
		"issue_identifier": identifier,
	}
	data, err := json.Marshal(meta)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(conductor, "meta.json"), data, 0o644))
	for _, r := range repos {
		require.NoError(t, os.MkdirAll(filepath.Join(root, key, r), 0o755))
	}
}

func testWorkspaceConfig(root string) config.Config {
	cfg := config.Defaults()
	cfg.Workspace.Root = root
	cfg.Project.ID = "demo"
	return cfg
}

func TestWorkspaceListReportsWorkspaces(t *testing.T) {
	root := t.TempDir()
	makeWorkspace(t, root, "ABC-1", "ABC-1", "service")
	makeWorkspace(t, root, "ABC-2", "ABC-2")
	cfg := testWorkspaceConfig(root)

	var out bytes.Buffer
	require.NoError(t, runWorkspaceList(&out, cfg, outputFormatText))
	s := out.String()
	require.Contains(t, s, "ABC-1")
	require.Contains(t, s, "ABC-2")
	require.Contains(t, s, "repos=1")
}

func TestWorkspaceListJSON(t *testing.T) {
	root := t.TempDir()
	makeWorkspace(t, root, "ABC-1", "ABC-1", "service")
	cfg := testWorkspaceConfig(root)

	var out bytes.Buffer
	require.NoError(t, runWorkspaceList(&out, cfg, outputFormatJSON))

	var rep struct {
		Root       string           `json:"root"`
		Workspaces []workspaceEntry `json:"workspaces"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &rep))
	require.Equal(t, root, rep.Root)
	require.Len(t, rep.Workspaces, 1)
	require.Equal(t, "ABC-1", rep.Workspaces[0].Key)
	require.Equal(t, "ABC-1", rep.Workspaces[0].IssueIdentifier)
	require.Equal(t, []string{"service"}, rep.Workspaces[0].Repos)
}

func TestWorkspaceListEmptyRoot(t *testing.T) {
	cfg := testWorkspaceConfig(filepath.Join(t.TempDir(), "does-not-exist"))
	var out bytes.Buffer
	require.NoError(t, runWorkspaceList(&out, cfg, outputFormatText))
	require.Contains(t, out.String(), "no workspaces")
}

func TestWorkspaceListBadFormat(t *testing.T) {
	cfg := testWorkspaceConfig(t.TempDir())
	require.Error(t, runWorkspaceList(&bytes.Buffer{}, cfg, "yaml"))
}

func TestWorkspaceRemoveDeletesViaManager(t *testing.T) {
	root := t.TempDir()
	makeWorkspace(t, root, "ABC-1", "ABC-1")
	cfg := testWorkspaceConfig(root)

	var out bytes.Buffer
	require.NoError(t, runWorkspaceRemove(context.Background(), &out, cfg, "ABC-1", true))
	require.Contains(t, out.String(), "removed workspace ABC-1")

	// Gone from disk and from list.
	_, err := os.Stat(filepath.Join(root, "ABC-1"))
	require.True(t, os.IsNotExist(err))

	entries, err := enumerateWorkspaces(cfg)
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestWorkspaceRemoveRequiresYes(t *testing.T) {
	root := t.TempDir()
	makeWorkspace(t, root, "ABC-1", "ABC-1")
	cfg := testWorkspaceConfig(root)

	err := runWorkspaceRemove(context.Background(), &bytes.Buffer{}, cfg, "ABC-1", false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "--yes")
	// Workspace must still exist.
	require.DirExists(t, filepath.Join(root, "ABC-1"))
}

func TestWorkspaceRemoveUnknownKey(t *testing.T) {
	cfg := testWorkspaceConfig(t.TempDir())
	err := runWorkspaceRemove(context.Background(), &bytes.Buffer{}, cfg, "NOPE-9", true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no workspace with key")
}

func TestWorkspaceOpenWithoutEditorPrintsPath(t *testing.T) {
	root := t.TempDir()
	makeWorkspace(t, root, "ABC-1", "ABC-1")
	cfg := testWorkspaceConfig(root)

	lookup := func(string) (string, bool) { return "", false }
	var out bytes.Buffer
	require.NoError(t, runWorkspaceOpen(&out, cfg, "ABC-1", lookup))
	require.Contains(t, out.String(), "$EDITOR not set")
	require.Contains(t, out.String(), filepath.Join(root, "ABC-1"))
}

func TestWorkspaceOpenMissingWorkspace(t *testing.T) {
	cfg := testWorkspaceConfig(t.TempDir())
	lookup := func(string) (string, bool) { return "", false }
	err := runWorkspaceOpen(&bytes.Buffer{}, cfg, "GONE-1", lookup)
	require.Error(t, err)
}

func TestWorkspaceCommandRegistered(t *testing.T) {
	cmd := newWorkspaceCommand()
	require.Equal(t, "workspace", cmd.Name())
	for _, name := range []string{"list", "remove", "open"} {
		sub, _, err := cmd.Find([]string{name})
		require.NoError(t, err)
		require.Equal(t, name, sub.Name())
	}
}
