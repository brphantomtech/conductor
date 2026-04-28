package orchestrator

import "errors"

// SPEC §23.4 — Run-attempt errors. These are owned by the orchestrator since
// it composes the lifecycle of a run attempt across workspace creation, hook
// execution, prompt rendering, and turn dispatch. Sentinel string values match
// the SPEC identifiers verbatim.
var (
	// ErrWorkspaceCreationFailed signals that the workspace directory or its
	// nested layout could not be created.
	ErrWorkspaceCreationFailed = errors.New("workspace_creation_failed")

	// ErrHookFailed signals that a lifecycle hook (after_create, before_run,
	// etc.) exited non-zero where the contract treats failure as fatal.
	ErrHookFailed = errors.New("hook_failed")

	// ErrPromptRenderFailed signals that the Liquid prompt template could not
	// be rendered for the current turn.
	ErrPromptRenderFailed = errors.New("prompt_render_failed")

	// ErrTurnTimeout signals that a turn exceeded its configured deadline.
	ErrTurnTimeout = errors.New("turn_timeout")

	// ErrTurnFailed signals that a turn ended in a non-success terminal
	// state classified by the provider adapter.
	ErrTurnFailed = errors.New("turn_failed")

	// ErrTurnCancelled signals that a turn was cancelled by the orchestrator
	// before completion.
	ErrTurnCancelled = errors.New("turn_cancelled")

	// ErrStallTimeout signals that the orchestrator detected no progress
	// within the configured stall window and terminated the worker.
	ErrStallTimeout = errors.New("stall_timeout")

	// ErrValidationPipelineFailed signals that the Validation Pipeline
	// returned a result at or above fail_on_severity.
	ErrValidationPipelineFailed = errors.New("validation_pipeline_failed")

	// ErrCancelledByReconciliation signals that the run attempt was cancelled
	// because the upstream tracker moved the issue out of an active state.
	ErrCancelledByReconciliation = errors.New("cancelled_by_reconciliation")
)
