package tracker

import "errors"

// SPEC §23.2 — Tracker errors. Sentinel string values match the SPEC
// identifiers verbatim.
var (
	// ErrUnsupportedKind signals a tracker.kind value not in the supported
	// list (linear, github, jira, plane, shortcut).
	ErrUnsupportedKind = errors.New("unsupported_tracker_kind")

	// ErrMissingAPIKey signals that tracker.api_key resolves to empty after
	// $VAR substitution.
	ErrMissingAPIKey = errors.New("missing_tracker_api_key")

	// ErrMissingProjectID signals that tracker.project_slug or project_id is
	// missing for a kind that requires it.
	ErrMissingProjectID = errors.New("missing_tracker_project_id")

	// ErrRequestFailed signals a transport-level failure when contacting the
	// tracker (network timeout, connection refused, etc.).
	ErrRequestFailed = errors.New("tracker_request_failed")

	// ErrResponseError signals that the tracker returned a non-2xx HTTP
	// status with a structured error body.
	ErrResponseError = errors.New("tracker_response_error")

	// ErrGraphQLErrors signals a GraphQL response containing one or more
	// errors entries.
	ErrGraphQLErrors = errors.New("tracker_graphql_errors")

	// ErrUnknownPayload signals a successful HTTP response whose body could
	// not be decoded into the expected schema.
	ErrUnknownPayload = errors.New("tracker_unknown_payload")
)
