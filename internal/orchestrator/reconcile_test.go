package orchestrator

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/audit"
	"github.com/conductor-sh/conductor/internal/config"
)

// awaitRunning blocks until the orchestrator reports n running attempts or the
// deadline elapses.
func awaitRunning(t *testing.T, o *Orchestrator, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if o.RuntimeState().RunningCount() == n {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d running attempts (have %d)", n, o.RuntimeState().RunningCount())
}

func TestReconcile_StallTerminatesAndRetries(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC))
	tr := &fakeTracker{}
	ws := &fakeWorkspaces{}
	started := make(chan struct{})
	pr := &fakeProvider{block: make(chan struct{}), started: started}
	w, sink := newCaptureWriter()
	o := New(
		WithTracker(tr), WithWorkspaces(ws),
		WithProvider(pr, config.ProviderConfig{Provider: "anthropic"}),
		WithAudit(w),
		WithConfig(func() config.Config { return config.Defaults() }),
		WithClock(clk.Now), WithLogger(nopLogger()),
		WithStallTimeout(30*time.Second),
	)

	o.dispatch(issue("i1", "A-1", "Todo"))
	<-started // turn is in flight
	awaitRunning(t, o, 1)

	// Advance past the stall window and reconcile — the worker is cancelled.
	clk.Advance(31 * time.Second)
	o.reconcile(context.Background())
	o.Wait()

	require.Equal(t, 1, sink.count(audit.EventSessionStalled))
	snap := o.RuntimeState()
	require.Equal(t, 0, snap.RunningCount())
	require.Contains(t, snap.RetryQueued, "i1", "stall schedules a retry")
	require.Equal(t, ErrStallTimeout.Error(), snap.RetryQueued["i1"].LastReason)
}

func TestReconcile_IssueLeftActiveStateCancelled(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC))
	// Tracker reports the running issue moved to Done (terminal).
	tr := &fakeTracker{statesByID: map[string]string{"i1": "Done"}}
	ws := &fakeWorkspaces{}
	started := make(chan struct{})
	pr := &fakeProvider{block: make(chan struct{}), started: started}
	w, sink := newCaptureWriter()
	o := New(
		WithTracker(tr), WithWorkspaces(ws),
		WithProvider(pr, config.ProviderConfig{Provider: "anthropic"}),
		WithAudit(w),
		WithConfig(func() config.Config { return config.Defaults() }),
		WithClock(clk.Now), WithLogger(nopLogger()),
	)

	o.dispatch(issue("i1", "A-1", "Todo"))
	<-started
	awaitRunning(t, o, 1)

	o.reconcile(context.Background())
	o.Wait()

	require.Equal(t, 1, sink.count(audit.EventIssueCancelled))
	require.Equal(t, 1, sink.count(audit.EventIssueReleased))
	snap := o.RuntimeState()
	require.Equal(t, 0, snap.RunningCount())
	require.NotContains(t, snap.Claimed, "i1")
	require.Empty(t, snap.RetryQueued, "reconciliation cancel does not retry")
}

func TestReconcile_StillActiveLeftRunning(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC))
	tr := &fakeTracker{statesByID: map[string]string{"i1": "In Progress"}}
	ws := &fakeWorkspaces{}
	started := make(chan struct{})
	block := make(chan struct{})
	pr := &fakeProvider{block: block, started: started}
	w, _ := newCaptureWriter()
	o := New(
		WithTracker(tr), WithWorkspaces(ws),
		WithProvider(pr, config.ProviderConfig{Provider: "anthropic"}),
		WithAudit(w),
		WithConfig(func() config.Config { return config.Defaults() }),
		WithClock(clk.Now), WithLogger(nopLogger()),
		WithStallTimeout(time.Hour),
	)

	o.dispatch(issue("i1", "A-1", "Todo"))
	<-started
	awaitRunning(t, o, 1)

	o.reconcile(context.Background())
	require.Equal(t, 1, o.RuntimeState().RunningCount(), "still-active issue keeps running")

	close(block)
	o.Wait()
}

func TestReconcile_MemoryPostProcessorInvoked(t *testing.T) {
	tr := &fakeTracker{}
	ws := &fakeWorkspaces{}
	pr := &fakeProvider{}
	mp := &recordingMemory{}
	o, _ := testOrchestrator(t, tr, ws, pr, WithMemoryPostProcessor(mp))

	o.dispatch(issue("i1", "A-1", "Todo"))
	o.Wait()

	mp.mu.Lock()
	defer mp.mu.Unlock()
	require.Equal(t, 1, mp.calls, "Part C runs once per terminated attempt")
}

type recordingMemory struct {
	mu    sync.Mutex
	calls int
}

func (m *recordingMemory) PostProcess(context.Context, *RunAttempt) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	return nil
}
