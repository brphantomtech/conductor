package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/audit"
	"github.com/conductor-sh/conductor/internal/config"
)

// fakeCloner materializes a repo by just creating its directory — no network.
func fakeCloner(_ context.Context, _ config.WorkspaceRepo, dest string) error {
	return os.MkdirAll(dest, 0o755)
}

func newTestManager(t *testing.T, cfg config.Workspace, hooks config.Hooks, opts ...Option) *Manager {
	t.Helper()
	all := append([]Option{withCloner(fakeCloner)}, opts...)
	return New(cfg, hooks, all...)
}

func TestCreate_LayoutAndSkeleton(t *testing.T) {
	root := t.TempDir()
	m := newTestManager(t, config.Workspace{Root: root}, config.Hooks{})

	ws, err := m.Create(context.Background(), "issue-1", "ABC-123", nil)
	require.NoError(t, err)

	require.Equal(t, "ABC-123", ws.Key)
	require.DirExists(t, ws.Path)
	require.DirExists(t, ws.ConductorDir)
	require.DirExists(t, ws.ValidationDir)
	require.FileExists(t, ws.MetaPath)

	rel, err := filepath.Rel(filepath.Clean(root), ws.Path)
	require.NoError(t, err)
	require.Equal(t, "ABC-123", rel)

	data, err := os.ReadFile(ws.MetaPath)
	require.NoError(t, err)
	var meta map[string]any
	require.NoError(t, json.Unmarshal(data, &meta))
	require.Equal(t, "ABC-123", meta["issue_identifier"])
	require.Equal(t, "issue-1", meta["issue_id"])
}

func TestCreate_SanitizesKey(t *testing.T) {
	m := newTestManager(t, config.Workspace{Root: t.TempDir()}, config.Hooks{})
	ws, err := m.Create(context.Background(), "id", "feature/login", nil)
	require.NoError(t, err)
	require.Equal(t, "feature_login", ws.Key)
	require.DirExists(t, ws.Path)
}

func TestCreate_RejectsTraversalKey(t *testing.T) {
	m := newTestManager(t, config.Workspace{Root: t.TempDir()}, config.Hooks{})
	_, err := m.Create(context.Background(), "id", "..", nil)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrUnsafePath))
	require.True(t, errors.Is(err, ErrWorkspaceCreationFailed))
}

func TestCreate_RejectsEmptyIdentifier(t *testing.T) {
	m := newTestManager(t, config.Workspace{Root: t.TempDir()}, config.Hooks{})
	_, err := m.Create(context.Background(), "id", "", nil)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrUnsafePath))
}

func TestCreate_RejectsUnconfiguredRoot(t *testing.T) {
	m := newTestManager(t, config.Workspace{Root: ""}, config.Hooks{})
	_, err := m.Create(context.Background(), "id", "ABC-1", nil)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrWorkspaceCreationFailed))
}

func TestCreate_MultiRepo(t *testing.T) {
	root := t.TempDir()
	var cloned []string
	cloner := func(ctx context.Context, repo config.WorkspaceRepo, dest string) error {
		cloned = append(cloned, repo.Name)
		return os.MkdirAll(dest, 0o755)
	}
	m := New(
		config.Workspace{Root: root, Repos: []config.WorkspaceRepo{
			{Name: "app", URL: "https://example.com/app.git"},
			{Name: "lib", URL: "https://example.com/lib.git"},
		}},
		config.Hooks{},
		withCloner(cloner),
	)

	ws, err := m.Create(context.Background(), "id", "ABC-1", nil)
	require.NoError(t, err)
	require.Len(t, ws.Repos, 2)
	require.Equal(t, []string{"app", "lib"}, cloned)
	require.Equal(t, filepath.Join(ws.Path, "app"), ws.Repos[0].Path)
	require.DirExists(t, filepath.Join(ws.Path, "app"))
	require.DirExists(t, filepath.Join(ws.Path, "lib"))
}

func TestRemove_DeletesWorkspace(t *testing.T) {
	m := newTestManager(t, config.Workspace{Root: t.TempDir()}, config.Hooks{})
	ws, err := m.Create(context.Background(), "id", "ABC-1", nil)
	require.NoError(t, err)

	require.NoError(t, m.Remove(context.Background(), ws, nil))
	require.NoDirExists(t, ws.Path)
}

func TestRemove_RefusesOutsideRoot(t *testing.T) {
	m := newTestManager(t, config.Workspace{Root: t.TempDir()}, config.Hooks{})
	outside := t.TempDir() // a real dir, but not under the manager's root
	evil := &Workspace{Key: "evil", Path: outside}

	err := m.Remove(context.Background(), evil, nil)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrUnsafePath))
	require.DirExists(t, outside, "a path outside the root must not be deleted")
}

func TestCreate_AfterCreateHookFailureAborts(t *testing.T) {
	m := newTestManager(t, config.Workspace{Root: t.TempDir()},
		config.Hooks{AfterCreate: "exit 1", TimeoutMS: 5000})

	_, err := m.Create(context.Background(), "id", "ABC-1", nil)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrHookFailed))
	require.True(t, errors.Is(err, ErrWorkspaceCreationFailed))
}

func TestCreate_EmitsAuditEvents(t *testing.T) {
	auditFile := filepath.Join(t.TempDir(), "audit.jsonl")
	sink, err := audit.NewJSONLSink(auditFile)
	require.NoError(t, err)
	w := audit.NewWriter(zerolog.Nop())
	w.AddSink(sink)

	m := newTestManager(t, config.Workspace{Root: t.TempDir()}, config.Hooks{},
		WithAudit(w), WithProjectID("proj-1"))

	ws, err := m.Create(context.Background(), "iss-9", "ABC-1", nil)
	require.NoError(t, err)
	require.NoError(t, m.Remove(context.Background(), ws, nil))
	require.NoError(t, w.Close())

	data, err := os.ReadFile(auditFile)
	require.NoError(t, err)
	s := string(data)
	require.Contains(t, s, "WorkspaceCreated")
	require.Contains(t, s, "WorkspaceRemoved")
	require.Contains(t, s, "proj-1")
}

func TestAgentCommand_ScopedToWorkspace(t *testing.T) {
	m := newTestManager(t, config.Workspace{Root: t.TempDir()}, config.Hooks{})
	ws, err := m.Create(context.Background(), "id", "ABC-1", nil)
	require.NoError(t, err)

	cmd, err := m.AgentCommand(context.Background(), ws, "go", "version")
	require.NoError(t, err)
	require.Equal(t, ws.Path, cmd.Dir)

	_, err = m.AgentCommand(context.Background(), nil, "go")
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrUnsafePath))
}
