package orchestrator

import (
	"context"

	"github.com/conductor-sh/conductor/internal/audit"
)

// reconcile is poll-loop step 2 (SPEC §13.5): stall detection (Part A) and
// tracker-state refresh (Part B). Part C (memory post-processing) runs per
// terminated attempt inside finishAttempt.
func (o *Orchestrator) reconcile(ctx context.Context) {
	o.reconcileStalls(ctx)
	o.reconcileTrackerStates(ctx)
}

// reconcileStalls is SPEC §13.5 Part A: terminate any worker that has made no
// progress within the stall window. Cancelling the worker context unblocks the
// in-flight turn; the worker classifies the outcome as stall_timeout and the
// failure path schedules a retry/backoff.
func (o *Orchestrator) reconcileStalls(ctx context.Context) {
	now := o.clock()
	o.wmu.Lock()
	stalled := make([]*worker, 0)
	for id, w := range o.workers {
		if w.cancelReason != "" {
			continue // already being torn down
		}
		if now.Sub(w.lastProgress) >= o.stallTimeout {
			w.cancelReason = ErrStallTimeout.Error()
			stalled = append(stalled, w)
			o.emitTo(ctx, audit.EventSessionStalled, id, map[string]any{
				"stall_timeout_ms": o.stallTimeout.Milliseconds(),
			})
		}
	}
	o.wmu.Unlock()

	for _, w := range stalled {
		w.cancel()
	}
}

// reconcileTrackerStates is SPEC §13.5 Part B: refresh the tracker state of
// running issues and cancel any run whose issue has moved out of an active
// state. The cancellation is classified as cancelled_by_reconciliation; the
// worker releases the claim without scheduling a retry.
func (o *Orchestrator) reconcileTrackerStates(ctx context.Context) {
	ids := o.store.runningIssueIDs()
	if len(ids) == 0 {
		return
	}
	states, err := o.tracker.FetchIssueStatesByIDs(ctx, ids)
	if err != nil {
		o.log.Warn().Err(err).Msg("reconciliation: fetch issue states failed")
		return
	}

	cfg := o.configFn()
	var toCancel []*worker
	o.wmu.Lock()
	for id, w := range o.workers {
		if w.cancelReason != "" {
			continue
		}
		state, ok := states[id]
		if !ok {
			continue // tracker no longer recognizes it; leave for next pass
		}
		stillActive := containsState(cfg.Tracker.ActiveStates, state) &&
			!containsState(cfg.Tracker.TerminalStates, state)
		if stillActive {
			continue
		}
		w.cancelReason = ErrCancelledByReconciliation.Error()
		toCancel = append(toCancel, w)
		o.emitTo(ctx, audit.EventIssueCancelled, id, map[string]any{
			"reason":        ErrCancelledByReconciliation.Error(),
			"tracker_state": state,
		})
	}
	o.wmu.Unlock()

	for _, w := range toCancel {
		w.cancel()
	}
}

// startupCleanup is SPEC §13.6: on boot, remove the workspaces of issues the
// tracker reports in terminal states and clear any stale runtime state. A
// per-workspace failure is logged and does not abort startup.
func (o *Orchestrator) startupCleanup(ctx context.Context) {
	cfg := o.configFn()
	terminal := cfg.Tracker.TerminalStates
	if len(terminal) == 0 {
		return
	}
	issues, err := o.tracker.FetchIssuesByStates(ctx, terminal)
	if err != nil {
		o.log.Warn().Err(err).Msg("startup cleanup: fetch terminal issues failed")
		return
	}
	for _, iss := range issues {
		o.store.release(iss.ID)
		o.store.clearRetry(iss.ID)
		ws, err := o.workspaces.Resolve(iss.ID, iss.Identifier)
		if err != nil {
			o.log.Warn().Err(err).Str("issue", iss.Identifier).
				Msg("startup cleanup: resolve workspace failed; continuing")
			continue
		}
		if err := o.workspaces.Remove(ctx, ws, nil); err != nil {
			o.log.Warn().Err(err).Str("issue", iss.Identifier).
				Msg("startup cleanup: remove workspace failed; continuing")
		}
	}
}

// emitTo writes a lifecycle audit event keyed on an issue id (used where no
// RunAttempt value is in hand, e.g. reconciliation).
func (o *Orchestrator) emitTo(ctx context.Context, t audit.EventType, issueID string, payload map[string]any) {
	if o.audit == nil {
		return
	}
	evt := audit.AuditEvent{
		ProjectID: o.configFn().Project.ID,
		IssueID:   issueID,
		EventType: t,
		Payload:   payload,
	}
	if err := o.audit.Write(ctx, evt); err != nil {
		o.log.Warn().Err(err).Str("event_type", string(t)).Msg("audit write failed")
	}
}
