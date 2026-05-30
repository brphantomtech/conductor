package workspace

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/conductor-sh/conductor/internal/audit"
)

// defaultHookTimeout is the SPEC §5.3.5 fallback when hooks.timeout_ms is
// unset or non-positive.
const defaultHookTimeout = 60 * time.Second

// hookOutputLimit caps captured stdout/stderr stored in the audit payload.
const hookOutputLimit = 4096

// RunHook executes the named lifecycle hook with the workspace as cwd, a
// per-hook timeout, and captured output, then emits a HookExecuted audit
// event (SPEC §5.3.5, §17.2). A disabled (empty) hook is a no-op. The caller
// decides whether a failure is fatal: Create aborts on after_create, Remove
// logs-and-continues on before_remove.
func (m *Manager) RunHook(ctx context.Context, ws *Workspace, h Hook, env map[string]string) error {
	script := m.hookScript(h)
	if script == "" {
		return nil
	}
	if ws == nil || ws.Path == "" {
		return fmt.Errorf("workspace: hook %q without workspace: %w", h, ErrHookFailed)
	}
	if !withinRoot(m.absRoot(), ws.Path) {
		return fmt.Errorf("workspace: hook %q workspace %q outside root: %w", h, ws.Path, ErrHookFailed)
	}

	start := m.clock()
	stdout, stderr, runErr := runScript(ctx, script, ws.Path, hookEnviron(env), m.hookTimeout())
	durMS := m.clock().Sub(start).Milliseconds()

	status := "success"
	if runErr != nil {
		status = "failed"
	}
	m.emit(ctx, audit.EventHookExecuted, ws, map[string]any{
		"hook":   string(h),
		"status": status,
		"stdout": truncate(stdout, hookOutputLimit),
		"stderr": truncate(stderr, hookOutputLimit),
	}, durMS)

	if runErr != nil {
		return fmt.Errorf("workspace: hook %q: %w", h, runErr)
	}
	return nil
}

// hookScript returns the configured script for a hook, or "" when disabled.
func (m *Manager) hookScript(h Hook) string {
	switch h {
	case HookAfterCreate:
		return m.hooks.AfterCreate
	case HookBeforeRun:
		return m.hooks.BeforeRun
	case HookAfterRun:
		return m.hooks.AfterRun
	case HookAfterTurn:
		return m.hooks.AfterTurn
	case HookBeforeRemove:
		return m.hooks.BeforeRemove
	case HookOnHarnessViolation:
		return m.hooks.OnHarnessViolation
	default:
		return ""
	}
}

// hookTimeout resolves the per-hook timeout (SPEC §5.3.5: non-positive values
// fall back to the default).
func (m *Manager) hookTimeout() time.Duration {
	if m.hooks.TimeoutMS <= 0 {
		return defaultHookTimeout
	}
	return time.Duration(m.hooks.TimeoutMS) * time.Millisecond
}

// runScript runs a hook script with the OS-appropriate shell, enforcing the
// timeout by killing the process group, and classifies any failure as
// ErrHookFailed.
//
// stdout/stderr are captured to temp files rather than in-memory buffers on
// purpose: with file-backed streams os/exec hands the file descriptor to the
// child directly and spawns no copy goroutine, so cmd.Wait returns as soon as
// the shell exits. With pipe-backed buffers, Wait would block draining the
// pipe until every handle inheritor (e.g. an orphaned grandchild) closed it,
// defeating the timeout on Windows.
func runScript(ctx context.Context, script, dir string, env []string, timeout time.Duration) (string, string, error) {
	cmd := hookCommand(script)
	cmd.Dir = dir
	cmd.Env = env

	outFile, errFile, cleanup, err := newOutputFiles()
	if err != nil {
		return "", "", joinErr(err, ErrHookFailed)
	}
	defer cleanup()
	cmd.Stdout = outFile
	cmd.Stderr = errFile
	setProcessGroup(cmd)

	if err := cmd.Start(); err != nil {
		return "", "", joinErr(fmt.Errorf("start: %w", err), ErrHookFailed)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	var runErr error
	select {
	case <-ctx.Done():
		killProcessGroup(cmd)
		<-done
		runErr = joinErr(fmt.Errorf("cancelled: %w", ctx.Err()), ErrHookFailed)
	case <-timer.C:
		killProcessGroup(cmd)
		<-done
		runErr = joinErr(fmt.Errorf("timeout after %s", timeout), ErrHookFailed)
	case werr := <-done:
		if werr != nil {
			runErr = joinErr(fmt.Errorf("exit: %w", werr), ErrHookFailed)
		}
	}

	return readTemp(outFile), readTemp(errFile), runErr
}

// newOutputFiles creates the temp stdout/stderr capture files plus a cleanup
// closure that closes and removes both.
func newOutputFiles() (out, errf *os.File, cleanup func(), err error) {
	out, err = os.CreateTemp("", "conductor-hook-out-*")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("hook stdout temp: %w", err)
	}
	errf, err = os.CreateTemp("", "conductor-hook-err-*")
	if err != nil {
		_ = out.Close()
		_ = os.Remove(out.Name())
		return nil, nil, nil, fmt.Errorf("hook stderr temp: %w", err)
	}
	cleanup = func() {
		_ = out.Close()
		_ = os.Remove(out.Name())
		_ = errf.Close()
		_ = os.Remove(errf.Name())
	}
	return out, errf, cleanup, nil
}

// readTemp rewinds a capture file and returns its contents, swallowing read
// errors (the captured output is best-effort diagnostic data).
func readTemp(f *os.File) string {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return ""
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return ""
	}
	return string(b)
}

// hookCommand builds the shell invocation per the SPEC §5.3.5 contract:
// `bash -lc <script>` on POSIX, `cmd /C <script>` on Windows.
func hookCommand(script string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/C", script)
	}
	return exec.Command("bash", "-lc", script)
}

// hookEnviron layers the caller-supplied hook variables on top of the parent
// process environment.
func hookEnviron(extra map[string]string) []string {
	base := os.Environ()
	for k, v := range extra {
		base = append(base, k+"="+v)
	}
	return base
}

// truncate bounds captured hook output stored in the audit payload.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…(truncated)"
}
