package harness

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/audit"
	"github.com/conductor-sh/conductor/internal/config"
)

// fakeFactory builds commands from a script registry keyed by the rule's check
// string so tests drive pass / fail / hang outcomes without a real toolchain.
type fakeFactory struct {
	// scripts maps a check command to the shell snippet actually executed.
	scripts map[string]string
	// buildErr, when set, makes RuleCommand fail to build.
	buildErr error
}

func (f fakeFactory) RuleCommand(ctx context.Context, command string) (*exec.Cmd, error) {
	if f.buildErr != nil {
		return nil, f.buildErr
	}
	script := f.scripts[command]
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", script)
	} else {
		cmd = exec.CommandContext(ctx, "bash", "-c", script)
	}
	return cmd, nil
}

// passScript / failScript are portable snippets for the two exit outcomes.
func passScript() string {
	if runtime.GOOS == "windows" {
		return "exit 0"
	}
	return "exit 0"
}

func failScript() string {
	if runtime.GOOS == "windows" {
		return "echo boom 1>&2 & exit 1"
	}
	return "echo boom >&2; exit 1"
}

func sleepScript() string {
	if runtime.GOOS == "windows" {
		// ping is the portable sleep on Windows; 5 hops ~= 4s.
		return "ping -n 5 127.0.0.1 >NUL"
	}
	return "sleep 5"
}

func TestRunner_PassProducesNoViolation(t *testing.T) {
	f := fakeFactory{scripts: map[string]string{"go vet": passScript()}}
	r := NewRunner(f)
	rules := []config.HarnessRule{{ID: "vet", Name: "vet", Severity: "error", Check: "go vet"}}

	got := r.Run(context.Background(), rules)
	require.Empty(t, got)
}

func TestRunner_FailProducesSeverityTaggedViolation(t *testing.T) {
	f := fakeFactory{scripts: map[string]string{"lint": failScript()}}
	r := NewRunner(f)
	rules := []config.HarnessRule{{
		ID: "lint-r", Name: "lint rule", Category: "style",
		Severity: "blocking", Check: "lint", FixHint: "run gofmt",
	}}

	got := r.Run(context.Background(), rules)
	require.Len(t, got, 1)
	require.Equal(t, "lint-r", got[0].RuleID)
	require.Equal(t, SeverityBlocking, got[0].Severity)
	require.Equal(t, "run gofmt", got[0].FixHint)
	require.Equal(t, "style", got[0].Category)
	require.NotEmpty(t, got[0].Summary)
}

func TestRunner_UnknownSeverityDefaultsToWarning(t *testing.T) {
	f := fakeFactory{scripts: map[string]string{"x": failScript()}}
	r := NewRunner(f)
	rules := []config.HarnessRule{{ID: "x", Severity: "weird", Check: "x"}}

	got := r.Run(context.Background(), rules)
	require.Len(t, got, 1)
	require.Equal(t, SeverityWarning, got[0].Severity)
}

func TestRunner_TimeoutIsAViolation(t *testing.T) {
	f := fakeFactory{scripts: map[string]string{"hang": sleepScript()}}
	r := NewRunner(f, WithRunnerTimeout(150*time.Millisecond))
	rules := []config.HarnessRule{{ID: "h", Name: "hang", Severity: "warning", Check: "hang"}}

	got := r.Run(context.Background(), rules)
	require.Len(t, got, 1, "a timed-out check must be a violation, not a hang")
	require.Contains(t, got[0].Summary, "timed out")
}

func TestRunner_EmptyCheckSkipped(t *testing.T) {
	r := NewRunner(fakeFactory{scripts: map[string]string{}})
	rules := []config.HarnessRule{{ID: "noop", Severity: "error", Check: "   "}}
	require.Empty(t, r.Run(context.Background(), rules))
}

func TestRunner_BuildErrorIsAViolation(t *testing.T) {
	r := NewRunner(fakeFactory{buildErr: errors.New("nope")})
	rules := []config.HarnessRule{{ID: "b", Severity: "error", Check: "anything"}}
	got := r.Run(context.Background(), rules)
	require.Len(t, got, 1)
}

// recordingSink captures emitted audit events for assertions.
type recordingSink struct {
	events []audit.AuditEvent
}

func (s *recordingSink) Write(_ context.Context, evt audit.AuditEvent) error {
	s.events = append(s.events, evt)
	return nil
}
func (s *recordingSink) Close() error { return nil }

func TestRunner_EmitsViolationEventPerViolation(t *testing.T) {
	sink := &recordingSink{}
	w := audit.NewWriter(zerolog.Nop())
	w.AddSink(sink)

	f := fakeFactory{scripts: map[string]string{"a": failScript(), "b": failScript()}}
	r := NewRunner(f, WithRunnerAudit(w), WithRunnerProjectID("proj"))
	rules := []config.HarnessRule{
		{ID: "a", Severity: "warning", Check: "a"},
		{ID: "b", Severity: "error", Check: "b"},
	}

	got := r.Run(context.Background(), rules)
	require.Len(t, got, 2)
	require.Len(t, sink.events, 2)
	for _, e := range sink.events {
		require.Equal(t, audit.EventHarnessViolationDetected, e.EventType)
		require.Equal(t, "proj", e.ProjectID)
	}
}
