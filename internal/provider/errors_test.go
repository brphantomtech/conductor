package provider

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSentinelStringsMatchSpec(t *testing.T) {
	pairs := map[string]error{
		"unsupported_provider":      ErrUnsupportedProvider,
		"missing_provider_api_key":  ErrMissingAPIKey,
		"provider_request_failed":   ErrRequestFailed,
		"provider_response_timeout": ErrResponseTimeout,
		"provider_stream_error":     ErrStreamError,
		"turn_input_required":       ErrTurnInputRequired,
		"context_budget_exceeded":   ErrContextBudgetExceeded,
	}
	for want, err := range pairs {
		require.Equal(t, want, err.Error(), "sentinel string for %q must be the SPEC identifier", want)
	}
}

func TestWrapRequest_PreservesIsCheck(t *testing.T) {
	cause := errors.New("dial tcp: connection refused")
	wrapped := wrapRequest("openai", "do request", cause)
	require.ErrorIs(t, wrapped, ErrRequestFailed)
	require.Contains(t, wrapped.Error(), "do request")
	require.Contains(t, wrapped.Error(), "connection refused")
}

func TestWrapStream_PreservesIsCheck(t *testing.T) {
	cause := errors.New("json: unexpected token")
	wrapped := wrapStream("anthropic", "decode chunk", cause)
	require.ErrorIs(t, wrapped, ErrStreamError)
}

func TestWrapTimeout_PreservesIsCheck(t *testing.T) {
	cause := errors.New("context deadline exceeded")
	wrapped := wrapTimeout("openai", "do request", cause)
	require.ErrorIs(t, wrapped, ErrResponseTimeout)
}
