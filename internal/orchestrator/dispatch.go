package orchestrator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/conductor-sh/conductor/internal/audit"
	"github.com/conductor-sh/conductor/internal/router"
	"github.com/conductor-sh/conductor/internal/tracker"
	"github.com/conductor-sh/conductor/internal/workspace"
)

// dispatch claims an eligible issue and launches a worker goroutine that runs
// the router-selected pipeline (SPEC §12 / §13 / §14.3). When no router is
// wired it runs the single hardcoded coder turn (Phase 6 behavior). It returns
// true when a worker was launched. A failed claim (the issue is already claimed
// or running) returns false without side effects.
func (o *Orchestrator) dispatch(iss tracker.Issue) bool {
	if !o.store.claim(iss.ID) {
		return false
	}

	attempt := &RunAttempt{
		ID:         newAttemptID(),
		IssueID:    iss.ID,
		Identifier: iss.Identifier,
		State:      iss.State,
		Pipeline:   o.selectPipeline(iss),
		Attempt:    o.nextAttemptNumber(iss.ID),
		StartedAt:  o.clock().UTC(),
	}

	o.emit(o.runContext(), audit.EventIssueDispatched, attempt, map[string]any{
		"identifier": iss.Identifier,
		"state":      iss.State,
		"pipeline":   attempt.Pipeline,
		"attempt":    attempt.Attempt,
	})

	o.sem <- struct{}{}
	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		defer func() { <-o.sem }()
		o.runWorker(iss, attempt)
	}()
	return true
}

// runWorker owns one run attempt end-to-end: it registers stall bookkeeping,
// marks the issue running, executes the turn, and finalizes the lifecycle.
func (o *Orchestrator) runWorker(iss tracker.Issue, attempt *RunAttempt) {
	ctx, cancel := context.WithCancel(o.runContext())
	defer cancel()

	now := o.clock()
	o.wmu.Lock()
	o.workers[iss.ID] = &worker{cancel: cancel, startedAt: now, lastProgress: now}
	o.wmu.Unlock()
	defer func() {
		o.wmu.Lock()
		delete(o.workers, iss.ID)
		o.wmu.Unlock()
	}()

	o.store.markRunning(attempt)
	o.emit(ctx, audit.EventRunAttemptStarted, attempt, map[string]any{
		"attempt":  attempt.Attempt,
		"role":     firstRole(attempt.Pipeline),
		"pipeline": attempt.Pipeline,
	})

	outcome, reason := o.runTurn(ctx, iss, attempt)
	o.finishAttempt(attempt, outcome, reason)
}

// selectPipeline returns the issue's pipeline. When the Agent Router is wired
// (Phase 7) it selects from routing.rules / routing.pipeline; otherwise it
// falls back to the Phase 6 single hardcoded coder pipeline.
func (o *Orchestrator) selectPipeline(iss tracker.Issue) []string {
	if o.router != nil {
		if p := o.router.SelectPipeline(iss); len(p) > 0 {
			return p
		}
	}
	return []string{coderRole}
}

// firstRole returns the first role of a pipeline, or the coder role when the
// pipeline is empty.
func firstRole(pipeline []string) string {
	if len(pipeline) > 0 {
		return pipeline[0]
	}
	return coderRole
}

// runTurn creates the workspace and then executes the attempt's pipeline,
// mapping the result to a run outcome and a SPEC §23.4 sentinel string. When
// the Agent Router is wired (Phase 7) it drives the full role pipeline through
// RunPipeline; otherwise it runs the Phase 6 single coder turn. Workspace
// creation, render, and turn failures are each classified distinctly.
func (o *Orchestrator) runTurn(ctx context.Context, iss tracker.Issue, attempt *RunAttempt) (Outcome, string) {
	// Workspace creation (SPEC §14). after_create hook failures surface as
	// ErrHookFailed wrapped inside the create error; classify them first.
	ws, err := o.workspaces.Create(ctx, iss.ID, iss.Identifier, o.hookEnv(iss))
	if err != nil {
		if errors.Is(err, workspace.ErrHookFailed) {
			return OutcomeFailed, ErrHookFailed.Error()
		}
		o.log.Warn().Err(err).Str("issue", iss.Identifier).Msg("workspace creation failed")
		return OutcomeFailed, ErrWorkspaceCreationFailed.Error()
	}

	if o.router != nil {
		return o.runPipeline(ctx, iss, attempt, ws.Path)
	}
	return o.runSingleCoderTurn(ctx, iss, attempt, ws.Path)
}

// runPipeline drives the router-selected pipeline (SPEC §12.4). The router
// renders each role's prompt, runs the turns, and hands output between roles;
// the orchestrator classifies any returned error into its SPEC §23.4 outcome
// (so reconciliation/stall cancellation is honored exactly as in Phase 6).
func (o *Orchestrator) runPipeline(
	ctx context.Context, iss tracker.Issue, attempt *RunAttempt, workspacePath string,
) (Outcome, string) {
	rc := router.RunContext{
		Issue:         iss,
		AttemptID:     attempt.ID,
		Attempt:       attempt.Attempt,
		WorkspacePath: workspacePath,
		Pipeline:      attempt.Pipeline,
		AdvanceIndex:  func(i int) { attempt.PipelineIndex = i },
	}
	if _, err := o.router.RunPipeline(ctx, rc); err != nil {
		if errors.Is(err, router.ErrPromptRenderFailed) {
			o.log.Warn().Err(err).Str("issue", iss.Identifier).Msg("pipeline prompt render failed")
			return OutcomeFailed, ErrPromptRenderFailed.Error()
		}
		if errors.Is(err, router.ErrValidationPipelineFailed) {
			return OutcomeFailed, ErrValidationPipelineFailed.Error()
		}
		return o.classifyTurnError(ctx, iss, err)
	}
	return OutcomeSucceeded, ""
}

// runSingleCoderTurn renders the coder prompt and runs a single provider turn —
// the Phase 6 dispatch path, retained for when no router is wired. A render
// failure is fatal and maps to prompt_render_failed.
func (o *Orchestrator) runSingleCoderTurn(
	ctx context.Context, iss tracker.Issue, attempt *RunAttempt, workspacePath string,
) (Outcome, string) {
	prompt, err := o.renderPrompt(iss, attempt)
	if err != nil {
		o.log.Warn().Err(err).Str("issue", iss.Identifier).Msg("coder prompt render failed")
		return OutcomeFailed, ErrPromptRenderFailed.Error()
	}

	sess, err := o.provider.CreateSession(ctx, o.providerCfg, workspacePath)
	if err != nil {
		return o.classifyTurnError(ctx, iss, err)
	}
	defer func() { _ = o.provider.EndSession(context.WithoutCancel(ctx), sess) }()

	stream, err := o.provider.StartTurn(ctx, sess, prompt, nil)
	if err != nil {
		return o.classifyTurnError(ctx, iss, err)
	}
	res := stream.Wait()
	if res.Err != nil {
		return o.classifyTurnError(ctx, iss, res.Err)
	}
	return OutcomeSucceeded, ""
}

// classifyTurnError maps a turn-time error to a SPEC §23.4 outcome. A
// reconciliation- or stall-initiated cancellation is recorded on the worker
// before cancel() is called, so it takes precedence over the raw ctx error.
func (o *Orchestrator) classifyTurnError(ctx context.Context, iss tracker.Issue, cause error) (Outcome, string) {
	o.wmu.Lock()
	reason := ""
	if w, ok := o.workers[iss.ID]; ok {
		reason = w.cancelReason
	}
	o.wmu.Unlock()

	switch reason {
	case ErrStallTimeout.Error():
		return OutcomeStalled, ErrStallTimeout.Error()
	case ErrCancelledByReconciliation.Error():
		return OutcomeCancelled, ErrCancelledByReconciliation.Error()
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(cause, context.DeadlineExceeded) {
		return OutcomeTimedOut, ErrTurnTimeout.Error()
	}
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(cause, context.Canceled) {
		return OutcomeCancelled, ErrTurnCancelled.Error()
	}
	o.log.Warn().Err(cause).Str("issue", iss.Identifier).Msg("agent turn failed")
	return OutcomeFailed, ErrTurnFailed.Error()
}

// finishAttempt records the terminal lifecycle: it stamps the attempt, emits
// RunAttemptEnded, runs the Part C memory seam, then either releases the
// claim (success / cancellation) or schedules a retry (failure / stall).
func (o *Orchestrator) finishAttempt(attempt *RunAttempt, outcome Outcome, reason string) {
	ctx := o.runContext()
	attempt.EndedAt = o.clock().UTC()
	attempt.Outcome = outcome
	attempt.FailureReason = reason

	o.emit(ctx, audit.EventRunAttemptEnded, attempt, map[string]any{
		"attempt": attempt.Attempt,
		"outcome": string(outcome),
		"reason":  reason,
	})

	if err := o.memoryPost.PostProcess(ctx, attempt); err != nil {
		o.log.Warn().Err(err).Str("issue", attempt.Identifier).
			Msg("memory post-processing failed")
	}

	switch outcome {
	case OutcomeSucceeded:
		o.store.clearRetry(attempt.IssueID)
		o.store.release(attempt.IssueID)
		o.emit(ctx, audit.EventIssueReleased, attempt, map[string]any{"reason": "succeeded"})
	case OutcomeCancelled:
		// The issue left an active state (reconciliation) — do not retry.
		o.store.clearRetry(attempt.IssueID)
		o.store.release(attempt.IssueID)
		o.emit(ctx, audit.EventIssueReleased, attempt, map[string]any{"reason": reason})
	default:
		// Failure or stall: schedule a failure-driven retry (SPEC §13.4/§13.5).
		o.scheduleRetry(attempt, reason, false)
	}
}

// scheduleRetry queues the next attempt for the issue. continuation selects
// the fixed 1000ms continuation delay (clean worker exit) versus the
// failure-driven exponential backoff (SPEC §13.4). It writes RetryScheduled.
func (o *Orchestrator) scheduleRetry(attempt *RunAttempt, reason string, continuation bool) {
	cfg := o.configFn()
	var delay time.Duration
	if continuation {
		delay = continuationDelay
	} else {
		delay = failureBackoff(attempt.Attempt, time.Duration(cfg.Agent.MaxRetryBackoffMS)*time.Millisecond)
	}
	next := o.clock().UTC().Add(delay)
	o.store.recordRetry(attempt.IssueID, retryEntry{
		Attempt:      attempt.Attempt,
		NextEligible: next,
		LastReason:   reason,
	})
	o.emit(o.runContext(), audit.EventRetryScheduled, attempt, map[string]any{
		"attempt":       attempt.Attempt,
		"delay_ms":      delay.Milliseconds(),
		"reason":        reason,
		"continuation":  continuation,
		"next_eligible": next.Format(time.RFC3339Nano),
	})
}

// nextAttemptNumber returns the attempt number for a fresh dispatch: one more
// than the failed attempt recorded in the retry queue, or 1 for a first run.
func (o *Orchestrator) nextAttemptNumber(issueID string) int {
	if e, ok := o.store.retryFor(issueID); ok {
		return e.Attempt + 1
	}
	return 1
}

// renderPrompt renders the coder template against the SPEC §16.2 variable set.
// When no coder template is configured, the empty template renders to an empty
// prompt rather than failing — Phase 6 still exercises the dispatch path.
func (o *Orchestrator) renderPrompt(iss tracker.Issue, attempt *RunAttempt) (string, error) {
	src := o.templateFn()[coderRole]
	return o.render(src, templateVars(iss, attempt))
}

// templateVars builds the SPEC §16.2 root-variable map. Every allowed variable
// is supplied (with Phase-6 placeholders for the memory/knowledge summaries)
// so the strict Liquid renderer never faults on an undefined reference.
func templateVars(iss tracker.Issue, attempt *RunAttempt) map[string]any {
	return map[string]any{
		"issue":             issueVars(iss),
		"attempt":           attempt.Attempt,
		"agent_role":        coderRole,
		"pipeline":          attempt.Pipeline,
		"pipeline_index":    attempt.PipelineIndex,
		"pipeline_length":   len(attempt.Pipeline),
		"memory_summary":    "",
		"knowledge_summary": "",
	}
}

// issueVars projects the tracker issue into the template-visible subset.
func issueVars(iss tracker.Issue) map[string]any {
	desc := ""
	if iss.Description != nil {
		desc = *iss.Description
	}
	return map[string]any{
		"id":          iss.ID,
		"identifier":  iss.Identifier,
		"title":       iss.Title,
		"description": desc,
		"state":       iss.State,
		"labels":      iss.Labels,
	}
}

// hookEnv builds the lifecycle-hook environment for the workspace manager
// (SPEC §5.3.5). Phase 6 supplies the issue identity; per-turn vars arrive in
// later phases.
func (o *Orchestrator) hookEnv(iss tracker.Issue) map[string]string {
	return map[string]string{
		"CONDUCTOR_ISSUE_ID":         iss.ID,
		"CONDUCTOR_ISSUE_IDENTIFIER": iss.Identifier,
	}
}

// emit writes an orchestrator lifecycle audit event when a writer is wired.
func (o *Orchestrator) emit(ctx context.Context, t audit.EventType, attempt *RunAttempt, payload map[string]any) {
	if o.audit == nil {
		return
	}
	evt := audit.AuditEvent{
		ProjectID: o.configFn().Project.ID,
		EventType: t,
		Payload:   payload,
	}
	if attempt != nil {
		evt.IssueID = attempt.IssueID
	}
	if err := o.audit.Write(ctx, evt); err != nil {
		o.log.Warn().Err(err).Str("event_type", string(t)).Msg("audit write failed")
	}
}

// newAttemptID returns a fresh 16-byte hex run-attempt identifier.
func newAttemptID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("orchestrator: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}
