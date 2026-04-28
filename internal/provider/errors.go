package provider

import "errors"

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
