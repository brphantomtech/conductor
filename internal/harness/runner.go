package harness

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/conductor-sh/conductor/internal/audit"
	"github.com/conductor-sh/conductor/internal/config"
)

// defaultRuleTimeout bounds each HarnessRule.check command (SPEC §11.2 step 1:
// rules run "under a timeout"). A check that exceeds it is recorded as a
// violation rather than being allowed to hang the tick.
const defaultRuleTimeout = 60 * time.Second

// ruleOutputMaxBytes bounds captured check output folded into a violation
// summary so a noisy rule cannot bloat a prompt or audit payload.
const ruleOutputMaxBytes = 2048

// CommandFactory builds a HarnessRule.check command pinned to the workspace
// root (SPEC §11.2 step 1: rules run "in the workspace root"). It is the seam
// through which the runner reuses the workspace's subprocess-construction path;
// tests inject a fake so no real shell is spawned. The returned *exec.Cmd must
// have its working directory configured; the runner only captures output and
// runs it.
type CommandFactory interface {
	// RuleCommand returns an *exec.Cmd for a shell command bound to ctx and the
	// workspace root.
	RuleCommand(ctx context.Context, command string) (*exec.Cmd, error)
}

// CommandFactoryFunc adapts a function to the CommandFactory interface.
type CommandFactoryFunc func(ctx context.Context, command string) (*exec.Cmd, error)

// RuleCommand calls f.
func (f CommandFactoryFunc) RuleCommand(ctx context.Context, command string) (*exec.Cmd, error) {
	return f(ctx, command)
}

// Runner executes the configured HarnessRules (SPEC §11.2 step 1). It is
// constructed once and is safe for concurrent use — it holds no per-run mutable
// state. The pre-dispatch check, scheduled GC, and the `harness check` CLI all
// drive rules through it.
type Runner struct {
	factory   CommandFactory
	audit     *audit.Writer
	log       zerolog.Logger
	clock     func() time.Time
	timeout   time.Duration
	projectID string
}

// RunnerOption configures a Runner at construction.
type RunnerOption func(*Runner)

// WithRunnerAudit wires the audit Writer used to emit HarnessViolationDetected
// events. Without it, events are dropped.
func WithRunnerAudit(w *audit.Writer) RunnerOption { return func(r *Runner) { r.audit = w } }

// WithRunnerLogger sets the structured logger. The default is a no-op logger.
func WithRunnerLogger(l zerolog.Logger) RunnerOption { return func(r *Runner) { r.log = l } }

// WithRunnerClock overrides the time source. Tests inject a deterministic clock.
func WithRunnerClock(f func() time.Time) RunnerOption {
	return func(r *Runner) {
		if f != nil {
			r.clock = f
		}
	}
}

// WithRunnerProjectID sets the project id stamped on emitted audit events.
func WithRunnerProjectID(id string) RunnerOption { return func(r *Runner) { r.projectID = id } }

// WithRunnerTimeout overrides the per-rule command timeout.
func WithRunnerTimeout(d time.Duration) RunnerOption {
	return func(r *Runner) {
		if d > 0 {
			r.timeout = d
		}
	}
}

// NewRunner constructs a Runner. The command factory pins each rule's check to
// the workspace root; tests inject a fake. When no factory is supplied a shell
// factory inheriting the parent cwd is used (backing the ad-hoc CLI path).
func NewRunner(factory CommandFactory, opts ...RunnerOption) *Runner {
	r := &Runner{
		factory: factory,
		log:     zerolog.Nop(),
		clock:   time.Now,
		timeout: defaultRuleTimeout,
	}
	if r.factory == nil {
		r.factory = ShellCommandFactory{}
	}
	for _, o := range opts {
		if o != nil {
			o(r)
		}
	}
	return r
}

// Run executes every rule in order, returning one Violation per rule whose
// check exits non-zero (SPEC §11.2 step 1). Exit 0 is a pass and yields no
// violation. A HarnessViolationDetected audit event is emitted per violation.
func (r *Runner) Run(ctx context.Context, rules []config.HarnessRule) []Violation {
	var out []Violation
	for _, rule := range rules {
		// A rule with no check command cannot fail; skip it.
		if strings.TrimSpace(rule.Check) == "" {
			continue
		}
		if v, ok := r.runRule(ctx, rule); ok {
			out = append(out, v)
			r.emit(ctx, v)
		}
	}
	return out
}

// runRule executes a single rule's check under its own timeout and classifies
// the outcome: exit 0 → pass (no violation), non-zero (or timeout / start
// failure) → a violation at the rule's severity.
func (r *Runner) runRule(ctx context.Context, rule config.HarnessRule) (Violation, bool) {
	cctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	output, exitCode, runErr := r.execute(cctx, rule.Check)
	if cctx.Err() != context.DeadlineExceeded && runErr == nil && exitCode == 0 {
		return Violation{}, false
	}

	summary := summarize(rule, cctx.Err() == context.DeadlineExceeded, output)
	return Violation{
		RuleID:   rule.ID,
		Name:     rule.Name,
		Category: rule.Category,
		Severity: classifySeverity(rule.Severity),
		Summary:  summary,
		FixHint:  rule.FixHint,
	}, true
}

// execute builds the command via the factory, captures combined output, and
// returns the exit code. A build/start failure surfaces as runErr with exit -1.
func (r *Runner) execute(ctx context.Context, command string) (output string, exitCode int, err error) {
	cmd, err := r.factory.RuleCommand(ctx, command)
	if err != nil {
		return "", -1, fmt.Errorf("harness: build rule command: %w", err)
	}
	out, runErr := cmd.CombinedOutput()
	if runErr == nil {
		return string(out), 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return string(out), exitErr.ExitCode(), runErr
	}
	return string(out), -1, runErr
}

// summarize builds a short violation summary from the rule name and either the
// timeout marker or a truncated excerpt of the check's output.
func summarize(rule config.HarnessRule, timedOut bool, output string) string {
	if timedOut {
		return fmt.Sprintf("%s check timed out", ruleLabel(rule))
	}
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return fmt.Sprintf("%s check failed", ruleLabel(rule))
	}
	if len(trimmed) > ruleOutputMaxBytes {
		trimmed = trimmed[:ruleOutputMaxBytes]
	}
	return trimmed
}

// ruleLabel returns the rule's name, falling back to its id when name is empty.
func ruleLabel(rule config.HarnessRule) string {
	if rule.Name != "" {
		return rule.Name
	}
	return rule.ID
}

// emit writes a HarnessViolationDetected audit event when a writer is wired
// (SPEC §11.2, §17.2).
func (r *Runner) emit(ctx context.Context, v Violation) {
	if r.audit == nil {
		return
	}
	evt := audit.AuditEvent{
		ProjectID: r.projectID,
		EventType: audit.EventHarnessViolationDetected,
		Payload: map[string]any{
			"rule_id":  v.RuleID,
			"name":     v.Name,
			"category": v.Category,
			"severity": string(v.Severity),
			"summary":  v.Summary,
		},
	}
	if err := r.audit.Write(ctx, evt); err != nil {
		r.log.Warn().Err(err).Str("rule_id", v.RuleID).
			Msg("harness: audit write failed")
	}
}

// ShellCommandFactory is the default CommandFactory: it runs a rule's check via
// the OS shell pinned to Dir (the workspace root). An empty Dir leaves cwd
// inherited. Production wires a workspace-backed factory; this default backs the
// ad-hoc `conductor harness check` path against an explicit workspace directory.
type ShellCommandFactory struct {
	// Dir is the working directory each check runs in (SPEC §11.2). Empty
	// inherits the parent process cwd.
	Dir string
}

// RuleCommand builds an OS-shell invocation: `cmd /C <command>` on Windows,
// `bash -lc <command>` on POSIX (matching the workspace hook + validation
// contract).
func (f ShellCommandFactory) RuleCommand(ctx context.Context, command string) (*exec.Cmd, error) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, "bash", "-lc", command)
	}
	cmd.Dir = f.Dir
	return cmd, nil
}

// NewDirCommandFactory returns a CommandFactory that runs each rule's check via
// the OS shell pinned to dir. It backs the ad-hoc `conductor harness check`
// path where an explicit workspace directory is supplied without a Manager.
func NewDirCommandFactory(dir string) CommandFactory {
	return ShellCommandFactory{Dir: dir}
}
