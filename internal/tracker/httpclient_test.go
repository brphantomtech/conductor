package tracker

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPostJSON_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		require.Equal(t, "application/json", r.Header.Get("Accept"))
		require.Equal(t, "token-x", r.Header.Get("Authorization"))

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"hello":"world"}`, string(body))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":"ok"}`))
	}))
	defer srv.Close()

	var out map[string]string
	headers := http.Header{}
	headers.Set("Authorization", "token-x")

	err := postJSON(context.Background(), srv.Client(), "linear", "test",
		srv.URL, headers, map[string]string{"hello": "world"}, &out)

	require.NoError(t, err)
	require.Equal(t, "ok", out["data"])
}

func TestGetJSON_ReturnsResponseHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		// GET requests must not carry a Content-Type header.
		require.Empty(t, r.Header.Get("Content-Type"))

		w.Header().Set("Link", `<https://example.com/page=2>; rel="next"`)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":"ok"}`))
	}))
	defer srv.Close()

	var out map[string]string
	respHeaders, err := getJSON(context.Background(), srv.Client(), "github", "test",
		srv.URL, nil, &out)

	require.NoError(t, err)
	require.Equal(t, "ok", out["data"])
	require.Contains(t, respHeaders.Get("Link"), `rel="next"`)
}

func TestPostJSON_401WithJSONBody_WrapsResponseError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
	}))
	defer srv.Close()

	var out map[string]any
	err := postJSON(context.Background(), srv.Client(), "github", "list issues",
		srv.URL, nil, map[string]any{}, &out)

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrResponseError))
	require.Contains(t, err.Error(), "http 401")
	require.Contains(t, err.Error(), "Bad credentials")
	require.Contains(t, err.Error(), "github")
	require.Contains(t, err.Error(), "list issues")
}

func TestPostJSON_5xxWithPlainTextBody_IsCaptured(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("service temporarily unavailable"))
	}))
	defer srv.Close()

	var out map[string]any
	err := postJSON(context.Background(), srv.Client(), "linear", "viewer",
		srv.URL, nil, map[string]any{}, &out)

	require.True(t, errors.Is(err, ErrResponseError))
	require.Contains(t, err.Error(), "http 500")
	require.Contains(t, err.Error(), "service temporarily unavailable")
}

func TestPostJSON_EmptyBodyOnError_StillUsable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	var out map[string]any
	err := postJSON(context.Background(), srv.Client(), "linear", "viewer",
		srv.URL, nil, map[string]any{}, &out)

	require.True(t, errors.Is(err, ErrResponseError))
	require.Contains(t, err.Error(), "http 403")
}

func TestPostJSON_MalformedJSON_WrapsUnknownPayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not-valid-json"))
	}))
	defer srv.Close()

	var out map[string]any
	err := postJSON(context.Background(), srv.Client(), "github", "fetch issue",
		srv.URL, nil, map[string]any{}, &out)

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrUnknownPayload))
	require.Contains(t, err.Error(), "github")
	require.Contains(t, err.Error(), "decode response")
}

func TestPostJSON_ContextCancelled_WrapsRequestFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel before the request goes out

	var out map[string]any
	err := postJSON(ctx, srv.Client(), "linear", "fetch candidates",
		srv.URL, nil, map[string]any{}, &out)

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrRequestFailed))
	// Underlying cause should be reachable for callers that distinguish
	// ctx-cancel from other transport faults.
	require.True(t, errors.Is(err, context.Canceled))
}

func TestReadErrorBody_TruncatesLargeBody(t *testing.T) {
	huge := strings.Repeat("x", errBodySnippetCap+500)
	resp := &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader(huge)),
	}
	status, snippet := readErrorBody(resp)
	require.Equal(t, http.StatusInternalServerError, status)
	// "…" is 3 bytes in UTF-8, so the truncated snippet is at most
	// errBodySnippetCap + 3 bytes long.
	require.LessOrEqual(t, len(snippet), errBodySnippetCap+len("…"))
	require.True(t, strings.HasSuffix(snippet, "…"),
		"truncated snippet must end with ellipsis")
}

func TestReadErrorBody_NilResponse(t *testing.T) {
	status, snippet := readErrorBody(nil)
	require.Equal(t, 0, status)
	require.Empty(t, snippet)
}

func TestReadErrorBody_NilBody(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusBadGateway, Body: nil}
	status, snippet := readErrorBody(resp)
	require.Equal(t, http.StatusBadGateway, status)
	require.Empty(t, snippet)
}
