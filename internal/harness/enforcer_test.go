package harness

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/audit"
	"github.com/conductor-sh/conductor/internal/config"
)

// staticConfig returns a configFn yielding cfg.
func staticConfig(cfg config.Config) func() config.Config {
	return func() config.Config { return cfg }
}

// fakeLayers is a LayerChecker returning a fixed set (or error).
type fakeLayers struct {
	out []LayerViolationInput
	err error
}

func (f fakeLayers) LayerViolations(context.Context) ([]LayerViolationInput, error) {
	return f.out, f.err
}

func newEnforcerFor(t *testing.T, cfg config.Config, factory CommandFactory, opts ...EnforcerOption) (*Enforcer, *recordingSink) {
	t.Helper()
	sink := &recordingSink{}
	w := audit.NewWriter(zerolog.Nop())
	w.AddSink(sink)
	r := NewRunner(factory, WithRunnerAudit(w))
	allOpts := append([]EnforcerOption{WithEnforcerAudit(w)}, opts...)
	return NewEnforcer(r, staticConfig(cfg), allOpts...), sink
}

func TestPreDispatch_DriftCheckDisabledIsNoOp(t *testing.T) {
	cfg := config.Config{
		HarnessRules: []config.HarnessRule{{ID: "x", Severity: "blocking", Check: "x"}},
		Enforcement:  config.Enforcement{DriftCheckOnDispatch: false, BlockingViolationsHaltDispatch: true},
	}
	// A factory that would fail if ever invoked: drift check disabled must not run rules.
	f := fakeFactory{scripts: map[string]string{"x": failScript()}}
	e, sink := newEnforcerFor(t, cfg, f)

	status, err := e.PreDispatch(context.Background())
	require.NoError(t, err)
	require.Equal(t, EnforcerClear, status)
	require.Empty(t, sink.events, "no rules run, no events")
}

func TestPreDispatch_BlockingHaltsDispatch(t *testing.T) {
	cfg := config.Config{
		HarnessRules: []config.HarnessRule{{ID: "blk", Name: "blk", Severity: "blocking", Check: "blk"}},
		Enforcement:  config.Enforcement{DriftCheckOnDispatch: true, BlockingViolationsHaltDispatch: true},
	}
	f := fakeFactory{scripts: map[string]string{"blk": failScript()}}
	e, sink := newEnforcerFor(t, cfg, f)

	status, err := e.PreDispatch(context.Background())
	require.NoError(t, err)
	require.Equal(t, EnforcerBlocked, status)

	var blocked bool
	for _, ev := range sink.events {
		if ev.EventType == audit.EventHarnessEnforcerBlocked {
			blocked = true
		}
	}
	require.True(t, blocked, "HarnessEnforcerBlocked must be emitted")
}

func TestPreDispatch_BlockingWithoutHaltProceeds(t *testing.T) {
	cfg := config.Config{
		HarnessRules: []config.HarnessRule{{ID: "blk", Severity: "blocking", Check: "blk"}},
		Enforcement:  config.Enforcement{DriftCheckOnDispatch: true, BlockingViolationsHaltDispatch: false},
	}
	f := fakeFactory{scripts: map[string]string{"blk": failScript()}}
	e, _ := newEnforcerFor(t, cfg, f)

	status, err := e.PreDispatch(context.Background())
	require.NoError(t, err)
	require.Equal(t, EnforcerViolationsPresent, status)
}

func TestPreDispatch_WarningAndErrorProceedWithSections(t *testing.T) {
	cfg := config.Config{
		HarnessRules: []config.HarnessRule{
			{ID: "w", Name: "debt", Severity: "warning", Check: "w"},
			{ID: "e", Name: "arch", Severity: "error", Check: "e"},
		},
		Enforcement: config.Enforcement{DriftCheckOnDispatch: true, BlockingViolationsHaltDispatch: true},
	}
	f := fakeFactory{scripts: map[string]string{"w": failScript(), "e": failScript()}}
	e, _ := newEnforcerFor(t, cfg, f)

	status, err := e.PreDispatch(context.Background())
	require.NoError(t, err)
	require.Equal(t, EnforcerViolationsPresent, status)
	require.Contains(t, e.TechnicalDebtSection(), sectionTechnicalDebt)
	require.Contains(t, e.ArchitecturalIssuesSection(), sectionArchitecturalIssue)
}

func TestPreDispatch_NoViolationsIsClear(t *testing.T) {
	cfg := config.Config{
		HarnessRules: []config.HarnessRule{{ID: "ok", Severity: "error", Check: "ok"}},
		Enforcement:  config.Enforcement{DriftCheckOnDispatch: true},
	}
	f := fakeFactory{scripts: map[string]string{"ok": passScript()}}
	e, _ := newEnforcerFor(t, cfg, f)

	status, err := e.PreDispatch(context.Background())
	require.NoError(t, err)
	require.Equal(t, EnforcerClear, status)
	require.Empty(t, e.TechnicalDebtSection())
}

func TestPreDispatch_LayerViolationsIncluded(t *testing.T) {
	cfg := config.Config{
		Enforcement: config.Enforcement{DriftCheckOnDispatch: true},
	}
	f := fakeFactory{scripts: map[string]string{}}
	layers := fakeLayers{out: []LayerViolationInput{
		{FromPath: "internal/harness/x.go", FromLayer: "domain", ToPath: "internal/api/y.go", ToLayer: "api"},
	}}
	e, _ := newEnforcerFor(t, cfg, f, WithEnforcerLayers(layers))

	status, err := e.PreDispatch(context.Background())
	require.NoError(t, err)
	require.Equal(t, EnforcerViolationsPresent, status)
	require.Contains(t, e.ArchitecturalIssuesSection(), "domain")
}
