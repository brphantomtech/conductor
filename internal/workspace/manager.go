package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/rs/zerolog"

	"github.com/conductor-sh/conductor/internal/audit"
	"github.com/conductor-sh/conductor/internal/config"
)

// cloneFunc materializes one configured repository into dest. It is a seam so
// tests can avoid real git clones; production uses gitClone.
type cloneFunc func(ctx context.Context, repo config.WorkspaceRepo, dest string) error

// Manager owns workspace creation, hook execution, and removal for one
// project. It is constructed once (Phase 6 orchestrator) and is safe for
// concurrent use across issues — it holds no per-workspace mutable state.
type Manager struct {
	cfg       config.Workspace
	hooks     config.Hooks
	log       zerolog.Logger
	clock     func() time.Time
	audit     *audit.Writer
	projectID string
	clone     cloneFunc
}

// Option configures a Manager at construction time.
type Option func(*Manager)

// WithLogger sets the structured logger. The default is a no-op logger.
func WithLogger(l zerolog.Logger) Option { return func(m *Manager) { m.log = l } }

// WithClock overrides the time source. Tests inject a fixed clock.
func WithClock(f func() time.Time) Option {
	return func(m *Manager) {
		if f != nil {
			m.clock = f
		}
	}
}

// WithAudit wires the audit Writer used to emit WorkspaceCreated /
// WorkspaceRemoved / HookExecuted events. Without it, events are dropped.
func WithAudit(w *audit.Writer) Option { return func(m *Manager) { m.audit = w } }

// WithProjectID sets the project id stamped on emitted audit events.
func WithProjectID(id string) Option { return func(m *Manager) { m.projectID = id } }

// withCloner overrides the repository materializer (test seam).
func withCloner(c cloneFunc) Option {
	return func(m *Manager) {
		if c != nil {
			m.clone = c
		}
	}
}

// New constructs a Manager from the workspace and hooks config slices.
func New(cfg config.Workspace, hooks config.Hooks, opts ...Option) *Manager {
	m := &Manager{
		cfg:   cfg,
		hooks: hooks,
		log:   zerolog.Nop(),
		clock: time.Now,
		clone: gitClone,
	}
	for _, o := range opts {
		if o != nil {
			o(m)
		}
	}
	return m
}

// Create materializes a workspace for the given issue: it resolves and
// confines the path (SPEC §14.2), builds the .conductor/ skeleton (§14.1),
// materializes any configured repos (§5.3.4), emits WorkspaceCreated, and runs
// the after_create hook. An after_create failure aborts creation (§5.3.5).
func (m *Manager) Create(ctx context.Context, issueID, issueIdentifier string, hookEnv map[string]string) (*Workspace, error) {
	path, err := m.resolvePath(issueIdentifier)
	if err != nil {
		return nil, wrapCreate("resolve workspace path", err)
	}
	conductor := filepath.Join(path, ".conductor")
	validation := filepath.Join(conductor, "validation")
	if err := os.MkdirAll(validation, 0o755); err != nil {
		return nil, wrapCreate("create .conductor skeleton", err)
	}

	ws := &Workspace{
		Key:             SanitizeKey(issueIdentifier),
		Path:            path,
		Root:            m.absRoot(),
		IssueID:         issueID,
		IssueIdentifier: issueIdentifier,
		ConductorDir:    conductor,
		AuditLogPath:    filepath.Join(conductor, "audit.jsonl"),
		ValidationDir:   validation,
		MetaPath:        filepath.Join(conductor, "meta.json"),
		CreatedAt:       m.clock().UTC(),
	}

	if err := m.writeMeta(ws); err != nil {
		return nil, wrapCreate("write meta.json", err)
	}

	repos, err := m.materializeRepos(ctx, ws)
	if err != nil {
		return nil, wrapCreate("materialize repos", err)
	}
	ws.Repos = repos

	m.emit(ctx, audit.EventWorkspaceCreated, ws, map[string]any{
		"key":   ws.Key,
		"path":  ws.Path,
		"repos": len(ws.Repos),
	}, 0)

	if err := m.RunHook(ctx, ws, HookAfterCreate, hookEnv); err != nil {
		return nil, wrapCreate("after_create hook", err)
	}

	return ws, nil
}

// Remove runs the before_remove hook (failure is logged and ignored per
// SPEC §5.3.5) and deletes the workspace directory after re-checking the
// §14.2 root-confinement invariant.
func (m *Manager) Remove(ctx context.Context, ws *Workspace, hookEnv map[string]string) error {
	if ws == nil || ws.Path == "" {
		return fmt.Errorf("workspace: remove nil workspace: %w", ErrUnsafePath)
	}
	root := m.absRoot()
	if !withinRoot(root, ws.Path) {
		return fmt.Errorf("workspace: refuse to remove %q outside root %q: %w",
			ws.Path, root, ErrUnsafePath)
	}

	if err := m.RunHook(ctx, ws, HookBeforeRemove, hookEnv); err != nil {
		m.log.Warn().Err(err).Str("workspace", ws.Key).
			Msg("workspace: before_remove hook failed; continuing removal")
	}

	if err := os.RemoveAll(ws.Path); err != nil {
		return fmt.Errorf("workspace: remove %q: %w", ws.Path, err)
	}

	m.emit(ctx, audit.EventWorkspaceRemoved, ws, map[string]any{
		"key":  ws.Key,
		"path": ws.Path,
	}, 0)
	return nil
}

// AgentCommand returns an *exec.Cmd scoped to the workspace directory — the
// subprocess isolation seam (SPEC §14.3). It enforces Invariant 1 (the agent
// runs within workspace_path) by setting cmd.Dir and verifying the directory
// exists; the caller is responsible for running it. Container isolation is a
// later opt-in (Phase 17).
func (m *Manager) AgentCommand(ctx context.Context, ws *Workspace, name string, args ...string) (*exec.Cmd, error) {
	if ws == nil || ws.Path == "" {
		return nil, fmt.Errorf("workspace: agent command without workspace: %w", ErrUnsafePath)
	}
	info, err := os.Stat(ws.Path)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("workspace: workspace path %q unavailable: %w", ws.Path, ErrUnsafePath)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = ws.Path
	setProcessGroup(cmd)
	return cmd, nil
}

// resolvePath sanitizes the identifier, joins it under the configured root,
// and verifies the result stays inside the root (SPEC §14.2 Invariants 2+3).
func (m *Manager) resolvePath(identifier string) (string, error) {
	root := m.absRoot()
	if root == "" {
		return "", fmt.Errorf("workspace: root is not configured: %w", ErrUnsafePath)
	}
	key := SanitizeKey(identifier)
	if key == "" {
		return "", fmt.Errorf("workspace: identifier %q sanitizes to empty: %w", identifier, ErrUnsafePath)
	}
	p, err := filepath.Abs(filepath.Join(root, key))
	if err != nil {
		return "", fmt.Errorf("workspace: resolve abs path: %w", err)
	}
	if !withinRoot(root, p) {
		return "", fmt.Errorf("workspace: path %q escapes root %q: %w", p, root, ErrUnsafePath)
	}
	return p, nil
}

// absRoot returns the cleaned absolute workspace root, or "" when unset.
func (m *Manager) absRoot() string {
	if m.cfg.Root == "" {
		return ""
	}
	abs, err := filepath.Abs(m.cfg.Root)
	if err != nil {
		return filepath.Clean(m.cfg.Root)
	}
	return filepath.Clean(abs)
}

// materializeRepos checks out every configured repository into its own
// confined subdirectory and records the layout.
func (m *Manager) materializeRepos(ctx context.Context, ws *Workspace) ([]RepoLayout, error) {
	if len(m.cfg.Repos) == 0 {
		return []RepoLayout{}, nil
	}
	out := make([]RepoLayout, 0, len(m.cfg.Repos))
	for _, r := range m.cfg.Repos {
		name := SanitizeKey(r.Name)
		if name == "" {
			return nil, fmt.Errorf("workspace: repo name %q sanitizes to empty: %w", r.Name, ErrUnsafePath)
		}
		dest := filepath.Join(ws.Path, name)
		if !withinRoot(ws.Path, dest) {
			return nil, fmt.Errorf("workspace: repo %q escapes workspace: %w", r.Name, ErrUnsafePath)
		}
		if err := m.clone(ctx, r, dest); err != nil {
			return nil, fmt.Errorf("workspace: clone repo %q: %w", r.Name, err)
		}
		out = append(out, RepoLayout{Name: name, Path: dest, URL: r.URL})
	}
	return out, nil
}

// writeMeta persists the workspace metadata snapshot (SPEC §14.1).
func (m *Manager) writeMeta(ws *Workspace) error {
	meta := map[string]any{
		"key":              ws.Key,
		"issue_id":         ws.IssueID,
		"issue_identifier": ws.IssueIdentifier,
		"created_at":       ws.CreatedAt.Format(time.RFC3339),
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("workspace: marshal meta: %w", err)
	}
	if err := os.WriteFile(ws.MetaPath, data, 0o644); err != nil {
		return fmt.Errorf("workspace: write meta: %w", err)
	}
	return nil
}

// emit writes a workspace audit event when an audit Writer is configured.
func (m *Manager) emit(ctx context.Context, t audit.EventType, ws *Workspace, payload map[string]any, durMS int64) {
	if m.audit == nil {
		return
	}
	evt := audit.AuditEvent{
		ProjectID:  m.projectID,
		IssueID:    ws.IssueID,
		EventType:  t,
		Payload:    payload,
		DurationMS: durMS,
	}
	if err := m.audit.Write(ctx, evt); err != nil {
		m.log.Warn().Err(err).Str("event_type", string(t)).
			Msg("workspace: audit write failed")
	}
}

// gitClone is the production cloneFunc. With no URL it creates an empty
// checkout directory (the orchestrator may populate it later); with a URL it
// shells out to git. Branch resolution from branch_template is wired by the
// router (Phase 7) once issue context is available.
func gitClone(ctx context.Context, repo config.WorkspaceRepo, dest string) error {
	if repo.URL == "" {
		if err := os.MkdirAll(dest, 0o755); err != nil {
			return fmt.Errorf("workspace: create repo dir %q: %w", dest, err)
		}
		return nil
	}
	args := []string{"clone"}
	if repo.Depth > 0 {
		args = append(args, "--depth", strconv.Itoa(repo.Depth))
	}
	args = append(args, repo.URL, dest)
	cmd := exec.CommandContext(ctx, "git", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("workspace: git clone %q: %w: %s", repo.URL, err, string(out))
	}
	return nil
}
