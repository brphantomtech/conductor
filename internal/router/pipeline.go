package router

import (
	"context"
	"fmt"
	"strings"

	"github.com/conductor-sh/conductor/internal/audit"
	"github.com/conductor-sh/conductor/internal/config"
	"github.com/conductor-sh/conductor/internal/provider"
	"github.com/conductor-sh/conductor/internal/tracker"
)

// RunContext carries the per-attempt data RunPipeline needs from the
// orchestrator without the router importing the orchestrator package (which
// would create an import cycle). The orchestrator owns the RunAttempt; it
// passes the fields the router reads/advances here.
type RunContext struct {
	// Issue is the claimed issue being executed.
	Issue tracker.Issue
	// AttemptID is the run-attempt identifier (audit correlation).
	AttemptID string
	// Attempt is the 1-based attempt number (SPEC §16.2 `attempt`).
	Attempt int
	// WorkspacePath is the per-issue workspace the turn runs against.
	WorkspacePath string
	// Pipeline is the router-selected ordered agent roles.
	Pipeline []string

	// AdvanceIndex is called as each role begins so the orchestrator can keep
	// RunAttempt.pipeline_index in sync (SPEC §4.1.12). It may be nil.
	AdvanceIndex func(index int)
}

// RunPipeline executes the selected pipeline role-by-role (SPEC §12.4) and then
// handles continuation (SPEC §12.5). It returns the final role output and a nil
// error on success. On failure it returns a sentinel-wrapped error the
// orchestrator maps to a run outcome and SPEC §23.4 reason. The router emits
// PipelineRoleStarted/PipelineRoleEnded per role; the orchestrator owns the
// RunAttempt lifecycle events.
func (r *Router) RunPipeline(ctx context.Context, rc RunContext) (string, error) {
	cfg := r.configFn()
	maxTurns := cfg.Agent.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 1
	}

	iss := rc.Issue
	var lastOutput string

	for turn := 1; turn <= maxTurns; turn++ {
		out, err := r.runIteration(ctx, rc, iss, turn)
		if err != nil {
			return "", err
		}
		lastOutput = out

		// SPEC §12.5: after the last role, re-fetch issue state. Continue only
		// while the issue is still active and turn_count < max_turns.
		if turn >= maxTurns {
			break
		}
		active, err := r.issueStillActive(ctx, iss, cfg)
		if err != nil {
			r.log.Warn().Err(err).Str("issue_identifier", iss.Identifier).
				Msg("continuation state re-fetch failed; exiting pipeline")
			break
		}
		if !active {
			break
		}
	}
	return lastOutput, nil
}

// runIteration runs every role of the pipeline once, passing each role's output
// forward as the next role's previous-role section (SPEC §12.4). turn is the
// 1-based iteration number; turns after the first use the continuation template
// for the role rather than re-sending the original role template (SPEC §16.3).
func (r *Router) runIteration(ctx context.Context, rc RunContext, iss tracker.Issue, turn int) (string, error) {
	pipeline := rc.Pipeline
	templates := r.templateFn()
	var previousRole, previousOutput string

	for idx, role := range pipeline {
		if rc.AdvanceIndex != nil {
			rc.AdvanceIndex(idx)
		}
		roleCfg := resolveProviderConfig(r.configFn(), role)

		source := templates[role]
		if turn > 1 {
			source = r.continuationTemplate()
		}

		prompt, err := r.BuildPrompt(PromptInputs{
			RoleTemplate:       source,
			Vars:               templateVars(iss, pipeline, idx, rc.Attempt),
			PreviousRole:       previousRole,
			PreviousRoleOutput: previousOutput,
			ContextBudget:      roleCfg.ContextBudget,
		})
		if err != nil {
			return "", err
		}

		r.emitRole(ctx, audit.EventPipelineRoleStarted, rc, role, idx)

		output, terr := r.runRoleTurn(ctx, roleCfg, rc.WorkspacePath, prompt, turn)
		if terr != nil {
			r.emitRole(ctx, audit.EventPipelineRoleEnded, rc, role, idx, "outcome", "failed")
			return "", terr
		}

		// SPEC §12.4 step 5: run the Validation Pipeline when wired and enabled.
		if verr := r.runValidation(ctx, rc.WorkspacePath, role); verr != nil {
			r.emitRole(ctx, audit.EventPipelineRoleEnded, rc, role, idx, "outcome", "validation_failed")
			return "", fmt.Errorf("%w: %w", ErrValidationPipelineFailed, verr)
		}

		r.emitRole(ctx, audit.EventPipelineRoleEnded, rc, role, idx, "outcome", "succeeded")

		previousRole = role
		previousOutput = output
	}
	return previousOutput, nil
}

// runRoleTurn opens a session for the role's provider and runs a single turn.
// On the first iteration it uses StartTurn; later iterations use ContinueTurn so
// the original prompt is not re-sent (SPEC §16.3). A non-success result maps to
// ErrTurnFailed wrapping the underlying cause so the orchestrator can classify
// stall/cancel/timeout precisely.
func (r *Router) runRoleTurn(
	ctx context.Context, cfg config.ProviderConfig, workspacePath, prompt string, turn int,
) (string, error) {
	if r.provider == nil {
		return "", fmt.Errorf("%w: no provider configured", ErrTurnFailed)
	}
	sess, err := r.provider.CreateSession(ctx, cfg, workspacePath)
	if err != nil {
		return "", fmt.Errorf("router: create session: %w", err)
	}
	defer func() { _ = r.provider.EndSession(context.WithoutCancel(ctx), sess) }()

	var stream provider.TurnStream
	if turn > 1 {
		stream, err = r.provider.ContinueTurn(ctx, sess, prompt)
	} else {
		stream, err = r.provider.StartTurn(ctx, sess, prompt, nil)
	}
	if err != nil {
		return "", fmt.Errorf("router: start turn: %w", err)
	}
	res := stream.Wait()
	if res.Err != nil {
		return "", fmt.Errorf("router: turn: %w", res.Err)
	}
	return res.Text, nil
}

// runValidation invokes the Phase 8 Validation Pipeline at SPEC §12.4 step 5
// when a validator is wired and validation.run_after_turn is enabled. It is a
// no-op (nil) when validation is disabled or no validator has been wired.
func (r *Router) runValidation(ctx context.Context, workspacePath, role string) error {
	if r.validator == nil {
		return nil
	}
	if !r.configFn().Validation.RunAfterTurn {
		return nil
	}
	if err := r.validator.Run(ctx, workspacePath, role); err != nil {
		return fmt.Errorf("router: validation %q: %w", role, err)
	}
	return nil
}

// issueStillActive re-fetches the issue state from the tracker and reports
// whether it remains in an active_state (SPEC §12.5).
func (r *Router) issueStillActive(ctx context.Context, iss tracker.Issue, cfg config.Config) (bool, error) {
	if r.tracker == nil {
		return false, nil
	}
	states, err := r.tracker.FetchIssueStatesByIDs(ctx, []string{iss.ID})
	if err != nil {
		return false, fmt.Errorf("router: re-fetch issue state %s: %w", iss.ID, err)
	}
	current, ok := states[iss.ID]
	if !ok {
		return false, nil
	}
	for _, s := range cfg.Tracker.ActiveStates {
		if strings.EqualFold(strings.TrimSpace(s), strings.TrimSpace(current)) {
			return true, nil
		}
	}
	return false, nil
}

// emitRole writes a PipelineRoleStarted/PipelineRoleEnded audit event for a
// role. Extra key/value pairs are appended to the payload.
func (r *Router) emitRole(ctx context.Context, t audit.EventType, rc RunContext, role string, idx int, kv ...any) {
	if r.audit == nil {
		return
	}
	payload := map[string]any{
		"identifier":     rc.Issue.Identifier,
		"attempt":        rc.Attempt,
		"role":           role,
		"pipeline_index": idx,
	}
	for i := 0; i+1 < len(kv); i += 2 {
		if key, ok := kv[i].(string); ok {
			payload[key] = kv[i+1]
		}
	}
	evt := audit.AuditEvent{
		ProjectID: r.configFn().Project.ID,
		IssueID:   rc.Issue.ID,
		AgentRole: role,
		EventType: t,
		Payload:   payload,
	}
	if err := r.audit.Write(ctx, evt); err != nil {
		r.log.Warn().Err(err).Str("event_type", string(t)).Msg("router audit write failed")
	}
}

// resolveProviderConfig returns the provider config for a role, falling back to
// providers.default when the role has no override (SPEC §5.3.6 / §4.1.4).
func resolveProviderConfig(cfg config.Config, role string) config.ProviderConfig {
	if rc, ok := cfg.Providers.Roles[role]; ok && rc.Provider != "" {
		return rc
	}
	return cfg.Providers.Default
}
