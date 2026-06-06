package workspace

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/rs/zerolog"
)

// ErrToolSocket classifies a failure creating or serving the host tool socket.
var ErrToolSocket = errors.New("tool_socket_failed")

// ToolHandler processes one decoded tool-call frame and returns the response
// frame. Tool dispatch itself is Phase 13; this phase delivers the transport,
// so the handler is injected and a nil handler echoes the request back (a
// minimal round-trip used by the plumbing tests).
type ToolHandler func(ctx context.Context, frame []byte) ([]byte, error)

// ToolSocket is the host-side Unix socket Conductor mounts into a
// container-isolated workspace so the in-container agent can reach the host
// tool server while the container has no external network (SPEC §21.3). It
// owns the listener and accept loop; the socket file is what gets bind-mounted
// into the container.
//
// Framing is a 4-byte big-endian length prefix followed by that many payload
// bytes, in both directions. This is the minimal framing Phase 13 tool
// dispatch plugs into.
type ToolSocket struct {
	path    string
	ln      net.Listener
	log     zerolog.Logger
	handler ToolHandler
	wg      sync.WaitGroup

	mu    sync.Mutex
	conns map[net.Conn]struct{}

	closeErr error
	once     sync.Once
}

// ListenToolSocket creates a Unix domain socket at path and returns a
// ToolSocket ready to Serve. The path is what the workspace Manager bind-mounts
// into the container at containerSocketPath.
func ListenToolSocket(path string, log zerolog.Logger, handler ToolHandler) (*ToolSocket, error) {
	if path == "" {
		return nil, fmt.Errorf("workspace: tool socket path is empty: %w", ErrToolSocket)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("workspace: listen tool socket %q: %w: %w", path, err, ErrToolSocket)
	}
	return &ToolSocket{
		path:    path,
		ln:      ln,
		log:     log,
		handler: handler,
		conns:   make(map[net.Conn]struct{}),
	}, nil
}

// Path returns the host socket path (the bind-mount source).
func (s *ToolSocket) Path() string { return s.path }

// Serve accepts connections until ctx is cancelled or Close is called. Each
// connection is handled in its own goroutine. Serve returns when the listener
// is closed.
func (s *ToolSocket) Serve(ctx context.Context) {
	go func() {
		<-ctx.Done()
		_ = s.Close()
	}()
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			// Accept fails once the listener is closed; that is the normal
			// shutdown path.
			return
		}
		s.trackConn(conn)
		s.wg.Add(1)
		go s.handleConn(ctx, conn)
	}
}

// trackConn / untrackConn record accepted connections so Close can force them
// shut and unblock any goroutine parked in a read.
func (s *ToolSocket) trackConn(conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conns != nil {
		s.conns[conn] = struct{}{}
	}
}

func (s *ToolSocket) untrackConn(conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.conns, conn)
}

// handleConn services framed request/response pairs on one connection until the
// peer closes it or a read error occurs.
func (s *ToolSocket) handleConn(ctx context.Context, conn net.Conn) {
	defer s.wg.Done()
	defer s.untrackConn(conn)
	defer func() { _ = conn.Close() }()

	r := bufio.NewReader(conn)
	for {
		frame, err := readFrame(r)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				s.log.Debug().Err(err).Msg("workspace: tool socket read")
			}
			return
		}
		resp, err := s.dispatch(ctx, frame)
		if err != nil {
			s.log.Debug().Err(err).Msg("workspace: tool socket handler")
			return
		}
		if err := writeFrame(conn, resp); err != nil {
			s.log.Debug().Err(err).Msg("workspace: tool socket write")
			return
		}
	}
}

// dispatch invokes the injected handler, or echoes the frame when no handler is
// configured (the Phase 13 dispatch is not yet wired).
func (s *ToolSocket) dispatch(ctx context.Context, frame []byte) ([]byte, error) {
	if s.handler == nil {
		return frame, nil
	}
	return s.handler(ctx, frame)
}

// Close stops accepting connections, removes the socket file, and waits for
// in-flight connections to drain.
func (s *ToolSocket) Close() error {
	s.once.Do(func() {
		s.closeErr = s.ln.Close()
		// Force-close in-flight connections so handler goroutines parked in a
		// blocking read return, then wait for them to drain.
		s.mu.Lock()
		for c := range s.conns {
			_ = c.Close()
		}
		s.mu.Unlock()
		s.wg.Wait()
	})
	return s.closeErr
}

// readFrame reads one length-prefixed frame.
func readFrame(r io.Reader) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, fmt.Errorf("workspace: read frame body: %w", err)
	}
	return buf, nil
}

// writeFrame writes one length-prefixed frame.
func writeFrame(w io.Writer, payload []byte) error {
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(payload)))
	if _, err := w.Write(hdr[:]); err != nil {
		return fmt.Errorf("workspace: write frame header: %w", err)
	}
	if _, err := w.Write(payload); err != nil {
		return fmt.Errorf("workspace: write frame body: %w", err)
	}
	return nil
}
