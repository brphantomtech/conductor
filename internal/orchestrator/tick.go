package orchestrator

import (
	"context"
	"fmt"
	"time"

	"github.com/conductor-sh/conductor/internal/tracker"
)

// Run drives the poll loop until ctx is cancelled (SPEC §13.2). It performs
// startup terminal cleanup first, then ticks on the polling.interval_ms
// cadence, re-reading the interval each tick so a hot-reloaded value takes
// effect without restart. On return it drains in-flight workers.
func (o *Orchestrator) Run(ctx context.Context) error {
	o.runCtx = ctx
	defer o.Wait()

	o.startupCleanup(ctx)

	// Fire an immediate first tick so dispatch does not wait a full interval.
	o.runTick(ctx)

	timer := time.NewTimer(o.interval())
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("orchestrator: poll loop stopped: %w", ctx.Err())
		case <-timer.C:
			o.runTick(ctx)
			timer.Reset(o.interval())
		}
	}
}

// interval returns the current poll interval from the live config, falling
// back to a sane default when the configured value is non-positive.
func (o *Orchestrator) interval() time.Duration {
	ms := o.configFn().Polling.IntervalMS
	if ms <= 0 {
		ms = 30_000
	}
	return time.Duration(ms) * time.Millisecond
}

// runTick executes one poll-loop tick in the SPEC §13.2 order. Steps 1, 3, 4,
// and 6 are extension seams (no-op in Phase 6); steps 2, 5, 7, 8, 9 are live.
// Per §13.2, when preflight (step 3) fails, steps 5–8 are skipped while steps
// 1–2 and 9 still run.
func (o *Orchestrator) runTick(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}

	// Step 1: Harness Enforcer pre-dispatch check (seam → Phase 12).
	if status, err := o.enforcer.PreDispatch(ctx); err != nil {
		o.log.Warn().Err(err).Msg("enforcer pre-dispatch check failed")
	} else {
		o.store.setEnforcerStatus(status)
	}

	// Step 2: reconcile active runs (stall detection + tracker-state refresh).
	o.reconcile(ctx)

	// Step 3: dispatch preflight validation.
	ok, err := o.preflight.Preflight(ctx)
	if err != nil {
		o.log.Warn().Err(err).Msg("dispatch preflight failed")
		ok = false
	}

	if ok {
		// Step 4: sync pending Doc Stores (seam → Phase 11).
		if err := o.docSync.SyncPending(ctx); err != nil {
			o.log.Warn().Err(err).Msg("doc-store sync failed")
		}

		// Step 5: fetch candidate issues from the tracker.
		candidates, err := o.tracker.FetchCandidateIssues(ctx)
		if err != nil {
			o.log.Warn().Err(err).Msg("fetch candidate issues failed")
		} else {
			// Step 6: classify unclassified candidates (seam → Phase 7).
			if classified, cerr := o.classifier.Classify(ctx, candidates); cerr != nil {
				o.log.Warn().Err(cerr).Msg("classification failed")
			} else {
				candidates = classified
			}

			// Steps 7 + 8: sort by dispatch priority and dispatch until
			// concurrency slots are exhausted.
			o.dispatchCandidates(candidates)
		}
	}

	// Step 9: notify observability consumers. Phase 6 exposes runtime state
	// through the RuntimeState accessor only (Phase 14 adds the WS surface).
	o.notify()
}

// dispatchCandidates selects the eligible candidates in dispatch order and
// claims each until the slots chosen by selection are consumed (SPEC §13.3).
// When the enforcer reported a blocking violation this tick (SPEC §11.2,
// enforcer_status == blocked), dispatch is skipped entirely so no new issue is
// claimed; reconciliation (step 2) and observability (step 9) still run.
func (o *Orchestrator) dispatchCandidates(candidates []tracker.Issue) {
	if o.store.snapshot().EnforcerStatus == EnforcerBlocked {
		return
	}
	cfg := selectionConfigFrom(o.configFn())
	selected := selectDispatch(candidates, o.store.snapshot(), o.clock(), cfg)
	for _, iss := range selected {
		o.dispatch(iss)
	}
}

// notify is the step-9 observability hook. Phase 6 has no external consumers;
// it is a placeholder the API layer (Phase 14) will extend.
func (o *Orchestrator) notify() {}
