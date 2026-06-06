package router

import (
	"context"
	"sync"

	"github.com/rs/zerolog"

	"github.com/conductor-sh/conductor/internal/audit"
	"github.com/conductor-sh/conductor/internal/config"
	"github.com/conductor-sh/conductor/internal/provider"
	"github.com/conductor-sh/conductor/internal/tracker"
)

func nopLogger() zerolog.Logger { return zerolog.Nop() }

// recordedTurn captures one StartTurn/ContinueTurn invocation for assertions.
type recordedTurn struct {
	prompt     string
	continued  bool
	providerID string
}

// fakeProvider is a filesystem-/network-free Provider with scripted turn
// results and recorded calls. Each turn pops the next result from results
// (falling back to defaultResult once exhausted).
type fakeProvider struct {
	mu sync.Mutex

	createErr error
	startErr  error

	results       []provider.TurnResult
	defaultResult provider.TurnResult

	turns []recordedTurn
}

func (f *fakeProvider) nextResult() provider.TurnResult {
	if len(f.results) == 0 {
		return f.defaultResult
	}
	r := f.results[0]
	f.results = f.results[1:]
	return r
}

func (f *fakeProvider) CreateSession(_ context.Context, cfg config.ProviderConfig, _ string) (*provider.Session, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	// Stash the provider id on the session via a sentinel workspace is not
	// possible (Session is opaque); record on the turn instead.
	_ = cfg
	return &provider.Session{}, nil
}

func (f *fakeProvider) StartTurn(_ context.Context, _ *provider.Session, prompt string, _ []provider.ToolSpec) (provider.TurnStream, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.startErr != nil {
		return nil, f.startErr
	}
	f.turns = append(f.turns, recordedTurn{prompt: prompt})
	return &fakeStream{result: f.nextResult()}, nil
}

func (f *fakeProvider) ContinueTurn(_ context.Context, _ *provider.Session, prompt string) (provider.TurnStream, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.startErr != nil {
		return nil, f.startErr
	}
	f.turns = append(f.turns, recordedTurn{prompt: prompt, continued: true})
	return &fakeStream{result: f.nextResult()}, nil
}

func (f *fakeProvider) EndSession(context.Context, *provider.Session) error { return nil }

func (f *fakeProvider) recorded() []recordedTurn {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recordedTurn(nil), f.turns...)
}

// fakeStream is a provider.TurnStream returning a preset result immediately.
type fakeStream struct {
	result provider.TurnResult
}

func (s *fakeStream) Events() <-chan provider.AgentEvent {
	ch := make(chan provider.AgentEvent)
	close(ch)
	return ch
}

func (s *fakeStream) Wait() provider.TurnResult { return s.result }

// fakeTracker returns scripted issue states for continuation re-fetch.
type fakeTracker struct {
	mu sync.Mutex

	states map[string]string
	err    error
	calls  int
}

func (f *fakeTracker) FetchIssueStatesByIDs(_ context.Context, ids []string) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	out := map[string]string{}
	for _, id := range ids {
		if s, ok := f.states[id]; ok {
			out[id] = s
		}
	}
	return out, nil
}

// fakeValidator records calls and returns a scripted error.
type fakeValidator struct {
	mu    sync.Mutex
	err   error
	calls int
	roles []string
}

func (f *fakeValidator) Run(_ context.Context, _, role string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.roles = append(f.roles, role)
	return f.err
}

// captureSink records every audit event for assertions.
type captureSink struct {
	mu     sync.Mutex
	events []audit.AuditEvent
}

func (s *captureSink) Write(_ context.Context, evt audit.AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, evt)
	return nil
}

func (s *captureSink) Close() error { return nil }

func (s *captureSink) count(t audit.EventType) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, e := range s.events {
		if e.EventType == t {
			n++
		}
	}
	return n
}

func (s *captureSink) classifications() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, e := range s.events {
		if v, ok := e.Payload["classification"].(bool); ok && v {
			n++
		}
	}
	return n
}

func newCaptureWriter() (*audit.Writer, *captureSink) {
	sink := &captureSink{}
	w := audit.NewWriter(nopLogger())
	w.AddSink(sink)
	return w, sink
}

// --- builders ------------------------------------------------------------

func strp(s string) *string { return &s }

func issue(id, identifier, title string) tracker.Issue {
	return tracker.Issue{ID: id, Identifier: identifier, Title: title, State: "Todo"}
}
