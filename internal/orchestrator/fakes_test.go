package orchestrator

import (
	"context"
	"os/exec"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/conductor-sh/conductor/internal/audit"
	"github.com/conductor-sh/conductor/internal/config"
	"github.com/conductor-sh/conductor/internal/provider"
	"github.com/conductor-sh/conductor/internal/tracker"
	"github.com/conductor-sh/conductor/internal/workspace"
)

func nopLogger() zerolog.Logger { return zerolog.Nop() }

// fakeClock is a controllable time source for deterministic timing tests.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock(t time.Time) *fakeClock { return &fakeClock{now: t} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// fakeTracker is an in-memory Tracker with injectable responses and recorded
// calls.
type fakeTracker struct {
	mu sync.Mutex

	candidates    []tracker.Issue
	candidatesErr error

	byStatesErr error
	byStatesAll []tracker.Issue // returned for any FetchIssuesByStates call

	statesByID    map[string]string
	statesByIDErr error

	fetchCandidateCalls int
}

func (f *fakeTracker) FetchCandidateIssues(context.Context) ([]tracker.Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fetchCandidateCalls++
	if f.candidatesErr != nil {
		return nil, f.candidatesErr
	}
	return f.candidates, nil
}

func (f *fakeTracker) FetchIssuesByStates(_ context.Context, _ []string) ([]tracker.Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.byStatesErr != nil {
		return nil, f.byStatesErr
	}
	return f.byStatesAll, nil
}

func (f *fakeTracker) FetchIssueStatesByIDs(_ context.Context, ids []string) (map[string]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.statesByIDErr != nil {
		return nil, f.statesByIDErr
	}
	out := map[string]string{}
	for _, id := range ids {
		if s, ok := f.statesByID[id]; ok {
			out[id] = s
		}
	}
	return out, nil
}

// fakeWorkspaces is a filesystem-free Workspaces implementation.
type fakeWorkspaces struct {
	mu sync.Mutex

	createErr error
	removeErr error

	created []string // issue identifiers created
	removed []string // issue identifiers removed
}

func (f *fakeWorkspaces) Create(_ context.Context, issueID, identifier string, _ map[string]string) (*workspace.Workspace, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return nil, f.createErr
	}
	f.created = append(f.created, identifier)
	return &workspace.Workspace{Key: identifier, Path: "/fake/" + identifier, IssueID: issueID, IssueIdentifier: identifier}, nil
}

func (f *fakeWorkspaces) Resolve(issueID, identifier string) (*workspace.Workspace, error) {
	return &workspace.Workspace{Key: identifier, Path: "/fake/" + identifier, IssueID: issueID, IssueIdentifier: identifier}, nil
}

func (f *fakeWorkspaces) Remove(_ context.Context, ws *workspace.Workspace, _ map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.removeErr != nil {
		return f.removeErr
	}
	f.removed = append(f.removed, ws.IssueIdentifier)
	return nil
}

func (f *fakeWorkspaces) AgentCommand(ctx context.Context, _ *workspace.Workspace, name string, args ...string) (*exec.Cmd, error) {
	return exec.CommandContext(ctx, name, args...), nil
}

func (f *fakeWorkspaces) removedIdentifiers() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.removed...)
}

// fakeProvider runs a turn that blocks until released or ctx is cancelled,
// then returns the preset result. This lets stall/cancel tests interrupt a
// turn in flight.
type fakeProvider struct {
	result      provider.TurnResult
	createErr   error
	startErr    error
	block       chan struct{} // when non-nil, StartTurn's stream waits on it/ctx
	started     chan struct{} // closed-on-first-start signal (optional)
	startedOnce sync.Once
}

func (f *fakeProvider) CreateSession(_ context.Context, _ config.ProviderConfig, _ string) (*provider.Session, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	return &provider.Session{}, nil
}

func (f *fakeProvider) StartTurn(ctx context.Context, _ *provider.Session, _ string, _ []provider.ToolSpec) (provider.TurnStream, error) {
	if f.startErr != nil {
		return nil, f.startErr
	}
	if f.started != nil {
		f.startedOnce.Do(func() { close(f.started) })
	}
	return &fakeStream{ctx: ctx, result: f.result, block: f.block}, nil
}

func (f *fakeProvider) EndSession(context.Context, *provider.Session) error { return nil }

// fakeStream implements provider.TurnStream. Wait blocks on ctx and the
// optional block channel so a test can hold a turn open until it cancels.
type fakeStream struct {
	ctx    context.Context
	result provider.TurnResult
	block  chan struct{}
}

func (s *fakeStream) Events() <-chan provider.AgentEvent {
	ch := make(chan provider.AgentEvent)
	close(ch)
	return ch
}

func (s *fakeStream) Wait() provider.TurnResult {
	if s.block == nil {
		return s.result
	}
	select {
	case <-s.block:
		return s.result
	case <-s.ctx.Done():
		return provider.TurnResult{Err: s.ctx.Err()}
	}
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

// newCaptureWriter builds an audit.Writer wired to a captureSink.
func newCaptureWriter() (*audit.Writer, *captureSink) {
	sink := &captureSink{}
	w := audit.NewWriter(nopLogger())
	w.AddSink(sink)
	return w, sink
}

// --- small builders ------------------------------------------------------

func intp(i int) *int              { return &i }
func strp(s string) *string        { return &s }
func timep(t time.Time) *time.Time { return &t }

// issue builds a minimal dispatch-eligible issue.
func issue(id, identifier, state string) tracker.Issue {
	return tracker.Issue{ID: id, Identifier: identifier, Title: "T-" + identifier, State: state}
}
