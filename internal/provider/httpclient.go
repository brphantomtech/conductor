package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// maxErrorBodyBytes caps how much of a non-2xx response body the error
// helpers read. 4 KB is enough to capture the provider's structured
// error and any wrapping HTML page without unbounded memory use.
const maxErrorBodyBytes = 4 * 1024

// readErrorBody is invoked when an HTTP request returned a non-2xx
// status. It reads up to maxErrorBodyBytes of the body, attempts to
// extract a provider-typed error code if the payload is JSON, and
// returns an error classified as ErrRequestFailed.
func readErrorBody(provider, op string, resp *http.Response) error {
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))

	code := extractErrorCode(body)
	msg := truncate(string(body), 256)

	if code != "" {
		return fmt.Errorf(
			"provider: %s: %s: http %d (%s): %s: %w",
			provider, op, resp.StatusCode, code, msg, ErrRequestFailed,
		)
	}
	return fmt.Errorf(
		"provider: %s: %s: http %d: %s: %w",
		provider, op, resp.StatusCode, msg, ErrRequestFailed,
	)
}

// extractErrorCode reads the common `{"error": {"type": "...", "code":
// "..."}}` envelopes both Anthropic and OpenAI use. Returns "" if the
// body is not JSON or carries no recognized field.
func extractErrorCode(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var env struct {
		Error struct {
			Type    string `json:"type"`
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return ""
	}
	switch {
	case env.Error.Code != "":
		return env.Error.Code
	case env.Error.Type != "":
		return env.Error.Type
	default:
		return ""
	}
}

// setJSONHeaders is the shared header preamble every adapter applies to
// its streaming POST: JSON request body, SSE response stream.
func setJSONHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
}
