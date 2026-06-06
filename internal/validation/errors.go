package validation

import "errors"

// Package-level sentinels. ErrValidationPipelineFailed mirrors the SPEC §23.4 /
// orchestrator turn-failure classification; the orchestrator owns the canonical
// sentinel, so this package exposes the reason string via FailureReason and
// returns this sentinel only for internal classification.
var (
	// ErrPersist signals that per-turn result persistence could not complete.
	ErrPersist = errors.New("validation_persist_error")

	// ErrNoCommandFactory signals the pipeline was constructed without a way to
	// build check commands (no workspace and no injected factory).
	ErrNoCommandFactory = errors.New("validation_no_command_factory")
)
