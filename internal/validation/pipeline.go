package validation

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"time"

	"github.com/rs/zerolog"

	"github.com/conductor-sh/conductor/internal/audit"
	"github.com/conductor-sh/conductor/internal/config"
)

// defaultCheckTimeout is the SPEC §15.2 fallback when a check's timeout_ms is
// unset or non-positive.
const defaultCheckTimeout = 60 * time.Second

// defaultOutputMaxBytes bounds captured output when a check sets no
// output_max_bytes (matches the SPEC §15.4 example budget).
const defaultOutputMaxBytes = 4096

// CommandFactory builds a check command pinned to the workspace root (SPEC
// §15.2: "execute command in workspace root"). It is the seam through which the
// pipeline reuses the workspace's subprocess-construction path; tests inject a
// fake so no real toolchain is spawned. The returned *exec.Cmd must have its
// working directory and isolation already configured; the pipeline only sets
// stdout/stderr and runs it.
type CommandFactory interface {
	// CheckCommand returns an *exec.Cmd for a shell command, bound to ctx and
	// the workspace root.
	CheckCommand(ctx context.Context, command string) (*exec.Cmd, error)
}

// CommandFactoryFunc adapts a function to the CommandFactory interface.
type CommandFactoryFunc func(ctx context.Context, command string) (*exec.Cmd, error)

// CheckCommand calls f.
func (f CommandFactoryFunc) CheckCommand(ctx context.Context, command string) (*exec.Cmd, error) {
	return f(ctx, command)
}

// Pipeline runs the configured validation checks for a turn (SPEC §15). It is
// constructed once and is safe for concurrent use across issues — it holds no
// per-turn mutable state. The per-turn invocation point is owned by the Phase 7
// router; this type exposes the Run entry point that router will call.
type Pipeline struct {
	cfg       config.Validation
	factory   CommandFactory
	log       zerolog.Logger
	clock     func() time.Time
	audit     *audit.Writer
	projectID string
}

// Option configures a Pipeline at construction time.
type Option func(*Pipeline)

// WithLogger sets the structured logger. The default is a no-op logger.
func WithLogger(l zerolog.Logger) Option { return func(p *Pipeline) { p.log = l } }

// WithClock overrides the time source. Tests inject a deterministic clock for
// duration measurement.
func WithClock(f func() time.Time) Option {
	return func(p *Pipeline) {
		if f != nil {
			p.clock = f
		}
	}
}

// WithAudit wires the audit Writer used to emit ValidationCheckRun events.
// Without it, events are dropped.
func WithAudit(w *audit.Writer) Option { return func(p *Pipeline) { p.audit = w } }

// WithProjectID sets the project id stamped on emitted audit events.
func WithProjectID(id string) Option { return func(p *Pipeline) { p.projectID = id } }

// WithCommandFactory overrides the command-construction seam. Production wires
// the workspace command path; tests inject a fake.
func WithCommandFactory(f CommandFactory) Option {
	return func(p *Pipeline) {
		if f != nil {
			p.factory = f
		}
	}
}

// New constructs a Pipeline from the validation config. When no command factory
// is injected via WithCommandFactory, it defaults to a shell-backed factory
// pinned to no directory (callers that need workspace-root pinning must supply a
// factory built from the workspace manager — see start.go wiring).
func New(cfg config.Validation, opts ...Option) *Pipeline {
	p := &Pipeline{
		cfg:     cfg,
		log:     zerolog.Nop(),
		clock:   time.Now,
		factory: shellCommandFactory{},
	}
	for _, o := range opts {
		if o != nil {
			o(p)
		}
	}
	return p
}

// Run executes every configured check in order against the workspace at
// workspaceRoot, persists the per-turn results to
// validationDir/<turnIndex>.json, and returns the aggregate result. validationDir
// is the workspace's .conductor/validation directory; an empty validationDir
// skips persistence (ad-hoc / dry runs). When validation is disabled or there
// are no checks, it returns an empty result without touching the filesystem.
func (p *Pipeline) Run(ctx context.Context, validationDir string, turnIndex int) (ValidationPipelineResult, error) {
	res := ValidationPipelineResult{TurnIndex: turnIndex, Checks: []ValidationResult{}}
	if !p.cfg.Enabled || len(p.cfg.Checks) == 0 {
		return res, nil
	}
	if p.factory == nil {
		return res, ErrNoCommandFactory
	}

	for _, check := range p.cfg.Checks {
		r := p.runCheck(ctx, check)
		res.Checks = append(res.Checks, r)
	}

	if validationDir != "" {
		if err := persistResult(validationDir, res); err != nil {
			return res, err
		}
	}
	return res, nil
}

// runCheck executes a single check under its own timeout, classifies the
// outcome, truncates captured output, and emits a ValidationCheckRun event
// (SPEC §15.2 steps 1–4, 6).
func (p *Pipeline) runCheck(ctx context.Context, check config.ValidationCheck) ValidationResult {
	timeout := checkTimeout(check.TimeoutMS, p.cfg.TimeoutMS)
	maxBytes := check.OutputMaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultOutputMaxBytes
	}

	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := p.clock()
	output, exitCode, runErr := p.execute(cctx, check.Command)
	durMS := p.clock().Sub(start).Milliseconds()

	status := classify(cctx, exitCode, runErr)
	result := ValidationResult{
		CheckID:    check.ID,
		Name:       check.Name,
		Severity:   check.Severity,
		Status:     status,
		ExitCode:   exitCode,
		Output:     truncateBytes(output, maxBytes),
		DurationMS: durMS,
	}

	p.emit(ctx, check, result)
	return result
}

// execute builds the command via the factory, captures combined output, and
// returns the exit code. A build/start failure surfaces as runErr with exit -1.
func (p *Pipeline) execute(ctx context.Context, command string) (output string, exitCode int, err error) {
	cmd, err := p.factory.CheckCommand(ctx, command)
	if err != nil {
		return "", -1, fmt.Errorf("validation: build check command: %w", err)
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

// classify maps an execution outcome to a Status (SPEC §15.2 step 3): a
// deadline-exceeded context is a timeout, exit 0 is passed, anything else is
// failed.
func classify(ctx context.Context, exitCode int, runErr error) Status {
	if ctx.Err() == context.DeadlineExceeded {
		return StatusTimeout
	}
	if runErr == nil && exitCode == 0 {
		return StatusPassed
	}
	return StatusFailed
}

// checkTimeout resolves the per-check timeout, falling back to the pipeline
// default then the package default (SPEC §15.2: non-positive values fall back).
func checkTimeout(checkMS, pipelineMS int) time.Duration {
	if checkMS > 0 {
		return time.Duration(checkMS) * time.Millisecond
	}
	if pipelineMS > 0 {
		return time.Duration(pipelineMS) * time.Millisecond
	}
	return defaultCheckTimeout
}

// emit writes a ValidationCheckRun audit event when a writer is wired (SPEC
// §15.2 step 6, §17.2).
func (p *Pipeline) emit(ctx context.Context, check config.ValidationCheck, r ValidationResult) {
	if p.audit == nil {
		return
	}
	evt := audit.AuditEvent{
		ProjectID:  p.projectID,
		EventType:  audit.EventValidationCheckRun,
		DurationMS: r.DurationMS,
		Payload: map[string]any{
			"check_id":  check.ID,
			"name":      check.Name,
			"severity":  check.Severity,
			"status":    string(r.Status),
			"exit_code": r.ExitCode,
		},
	}
	if err := p.audit.Write(ctx, evt); err != nil {
		p.log.Warn().Err(err).Str("check_id", check.ID).
			Msg("validation: audit write failed")
	}
}

// shellCommandFactory is the default CommandFactory: it runs a check command via
// the OS shell pinned to Dir (the workspace root). An empty Dir leaves cwd
// inherited. Production wires a workspace-backed factory (see start.go); this
// default also backs the ad-hoc CLI run against an explicit workspace path.
type shellCommandFactory struct {
	// Dir is the working directory each check runs in (SPEC §15.2). Empty
	// inherits the parent process cwd.
	Dir string
}

// CheckCommand builds an OS-shell invocation: `cmd /C <command>` on Windows,
// `bash -lc <command>` on POSIX (matching the workspace hook contract).
func (f shellCommandFactory) CheckCommand(ctx context.Context, command string) (*exec.Cmd, error) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, "bash", "-lc", command)
	}
	cmd.Dir = f.Dir
	return cmd, nil
}

// NewDirCommandFactory returns a CommandFactory that runs each check via the OS
// shell pinned to dir. It backs the ad-hoc `conductor validation run` path where
// an explicit workspace directory is supplied without a workspace Manager.
func NewDirCommandFactory(dir string) CommandFactory {
	return shellCommandFactory{Dir: dir}
}
