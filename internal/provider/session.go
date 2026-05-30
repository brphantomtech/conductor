package provider

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
)

// Session is the opaque handle adapters return from CreateSession. It
// carries the cross-adapter identity (id, provider, workspace) plus an
// adapter-private payload that holds the running message history and
// accumulated usage. Higher tiers treat the value as opaque.
type Session struct {
	id        string
	provider  string
	workspace string

	mu      sync.Mutex
	closed  bool
	payload any
}

// newSession constructs a Session with a fresh 16-byte hex identifier.
// Adapters call this from CreateSession.
func newSession(provider, workspace string, payload any) *Session {
	return &Session{
		id:        newSessionID(),
		provider:  provider,
		workspace: workspace,
		payload:   payload,
	}
}

// ID returns the opaque session identifier.
func (s *Session) ID() string { return s.id }

// Provider returns the adapter kind (`anthropic`, `openai`, `openrouter`)
// that owns the session.
func (s *Session) Provider() string { return s.provider }

// Workspace returns the workspace key the session was created for.
func (s *Session) Workspace() string { return s.workspace }

// isClosed reports whether EndSession has been called. Adapters use this
// to reject StartTurn/ContinueTurn calls on a torn-down session.
func (s *Session) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// markClosed flips the session to closed and clears its payload. Idempotent.
func (s *Session) markClosed() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.payload = nil
}

// loadPayload returns the adapter-private payload. Adapters type-assert
// the returned value to their own session struct.
func (s *Session) loadPayload() any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.payload
}

func newSessionID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failing is an OS-level invariant violation —
		// matches the Phase 1 convention in internal/audit.
		panic("provider: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}
