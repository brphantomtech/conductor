package provider

import (
	"context"
	"errors"
	"fmt"
)

// SPEC §23.3 — Provider errors. Sentinel string values match the SPEC
// identifiers verbatim.
var (
	// ErrUnsupportedProvider signals a providers.<role>.provider value not in
	// the supported list (openrouter, anthropic, openai, ollama, lm_studio,
	// custom).
	ErrUnsupportedProvider = errors.New("unsupported_provider")

	// ErrMissingAPIKey signals that a configured provider's api_key resolves
	// to empty after $VAR substitution.
	ErrMissingAPIKey = errors.New("missing_provider_api_key")

	// ErrRequestFailed signals a transport-level failure when calling a
	// provider's HTTP API.
	ErrRequestFailed = errors.New("provider_request_failed")

	// ErrResponseTimeout signals that a provider response did not complete
	// within the configured deadline.
	ErrResponseTimeout = errors.New("provider_response_timeout")

	// ErrStreamError signals a malformed or interrupted provider streaming
	// response.
	ErrStreamError = errors.New("provider_stream_error")

	// ErrTurnInputRequired signals that the agent requested human input —
	// treated as a hard turn failure, since Conductor runs unattended.
	ErrTurnInputRequired = errors.New("turn_input_required")

	// ErrContextBudgetExceeded signals that the in-flight context exceeded
	// the configured context_budget when compaction_strategy is "none".
	ErrContextBudgetExceeded = errors.New("context_budget_exceeded")
)

// ErrSessionClosed signals that StartTurn/ContinueTurn was called on a
// session that had already been ended. Not a SPEC §23.3 classification —
// it's a programmer-error guard distinct from the on-wire error set.
var ErrSessionClosed = errors.New("provider_session_closed")

// joinErrs is the canonical way adapters wrap a transport-layer cause
// with one of the SPEC §23.3 sentinels so callers can both read the
// underlying message and match on the classification.
func joinErrs(cause, sentinel error) error {
	if cause == nil {
		return sentinel
	}
	return errors.Join(cause, sentinel)
}

// wrapRequest classifies a transport-layer failure as ErrRequestFailed
// while keeping the provider name, operation, and underlying message
// readable in logs.
func wrapRequest(provider, op string, cause error) error {
	return fmt.Errorf("provider: %s: %s: %w", provider, op, joinErrs(cause, ErrRequestFailed))
}

// wrapStream classifies a stream-parsing failure as ErrStreamError.
func wrapStream(provider, op string, cause error) error {
	return fmt.Errorf("provider: %s: %s: %w", provider, op, joinErrs(cause, ErrStreamError))
}

// wrapTimeout classifies a context-deadline failure as ErrResponseTimeout.
func wrapTimeout(provider, op string, cause error) error {
	return fmt.Errorf("provider: %s: %s: %w", provider, op, joinErrs(cause, ErrResponseTimeout))
}

// wrapContextErr maps a context error to the SPEC §23.3 sentinel. Used
// by readers / parsers that detect ctx cancellation between frames.
func wrapContextErr(err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return joinErrs(err, ErrResponseTimeout)
	}
	return err
}
