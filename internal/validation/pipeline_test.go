package validation

import (
	"context"
	"os/exec"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/audit"
	"github.com/conductor-sh/conductor/internal/config"
)

// memSink captures audit events for assertions.
type memSink struct {
	mu     sync.Mutex
	events []audit.AuditEvent
}

func (s *memSink) Write(_ context.Context, evt audit.AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, evt)
	return nil
}

func (s *memSink) Close() error { return nil }

// scriptFactory builds trivial OS-shell commands keyed by the configured check
// command string, so tests exercise real process execution + classification
// without depending on a linter/test toolchain in CI.
type scriptFactory struct {
	calls []string
}

func (f *scriptFactory) CheckCommand(ctx context.Context, command string) (*exec.Cmd, error) {
	f.calls = append(f.calls, command)
	return shellCmd(ctx, command), nil
}

// shellCmd maps a logical command keyword to a platform-appropriate trivial
// shell invocation: "pass" exits 0, "fail" exits 1 with output, "sleep" blocks
// long enough to trip a short timeout, "echo:<text>" prints and exits 0.
func shellCmd(ctx context.Context, command string) *exec.Cmd {
	win := runtime.GOOS == "windows"
	switch {
	case command == "pass":
		if win {
			return exec.CommandContext(ctx, "cmd", "/C", "exit 0")
		}
		return exec.CommandContext(ctx, "sh", "-c", "exit 0")
	case command == "fail":
		if win {
			return exec.CommandContext(ctx, "cmd", "/C", "echo failure detail 1>&2& exit 1")
		}
		return exec.CommandContext(ctx, "sh", "-c", "echo failure detail >&2; exit 1")
	case command == "sleep":
		if win {
			// ping loopback as a portable sleep substitute on Windows.
			return exec.CommandContext(ctx, "cmd", "/C", "ping -n 10 127.0.0.1 >NUL")
		}
		return exec.CommandContext(ctx, "sh", "-c", "sleep 10")
	default:
		if win {
			return exec.CommandContext(ctx, "cmd", "/C", "echo "+command)
		}
		return exec.CommandContext(ctx, "sh", "-c", "echo "+command)
	}
}

func TestPipelineRunClassification(t *testing.T) {
	fac := &scriptFactory{}
	cfg := config.Validation{
		Enabled: true,
		Checks: []config.ValidationCheck{
			{ID: "ok", Name: "OK", Severity: "error", Command: "pass", TimeoutMS: 5000},
			{ID: "bad", Name: "Bad", Severity: "error", Command: "fail", TimeoutMS: 5000, OutputMaxBytes: 1024},
			{ID: "slow", Name: "Slow", Severity: "warning", Command: "sleep", TimeoutMS: 100},
		},
	}
	p := New(cfg, WithCommandFactory(fac))

	res, err := p.Run(context.Background(), "", 0)
	require.NoError(t, err)
	require.Len(t, res.Checks, 3)

	require.Equal(t, StatusPassed, res.Checks[0].Status)
	require.Equal(t, 0, res.Checks[0].ExitCode)

	require.Equal(t, StatusFailed, res.Checks[1].Status)
	require.NotZero(t, res.Checks[1].ExitCode)
	require.Contains(t, res.Checks[1].Output, "failure detail")

	require.Equal(t, StatusTimeout, res.Checks[2].Status)

	// Checks executed in configured order.
	require.Equal(t, []string{"pass", "fail", "sleep"}, fac.calls)
}

func TestPipelineRunDisabledOrEmpty(t *testing.T) {
	disabled := New(config.Validation{Enabled: false, Checks: []config.ValidationCheck{{ID: "x", Command: "pass"}}},
		WithCommandFactory(&scriptFactory{}))
	res, err := disabled.Run(context.Background(), "", 0)
	require.NoError(t, err)
	require.Empty(t, res.Checks)

	empty := New(config.Validation{Enabled: true}, WithCommandFactory(&scriptFactory{}))
	res, err = empty.Run(context.Background(), "", 0)
	require.NoError(t, err)
	require.Empty(t, res.Checks)
}

func TestPipelineRunPersists(t *testing.T) {
	vdir := t.TempDir()
	cfg := config.Validation{
		Enabled: true,
		Checks:  []config.ValidationCheck{{ID: "ok", Command: "pass", TimeoutMS: 5000}},
	}
	p := New(cfg, WithCommandFactory(&scriptFactory{}))

	_, err := p.Run(context.Background(), vdir, 4)
	require.NoError(t, err)

	got, err := LoadResult(vdir, 4)
	require.NoError(t, err)
	require.Equal(t, 4, got.TurnIndex)
	require.Len(t, got.Checks, 1)
	require.Equal(t, StatusPassed, got.Checks[0].Status)
}

func TestPipelineOutputTruncation(t *testing.T) {
	fac := CommandFactoryFunc(func(ctx context.Context, command string) (*exec.Cmd, error) {
		return shellCmd(ctx, "echo:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), nil
	})
	cfg := config.Validation{
		Enabled: true,
		Checks:  []config.ValidationCheck{{ID: "noisy", Command: "noisy", TimeoutMS: 5000, OutputMaxBytes: 8}},
	}
	p := New(cfg, WithCommandFactory(fac))
	res, err := p.Run(context.Background(), "", 0)
	require.NoError(t, err)
	require.LessOrEqual(t, len(res.Checks[0].Output), 8)
}

func TestPipelineFactoryError(t *testing.T) {
	fac := CommandFactoryFunc(func(ctx context.Context, command string) (*exec.Cmd, error) {
		return nil, context.Canceled
	})
	cfg := config.Validation{Enabled: true, Checks: []config.ValidationCheck{{ID: "x", Command: "pass"}}}
	p := New(cfg, WithCommandFactory(fac))
	res, err := p.Run(context.Background(), "", 0)
	require.NoError(t, err)
	require.Equal(t, StatusFailed, res.Checks[0].Status)
	require.Equal(t, -1, res.Checks[0].ExitCode)
}

func TestPipelineNilFactory(t *testing.T) {
	cfg := config.Validation{Enabled: true, Checks: []config.ValidationCheck{{ID: "x", Command: "pass"}}}
	p := New(cfg, WithCommandFactory(nil)) // nil option is ignored; default factory remains
	require.NotNil(t, p.factory)
}

func TestPipelineEmitsAuditPerCheck(t *testing.T) {
	sink := &memSink{}
	w := audit.NewWriter(zerolog.Nop())
	w.AddSink(sink)

	cfg := config.Validation{
		Enabled: true,
		Checks: []config.ValidationCheck{
			{ID: "ok", Name: "OK", Severity: "error", Command: "pass", TimeoutMS: 5000},
			{ID: "bad", Name: "Bad", Severity: "error", Command: "fail", TimeoutMS: 5000},
		},
	}
	p := New(cfg, WithCommandFactory(&scriptFactory{}), WithAudit(w), WithProjectID("proj-1"))
	_, err := p.Run(context.Background(), "", 0)
	require.NoError(t, err)

	require.Len(t, sink.events, 2)
	for _, e := range sink.events {
		require.Equal(t, audit.EventValidationCheckRun, e.EventType)
		require.Equal(t, "proj-1", e.ProjectID)
		require.Contains(t, e.Payload, "check_id")
		require.Contains(t, e.Payload, "status")
	}
	require.Equal(t, "ok", sink.events[0].Payload["check_id"])
	require.Equal(t, string(StatusFailed), sink.events[1].Payload["status"])
}

func TestCheckTimeout(t *testing.T) {
	require.Equal(t, 200*time.Millisecond, checkTimeout(200, 0))
	require.Equal(t, 300*time.Millisecond, checkTimeout(0, 300))
	require.Equal(t, defaultCheckTimeout, checkTimeout(0, 0))
	require.Equal(t, defaultCheckTimeout, checkTimeout(-5, -5))
}
