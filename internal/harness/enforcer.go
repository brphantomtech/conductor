package harness

import (
	"context"
	"sync"

	"github.com/rs/zerolog"

	"github.com/conductor-sh/conductor/internal/audit"
	"github.com/conductor-sh/conductor/internal/config"
)

// EnforcerStatus mirrors the orchestrator's enforcer_status (SPEC §4.1.11). It
// is declared here so the enforcer (Tier 2) does not import the orchestrator
// (Tier 4); the wiring layer maps these values onto orchestrator.EnforcerStatus.
// The string values match the orchestrator's verbatim.
type EnforcerStatus string

// Enforcer status values per SPEC §4.1.11.
const (
	EnforcerClear             EnforcerStatus = "clear"
	EnforcerViolationsPresent EnforcerStatus = "violations_present"
	EnforcerBlocked           EnforcerStatus = "blocked"
)

// LayerChecker reports prohibited cross-layer dependencies (SPEC §8.6). The
// Knowledge Engine satisfies it via an adapter at wiring time; harness declares
// it as a consumer-side interface so it does not import the knowledge package.
// A nil LayerChecker disables layer translation.
type LayerChecker interface {
	LayerViolations(ctx context.Context) ([]LayerViolationInput, error)
}

// Enforcer runs the project's HarnessRules pre-dispatch, on a GC schedule, and
// on demand (SPEC §11). It implements the orchestrator's EnforcerCheck seam via
// PreDispatch and owns the debt/architectural prompt sections produced by the
// most recent pre-dispatch run for later injection (SPEC §16.1 steps 7–8).
type Enforcer struct {
	runner    *Runner
	configFn  func() config.Config
	layers    LayerChecker
	tracker   TrackerIssuer
	audit     *audit.Writer
	log       zerolog.Logger
	projectID string

	// mu guards the cached prompt sections from the most recent pre-dispatch
	// run so a concurrent reader (Phase 13 turn assembly) sees a consistent view.
	mu                  sync.Mutex
	technicalDebt       string
	architecturalIssues string
}

// EnforcerOption configures an Enforcer at construction.
type EnforcerOption func(*Enforcer)

// WithEnforcerLayers wires the layer-violation source (Knowledge Engine).
func WithEnforcerLayers(l LayerChecker) EnforcerOption {
	return func(e *Enforcer) { e.layers = l }
}

// WithEnforcerTracker wires the GC issue creator/dedup source (tracker).
func WithEnforcerTracker(t TrackerIssuer) EnforcerOption {
	return func(e *Enforcer) { e.tracker = t }
}

// WithEnforcerAudit wires the audit Writer used for HarnessEnforcerBlocked.
func WithEnforcerAudit(w *audit.Writer) EnforcerOption {
	return func(e *Enforcer) { e.audit = w }
}

// WithEnforcerLogger sets the structured logger.
func WithEnforcerLogger(l zerolog.Logger) EnforcerOption {
	return func(e *Enforcer) { e.log = l }
}

// WithEnforcerProjectID sets the project id stamped on emitted audit events.
func WithEnforcerProjectID(id string) EnforcerOption {
	return func(e *Enforcer) { e.projectID = id }
}

// NewEnforcer constructs an Enforcer. The runner executes rule checks; configFn
// is read fresh each run so a hot-reloaded harness_rules / enforcement block
// takes effect without restart.
func NewEnforcer(runner *Runner, configFn func() config.Config, opts ...EnforcerOption) *Enforcer {
	e := &Enforcer{
		runner:   runner,
		configFn: configFn,
		log:      zerolog.Nop(),
	}
	if e.configFn == nil {
		e.configFn = func() config.Config { return config.Defaults() }
	}
	for _, o := range opts {
		if o != nil {
			o(e)
		}
	}
	return e
}

// collect runs every configured rule plus any translated layer violations,
// returning the combined violation set. It is the shared detection path for
// PreDispatch, RunGC, and the CLI.
func (e *Enforcer) collect(ctx context.Context, cfg config.Config) []Violation {
	violations := e.runner.Run(ctx, cfg.HarnessRules)
	if e.layers != nil {
		lvs, err := e.layers.LayerViolations(ctx)
		if err != nil {
			e.log.Warn().Err(err).Msg("harness: layer violation check failed")
		} else {
			violations = append(violations, TranslateLayerViolations(lvs)...)
		}
	}
	return violations
}

// PreDispatch is the orchestrator's pre-dispatch seam (SPEC §11.2). When
// enforcement.drift_check_on_dispatch is false it is a no-op reporting
// EnforcerClear without running rules. Otherwise it runs all rules, caches the
// technical-debt and architectural-issues prompt sections, and reports the
// resulting status: EnforcerBlocked when a blocking violation exists and
// blocking_violations_halt_dispatch is set (emitting HarnessEnforcerBlocked),
// EnforcerViolationsPresent when any violation exists, else EnforcerClear.
func (e *Enforcer) PreDispatch(ctx context.Context) (EnforcerStatus, error) {
	cfg := e.configFn()
	enf := cfg.Enforcement
	if !enf.DriftCheckOnDispatch {
		e.setSections("", "")
		return EnforcerClear, nil
	}

	violations := e.collect(ctx, cfg)
	e.setSections(
		FormatTechnicalDebt(violations),
		FormatArchitecturalIssues(violations),
	)

	if len(violations) == 0 {
		return EnforcerClear, nil
	}

	if enf.BlockingViolationsHaltDispatch && hasBlocking(violations) {
		e.emitBlocked(ctx, violations)
		return EnforcerBlocked, nil
	}
	return EnforcerViolationsPresent, nil
}

// TechnicalDebtSection returns the cached "## Known Technical Debt" prompt
// section from the most recent pre-dispatch run, or "" when there is none.
func (e *Enforcer) TechnicalDebtSection() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.technicalDebt
}

// ArchitecturalIssuesSection returns the cached "## Architectural Issues You
// Must Fix" prompt section from the most recent pre-dispatch run, or "".
func (e *Enforcer) ArchitecturalIssuesSection() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.architecturalIssues
}

func (e *Enforcer) setSections(debt, arch string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.technicalDebt = debt
	e.architecturalIssues = arch
}

// emitBlocked writes a HarnessEnforcerBlocked audit event (SPEC §11.2, §17.2).
func (e *Enforcer) emitBlocked(ctx context.Context, violations []Violation) {
	if e.audit == nil {
		return
	}
	evt := audit.AuditEvent{
		ProjectID: e.projectID,
		EventType: audit.EventHarnessEnforcerBlocked,
		Payload: map[string]any{
			"blocking_rules": blockingRuleIDs(violations),
		},
	}
	if err := e.audit.Write(ctx, evt); err != nil {
		e.log.Warn().Err(err).Msg("harness: audit write failed")
	}
}

// hasBlocking reports whether any violation is blocking-severity.
func hasBlocking(violations []Violation) bool {
	for _, v := range violations {
		if v.Severity == SeverityBlocking {
			return true
		}
	}
	return false
}

// blockingRuleIDs returns the rule ids of every blocking violation.
func blockingRuleIDs(violations []Violation) []string {
	var out []string
	for _, v := range violations {
		if v.Severity == SeverityBlocking {
			out = append(out, v.RuleID)
		}
	}
	return out
}
