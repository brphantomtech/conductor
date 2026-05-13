package tracker

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWrapRequest_WrapsSentinelAndPreservesContext(t *testing.T) {
	cause := errors.New("dial tcp: connection refused")
	err := wrapRequest("linear", "fetch candidates", cause)

	require.True(t, errors.Is(err, ErrRequestFailed),
		"errors.Is must reach ErrRequestFailed")
	require.True(t, errors.Is(err, cause),
		"errors.Is must reach the wrapped cause")
	require.Contains(t, err.Error(), "linear")
	require.Contains(t, err.Error(), "fetch candidates")
	require.Contains(t, err.Error(), "connection refused")
}

func TestWrapRequest_NilCause(t *testing.T) {
	err := wrapRequest("github", "list issues", nil)
	require.True(t, errors.Is(err, ErrRequestFailed))
	require.Contains(t, err.Error(), "github")
	require.Contains(t, err.Error(), "list issues")
}

func TestWrapResponse_IncludesStatusAndSnippet(t *testing.T) {
	err := wrapResponse("github", "list issues", 401, `{"message":"Bad credentials"}`)

	require.True(t, errors.Is(err, ErrResponseError))
	require.Contains(t, err.Error(), "github")
	require.Contains(t, err.Error(), "list issues")
	require.Contains(t, err.Error(), "http 401")
	require.Contains(t, err.Error(), "Bad credentials")
}

func TestWrapResponse_EmptySnippet(t *testing.T) {
	err := wrapResponse("linear", "viewer", 500, "")
	require.True(t, errors.Is(err, ErrResponseError))
	require.Contains(t, err.Error(), "http 500")
}

func TestWrapGraphQLErrors_JoinsMessages(t *testing.T) {
	err := wrapGraphQLErrors("linear", "candidate issues", []string{
		"Argument 'projectId' has invalid value",
		"Cannot query field 'foo' on type 'Issue'",
	})

	require.True(t, errors.Is(err, ErrGraphQLErrors))
	msg := err.Error()
	require.Contains(t, msg, "linear")
	require.Contains(t, msg, "candidate issues")
	require.Contains(t, msg, "Argument 'projectId'")
	require.Contains(t, msg, "Cannot query field 'foo'")
	require.Equal(t, 1, strings.Count(msg, "graphql errors"))
}

func TestWrapGraphQLErrors_EmptyMessagesSlice(t *testing.T) {
	err := wrapGraphQLErrors("linear", "candidate issues", nil)
	require.True(t, errors.Is(err, ErrGraphQLErrors))
	require.Contains(t, err.Error(), "<empty>")
}

func TestWrapUnknownPayload_WrapsSentinel(t *testing.T) {
	cause := fmt.Errorf("invalid character 'x' looking for beginning of value")
	err := wrapUnknownPayload("github", "fetch issue", cause)

	require.True(t, errors.Is(err, ErrUnknownPayload))
	require.True(t, errors.Is(err, cause))
	require.Contains(t, err.Error(), "github")
	require.Contains(t, err.Error(), "decode response")
}

// TestSentinelStringsMatchSpec is a local guard so a future PR that
// edits the sentinel strings notices the breakage before
// internal/harness/errors_test.go fails on a less-obvious diff.
func TestSentinelStringsMatchSpec(t *testing.T) {
	cases := map[string]error{
		"unsupported_tracker_kind":   ErrUnsupportedKind,
		"missing_tracker_api_key":    ErrMissingAPIKey,
		"missing_tracker_project_id": ErrMissingProjectID,
		"tracker_request_failed":     ErrRequestFailed,
		"tracker_response_error":     ErrResponseError,
		"tracker_graphql_errors":     ErrGraphQLErrors,
		"tracker_unknown_payload":    ErrUnknownPayload,
	}
	for want, err := range cases {
		require.Equal(t, want, err.Error())
	}
}
