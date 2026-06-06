package orchestrator

import (
	"context"

	"github.com/conductor-sh/conductor/internal/router"
	"github.com/conductor-sh/conductor/internal/tracker"
)

// This file defines the poll-loop extension seams (SPEC §13.2) for engines
// that do not exist yet. Keeping them as interface fields with no-op defaults
// lets Phase 6 preserve the §13.2 step ordering literally; later phases supply
// a real implementation by wiring an Option instead of rewriting control flow.

// EnforcerCheck is poll-loop step 1: the Harness Enforcer pre-dispatch check
// (Phase 12). It returns the resulting enforcer status. The no-op default
// reports EnforcerClear so dispatch is never blocked.
type EnforcerCheck interface {
	PreDispatch(ctx context.Context) (EnforcerStatus, error)
}

// PreflightCheck is poll-loop step 3: dispatch preflight validation. When it
// reports false, SPEC §13.2 requires steps 5–8 (fetch → dispatch) to be
// skipped while steps 1–2 and 9 still run. The no-op default always passes.
type PreflightCheck interface {
	Preflight(ctx context.Context) (ok bool, err error)
}

// DocStoreSync is poll-loop step 4: syncing pending Doc Stores (Phase 11).
// The no-op default does nothing.
type DocStoreSync interface {
	SyncPending(ctx context.Context) error
}

// Classifier is poll-loop step 6: classifying unclassified candidates so the
// router (Phase 7) can pick a pipeline. The no-op default returns the
// candidates unchanged, which is correct for Phase 6's single coder pipeline.
type Classifier interface {
	Classify(ctx context.Context, candidates []tracker.Issue) ([]tracker.Issue, error)
}

// PipelineRouter is the Phase 7 Agent Router seam (SPEC §12.3/§12.4). When
// wired, dispatch asks it for the issue's pipeline (SelectPipeline) and drives
// the role loop through it (RunPipeline). When no router is wired (Phase 6
// default), dispatch falls back to the single hardcoded coder turn so the
// earlier behavior is reproduced exactly.
type PipelineRouter interface {
	SelectPipeline(iss tracker.Issue) []string
	RunPipeline(ctx context.Context, rc router.RunContext) (string, error)
}

// MemoryPostProcessor is reconciliation Part C (SPEC §13.5): writing a
// session-end episodic memory for each terminated run (Phase 9). The no-op
// default does nothing.
type MemoryPostProcessor interface {
	PostProcess(ctx context.Context, attempt *RunAttempt) error
}

// --- No-op default implementations ---------------------------------------

type noopEnforcer struct{}

func (noopEnforcer) PreDispatch(context.Context) (EnforcerStatus, error) {
	return EnforcerClear, nil
}

type noopPreflight struct{}

func (noopPreflight) Preflight(context.Context) (bool, error) { return true, nil }

type noopDocStoreSync struct{}

func (noopDocStoreSync) SyncPending(context.Context) error { return nil }

type noopClassifier struct{}

func (noopClassifier) Classify(_ context.Context, c []tracker.Issue) ([]tracker.Issue, error) {
	return c, nil
}

type noopMemoryPostProcessor struct{}

func (noopMemoryPostProcessor) PostProcess(context.Context, *RunAttempt) error { return nil }
