package provider

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// stringReadCloser is a small io.ReadCloser used to feed fixtures into
// the SSE reader without spinning up an HTTP server.
type stringReadCloser struct{ r *strings.Reader }

func newStringReadCloser(s string) *stringReadCloser {
	return &stringReadCloser{r: strings.NewReader(s)}
}

func (s *stringReadCloser) Read(p []byte) (int, error) { return s.r.Read(p) }
func (s *stringReadCloser) Close() error               { return nil }

func TestSSEReader_NamedAndUnnamedFrames(t *testing.T) {
	body := "event: a\ndata: 1\n\n" + "data: 2\ndata: 2b\n\n" + "data: [DONE]\n\n"
	r := newSSEReader(newStringReadCloser(body))

	f1, err := r.Next(context.Background())
	require.NoError(t, err)
	require.Equal(t, "a", f1.Event)
	require.Equal(t, "1", f1.Data)

	f2, err := r.Next(context.Background())
	require.NoError(t, err)
	require.Equal(t, "", f2.Event)
	require.Equal(t, "2\n2b", f2.Data)

	f3, err := r.Next(context.Background())
	require.NoError(t, err)
	require.Equal(t, "[DONE]", f3.Data)

	_, err = r.Next(context.Background())
	require.ErrorIs(t, err, io.EOF)
}

func TestSSEReader_MalformedFrameSurfacesStreamError(t *testing.T) {
	body := "i-have-no-colon\n\n"
	r := newSSEReader(newStringReadCloser(body))
	_, err := r.Next(context.Background())
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrStreamError), "want ErrStreamError, got %v", err)
}

func TestSSEReader_CancelledContext(t *testing.T) {
	body := "data: 1\n\n"
	r := newSSEReader(newStringReadCloser(body))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := r.Next(ctx)
	require.True(t, errors.Is(err, ErrResponseTimeout), "want ErrResponseTimeout, got %v", err)
}

func TestSSEReader_CommentsAreIgnored(t *testing.T) {
	body := ":keep-alive\ndata: hello\n\n"
	r := newSSEReader(newStringReadCloser(body))
	f, err := r.Next(context.Background())
	require.NoError(t, err)
	require.Equal(t, "hello", f.Data)
}
