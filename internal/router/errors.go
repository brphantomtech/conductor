package router

import "errors"

// SPEC §23 — Router/run-attempt error classifications surfaced by the Agent
// Router. The string value of each sentinel matches the SPEC identifier
// verbatim so it appears unchanged in audit events and API responses.
var (
	// ErrPromptRenderFailed signals that a role's prompt could not be
	// rendered (unknown Liquid variable/filter or assembly failure). It maps
	// to the SPEC §16.2 template_render_error / prompt_render_failed family.
	ErrPromptRenderFailed = errors.New("prompt_render_failed")

	// ErrTurnFailed signals that a role's agent turn ended in a non-success
	// terminal state.
	ErrTurnFailed = errors.New("turn_failed")

	// ErrValidationPipelineFailed signals that the Validation Pipeline
	// returned a result at or above fail_on_severity for a role's turn.
	ErrValidationPipelineFailed = errors.New("validation_pipeline_failed")
)
