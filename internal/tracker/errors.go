package tracker

import (
	"errors"
	"fmt"
	"strings"
)

// SPEC §23.2 — Tracker errors. Sentinel string values match the SPEC
// identifiers verbatim. DO NOT change these strings without coordinating
// with internal/harness/errors_test.go (the SPEC §23 registry test).
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

// joinErrs is the canonical way adapters wrap an underlying cause with
// one of the SPEC §23.2 sentinels so callers can both read the message
// and match on the classification.
func joinErrs(cause, sentinel error) error {
	if cause == nil {
		return sentinel
	}
	return errors.Join(cause, sentinel)
}

// wrapRequest classifies a transport-layer failure as ErrRequestFailed
// while keeping the tracker kind, operation, and underlying message
// readable in logs. Used for DNS/TCP/TLS failures, ctx cancellation,
// and any other non-HTTP-status transport fault.
func wrapRequest(kind, op string, cause error) error {
	return fmt.Errorf("tracker: %s: %s: %w", kind, op, joinErrs(cause, ErrRequestFailed))
}

// wrapResponse classifies an HTTP non-2xx response as ErrResponseError
// and includes the status code plus a body snippet in the error message
// so operators can diagnose without re-running the request.
func wrapResponse(kind, op string, status int, snippet string) error {
	if snippet == "" {
		return fmt.Errorf("tracker: %s: %s: http %d: %w", kind, op, status, ErrResponseError)
	}
	return fmt.Errorf("tracker: %s: %s: http %d: %s: %w",
		kind, op, status, snippet, ErrResponseError)
}

// wrapGraphQLErrors classifies a populated GraphQL errors[] array as
// ErrGraphQLErrors. The messages are concatenated with `; ` so callers
// see every reported error in a single message string.
func wrapGraphQLErrors(kind, op string, msgs []string) error {
	joined := strings.Join(msgs, "; ")
	if joined == "" {
		joined = "<empty>"
	}
	return fmt.Errorf("tracker: %s: %s: graphql errors: %s: %w",
		kind, op, joined, ErrGraphQLErrors)
}

// wrapUnknownPayload classifies a JSON-decode failure on an otherwise
// successful response as ErrUnknownPayload.
func wrapUnknownPayload(kind, op string, cause error) error {
	return fmt.Errorf("tracker: %s: %s: decode response: %w",
		kind, op, joinErrs(cause, ErrUnknownPayload))
}
