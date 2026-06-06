package workspace

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/config"
)

// recordingRunner captures the spec it was asked to run.
type recordingRunner struct {
	spec   ContainerSpec
	called bool
	code   int
}

func (r *recordingRunner) Run(_ context.Context, spec ContainerSpec) (int, error) {
	r.called = true
	r.spec = spec
	return r.code, nil
}

func TestManager_UsesContainerWhenConfigured(t *testing.T) {
	root := t.TempDir()
	rr := &recordingRunner{code: 0}
	m := newTestManager(t,
		config.Workspace{
			Root:      root,
			Container: &config.Container{Image: "conductor/agent:latest", MemoryLimit: "256m"},
		},
		config.Hooks{},
		WithContainerRunner(rr),
		WithToolSocketPath("/host/tool.sock"),
	)
	require.True(t, m.UseContainer())

	ws, err := m.Create(context.Background(), "id", "ABC-1", nil)
	require.NoError(t, err)

	code, err := m.RunAgent(context.Background(), ws, "agent", "run")
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.True(t, rr.called, "container runner must be used when container configured")
	require.Equal(t, "conductor/agent:latest", rr.spec.Image)
	require.Equal(t, ws.Path, rr.spec.WorkspaceMount.Source)
	require.Equal(t, "none", rr.spec.Network)
	require.Equal(t, "256m", rr.spec.MemoryLimit)
	require.Equal(t, "/host/tool.sock", rr.spec.SocketMount.Source)
}

func TestManager_UsesSubprocessWhenNoContainer(t *testing.T) {
	root := t.TempDir()
	rr := &recordingRunner{}
	m := newTestManager(t,
		config.Workspace{Root: root}, // no container block
		config.Hooks{},
		WithContainerRunner(rr),
	)
	require.False(t, m.UseContainer())

	ws, err := m.Create(context.Background(), "id", "ABC-2", nil)
	require.NoError(t, err)

	// The subprocess path is unchanged: AgentCommand returns an *exec.Cmd
	// scoped to the workspace and the container runner is never touched.
	cmd, err := m.AgentCommand(context.Background(), ws, hostShell(), shellNoop()...)
	require.NoError(t, err)
	require.Equal(t, ws.Path, cmd.Dir)

	code, err := m.RunAgent(context.Background(), ws, hostShell(), shellNoop()...)
	require.NoError(t, err)
	require.Equal(t, 0, code)
	require.False(t, rr.called, "container runner must NOT be used without container config")
}

// hostShell / shellNoop pick a trivially-succeeding command per OS so the
// subprocess path actually runs without depending on a real agent binary.
func hostShell() string {
	if os.PathSeparator == '\\' {
		return "cmd"
	}
	return "true"
}

func shellNoop() []string {
	if os.PathSeparator == '\\' {
		return []string{"/C", "exit", "0"}
	}
	return nil
}
