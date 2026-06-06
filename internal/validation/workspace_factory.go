package validation

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"

	"github.com/conductor-sh/conductor/internal/workspace"
)

// workspaceCommander is the subset of the workspace Manager the validation
// pipeline needs: building a subprocess pinned to the workspace root (SPEC
// §14.3, §15.2). Defining it here keeps the dependency at the consumer.
type workspaceCommander interface {
	AgentCommand(ctx context.Context, ws *workspace.Workspace, name string, args ...string) (*exec.Cmd, error)
}

// WorkspaceCommandFactory builds check commands through the workspace manager so
// each check runs in the workspace root with the SPEC §14.2 confinement
// invariants the orchestrator already enforces. It wraps the shell invocation
// (`cmd /C` on Windows, `bash -lc` on POSIX) the same way the workspace hooks do.
type WorkspaceCommandFactory struct {
	mgr workspaceCommander
	ws  *workspace.Workspace
}

// NewWorkspaceCommandFactory returns a CommandFactory that pins check commands
// to ws via mgr.AgentCommand.
func NewWorkspaceCommandFactory(mgr *workspace.Manager, ws *workspace.Workspace) *WorkspaceCommandFactory {
	return &WorkspaceCommandFactory{mgr: mgr, ws: ws}
}

// CheckCommand builds a shell invocation of command, scoped to the workspace.
func (f *WorkspaceCommandFactory) CheckCommand(ctx context.Context, command string) (*exec.Cmd, error) {
	name, args := "bash", []string{"-lc", command}
	if runtime.GOOS == "windows" {
		name, args = "cmd", []string{"/C", command}
	}
	cmd, err := f.mgr.AgentCommand(ctx, f.ws, name, args...)
	if err != nil {
		return nil, fmt.Errorf("validation: workspace command: %w", err)
	}
	return cmd, nil
}
