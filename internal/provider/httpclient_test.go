package provider

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadErrorBody_JSONEnvelope(t *testing.T) {
	body := io.NopCloser(strings.NewReader(`{"error":{"type":"authentication_error","message":"Bad key"}}`))
	resp := &http.Response{StatusCode: 401, Body: body}
	err := readErrorBody("openai", "do request", resp)
	require.ErrorIs(t, err, ErrRequestFailed)
	require.Contains(t, err.Error(), "401")
	require.Contains(t, err.Error(), "authentication_error")
}

func TestReadErrorBody_PlainText(t *testing.T) {
	body := io.NopCloser(strings.NewReader("upstream returned 500"))
	resp := &http.Response{StatusCode: 502, Body: body}
	err := readErrorBody("openrouter", "do request", resp)
	require.ErrorIs(t, err, ErrRequestFailed)
	require.Contains(t, err.Error(), "502")
}

func TestReadErrorBody_EmptyBody(t *testing.T) {
	resp := &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader(""))}
	err := readErrorBody("anthropic", "do request", resp)
	require.ErrorIs(t, err, ErrRequestFailed)
	require.Contains(t, err.Error(), "503")
}

func TestExtractErrorCode_FavoursCodeOverType(t *testing.T) {
	body := []byte(`{"error":{"type":"invalid_request_error","code":"invalid_api_key"}}`)
	require.Equal(t, "invalid_api_key", extractErrorCode(body))
}

func TestExtractErrorCode_NonJSONReturnsEmpty(t *testing.T) {
	require.Equal(t, "", extractErrorCode([]byte("not json")))
}
