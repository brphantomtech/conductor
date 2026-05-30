package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/config"
)

func newHookWorkspace(t *testing.T, hooks config.Hooks) (*Manager, *Workspace) {
	t.Helper()
	m := New(config.Workspace{Root: t.TempDir()}, hooks, withCloner(fakeCloner))
	ws, err := m.Create(context.Background(), "id", "ABC-1", nil)
	require.NoError(t, err)
	return m, ws
}

// sleepScript blocks for ~2s on either platform so a short timeout fires.
func sleepScript() string {
	if runtime.GOOS == "windows" {
		return "ping -n 3 127.0.0.1 > NUL"
	}
	return "sleep 2"
}

func TestRunHook_Disabled(t *testing.T) {
	m, ws := newHookWorkspace(t, config.Hooks{})
	require.NoError(t, m.RunHook(context.Background(), ws, HookAfterTurn, nil))
}

func TestRunHook_Success(t *testing.T) {
	m, ws := newHookWorkspace(t, config.Hooks{AfterTurn: "echo hi", TimeoutMS: 5000})
	require.NoError(t, m.RunHook(context.Background(), ws, HookAfterTurn, nil))
}

func TestRunHook_FailureClassified(t *testing.T) {
	m, ws := newHookWorkspace(t, config.Hooks{AfterTurn: "exit 3", TimeoutMS: 5000})
	err := m.RunHook(context.Background(), ws, HookAfterTurn, nil)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrHookFailed))
}

func TestRunHook_Timeout(t *testing.T) {
	m, ws := newHookWorkspace(t, config.Hooks{AfterTurn: sleepScript(), TimeoutMS: 150})

	start := time.Now()
	err := m.RunHook(context.Background(), ws, HookAfterTurn, nil)
	elapsed := time.Since(start)

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrHookFailed))
	require.Less(t, elapsed, 1500*time.Millisecond, "hook should be killed at the timeout, not run to completion")
}

func TestRunHook_EnvInjection(t *testing.T) {
	var script string
	if runtime.GOOS == "windows" {
		script = "echo %CONDUCTOR_TEST% > out.txt"
	} else {
		script = `printf %s "$CONDUCTOR_TEST" > out.txt`
	}
	m, ws := newHookWorkspace(t, config.Hooks{AfterTurn: script, TimeoutMS: 5000})

	err := m.RunHook(context.Background(), ws, HookAfterTurn, map[string]string{"CONDUCTOR_TEST": "hello-env"})
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(ws.Path, "out.txt"))
	require.NoError(t, err)
	require.Contains(t, string(data), "hello-env")
}
