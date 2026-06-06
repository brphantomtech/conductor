package workspace

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// dialUnix opens the socket, skipping the test if the platform does not support
// Unix domain sockets at the chosen path.
func dialUnix(t *testing.T, path string) net.Conn {
	t.Helper()
	conn, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		t.Skipf("unix socket dial unsupported here: %v", err)
	}
	return conn
}

func sockPath(t *testing.T) string {
	t.Helper()
	// Keep the path short; some platforms cap the sun_path length.
	return filepath.Join(t.TempDir(), "t.sock")
}

func TestToolSocket_EchoRoundTrip(t *testing.T) {
	path := sockPath(t)
	ts, err := ListenToolSocket(path, zerolog.Nop(), nil) // nil handler → echo
	if err != nil {
		t.Skipf("listen unix socket unsupported here: %v", err)
	}
	require.Equal(t, path, ts.Path())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ts.Serve(ctx)

	conn := dialUnix(t, path)
	defer conn.Close()

	payload := []byte("ping")
	require.NoError(t, writeFrame(conn, payload))

	got, err := readFrame(conn)
	require.NoError(t, err)
	require.Equal(t, payload, got)

	require.NoError(t, ts.Close())
}

func TestToolSocket_HandlerInvoked(t *testing.T) {
	path := sockPath(t)
	handler := func(_ context.Context, frame []byte) ([]byte, error) {
		return append([]byte("pong:"), frame...), nil
	}
	ts, err := ListenToolSocket(path, zerolog.Nop(), handler)
	if err != nil {
		t.Skipf("listen unix socket unsupported here: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ts.Serve(ctx)

	conn := dialUnix(t, path)
	defer conn.Close()

	require.NoError(t, writeFrame(conn, []byte("hi")))
	got, err := readFrame(conn)
	require.NoError(t, err)
	require.Equal(t, "pong:hi", string(got))

	require.NoError(t, ts.Close())
}

func TestToolSocket_EmptyPath(t *testing.T) {
	t.Parallel()
	_, err := ListenToolSocket("", zerolog.Nop(), nil)
	require.Error(t, err)
}

func TestFrameRoundTrip(t *testing.T) {
	t.Parallel()
	r, w := net.Pipe()
	defer r.Close()
	defer w.Close()

	go func() {
		_ = writeFrame(w, []byte("framed-payload"))
	}()

	got, err := readFrame(r)
	require.NoError(t, err)
	require.Equal(t, "framed-payload", string(got))
}

func TestReadFrame_EOF(t *testing.T) {
	t.Parallel()
	pr, pw := io.Pipe()
	pw.Close()
	_, err := readFrame(pr)
	require.Error(t, err)
}

func TestWriteFrame_Header(t *testing.T) {
	t.Parallel()
	r, w := net.Pipe()
	defer r.Close()
	defer w.Close()
	go func() { _ = writeFrame(w, []byte("ab")) }()

	var hdr [4]byte
	_, err := io.ReadFull(r, hdr[:])
	require.NoError(t, err)
	require.Equal(t, uint32(2), binary.BigEndian.Uint32(hdr[:]))
}
