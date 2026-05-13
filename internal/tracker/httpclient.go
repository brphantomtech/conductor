package tracker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// errBodySnippetCap is the maximum number of bytes of a non-2xx response
// body that wrapResponse will include in its error message. Larger
// bodies are truncated; the suffix `…` signals truncation.
const errBodySnippetCap = 4 * 1024

// httpDoer is the subset of *http.Client the package needs. Tests
// inject a fake; production callers pass a real client.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// postJSON sends a JSON-encoded body to url and decodes the response
// body into out (must be a pointer). Non-2xx responses are wrapped as
// ErrResponseError. Decode failures on a 2xx response are wrapped as
// ErrUnknownPayload. Transport faults are wrapped as ErrRequestFailed.
//
// The kind / op pair is used for the error wrapping prefix so operators
// can trace which adapter operation failed without unwrapping.
//
// headers is layered on top of the standard Content-Type +
// Accept JSON pair — callers add auth headers here.
func postJSON(
	ctx context.Context,
	doer httpDoer,
	kind, op, url string,
	headers http.Header,
	body any,
	out any,
) error {
	_, err := doJSON(ctx, doer, http.MethodPost, kind, op, url, headers, body, out)
	return err
}

// getJSON issues a GET request and decodes the body into out. The
// response headers are returned so callers can read pagination hints
// (GitHub's Link header). Same wrapping rules as postJSON.
func getJSON(
	ctx context.Context,
	doer httpDoer,
	kind, op, url string,
	headers http.Header,
	out any,
) (http.Header, error) {
	return doJSON(ctx, doer, http.MethodGet, kind, op, url, headers, nil, out)
}

// doJSON is the shared implementation for postJSON / getJSON. Kept
// unexported so tests target the typed wrappers.
func doJSON(
	ctx context.Context,
	doer httpDoer,
	method, kind, op, url string,
	headers http.Header,
	body any,
	out any,
) (http.Header, error) {
	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, wrapRequest(kind, op, fmt.Errorf("encode body: %w", err))
		}
		reqBody = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, wrapRequest(kind, op, err)
	}
	// Caller-provided headers take precedence over defaults — GitHub
	// needs Accept: application/vnd.github+json, not the generic JSON
	// type. Set the caller's headers first, then add defaults only if
	// the caller hasn't supplied them.
	for k, vs := range headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json")
	}
	if body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := doer.Do(req)
	if err != nil {
		// ctx cancellation surfaces here as a net error wrapping
		// context.Canceled / context.DeadlineExceeded; errors.Is still
		// reaches them through the wrapRequest join, so callers that
		// want to distinguish ctx-cancel from other transport faults
		// can do so.
		return nil, wrapRequest(kind, op, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, snippet := readErrorBody(resp)
		return resp.Header, wrapResponse(kind, op, resp.StatusCode, snippet)
	}

	if out == nil {
		return resp.Header, nil
	}

	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(out); err != nil {
		return resp.Header, wrapUnknownPayload(kind, op, err)
	}

	return resp.Header, nil
}

// readErrorBody drains up to errBodySnippetCap bytes of the response
// body for inclusion in a wrapped error message. The body is closed by
// the caller (the defer in doJSON). The status code is returned alongside
// the snippet so callers can pass both to wrapResponse without re-reading
// resp.StatusCode.
func readErrorBody(resp *http.Response) (status int, snippet string) {
	if resp == nil {
		return 0, ""
	}
	if resp.Body == nil {
		return resp.StatusCode, ""
	}
	buf := make([]byte, errBodySnippetCap+1)
	n, err := io.ReadFull(resp.Body, buf)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		// Read fault — return what we have.
		return resp.StatusCode, string(bytes.TrimSpace(buf[:n]))
	}
	if n > errBodySnippetCap {
		return resp.StatusCode, string(bytes.TrimSpace(buf[:errBodySnippetCap])) + "…"
	}
	return resp.StatusCode, string(bytes.TrimSpace(buf[:n]))
}
