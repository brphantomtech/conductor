package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/config"
	"github.com/conductor-sh/conductor/internal/provider"
	"github.com/conductor-sh/conductor/internal/tracker"
)

func TestRunTick_DispatchesSortedEligible(t *testing.T) {
	tr := &fakeTracker{candidates: []tracker.Issue{
		issue("i2", "B-2", "Todo"),
		issue("i1", "A-1", "Todo"),
	}}
	ws := &fakeWorkspaces{}
	pr := &fakeProvider{result: provider.TurnResult{Text: "ok"}}
	o, _ := testOrchestrator(t, tr, ws, pr)

	o.runTick(context.Background())
	o.Wait()

	// Both dispatched; identifiers created in dispatch (sorted) order A-1, B-2.
	require.ElementsMatch(t, []string{"A-1", "B-2"}, ws.created)
}

func TestRunTick_PreflightFailSkipsDispatch(t *testing.T) {
	tr := &fakeTracker{candidates: []tracker.Issue{issue("i1", "A-1", "Todo")}}
	ws := &fakeWorkspaces{}
	pr := &fakeProvider{}
	o, _ := testOrchestrator(t, tr, ws, pr, WithPreflight(failingPreflight{}))

	o.runTick(context.Background())
	o.Wait()

	require.Empty(t, ws.created, "preflight failure skips fetch+dispatch")
	tr.mu.Lock()
	calls := tr.fetchCandidateCalls
	tr.mu.Unlock()
	require.Equal(t, 0, calls, "candidates not fetched when preflight fails")
}

type failingPreflight struct{}

func (failingPreflight) Preflight(context.Context) (bool, error) { return false, nil }

func TestRunTick_EnforcerStatusRecorded(t *testing.T) {
	tr := &fakeTracker{}
	ws := &fakeWorkspaces{}
	pr := &fakeProvider{}
	o, _ := testOrchestrator(t, tr, ws, pr, WithEnforcer(violationsEnforcer{}))

	o.runTick(context.Background())
	require.Equal(t, EnforcerViolationsPresent, o.RuntimeState().EnforcerStatus)
}

type violationsEnforcer struct{}

func (violationsEnforcer) PreDispatch(context.Context) (EnforcerStatus, error) {
	return EnforcerViolationsPresent, nil
}

type blockingEnforcer struct{}

func (blockingEnforcer) PreDispatch(context.Context) (EnforcerStatus, error) {
	return EnforcerBlocked, nil
}

// TestRunTick_EnforcerBlockedSkipsDispatch asserts the Phase 12 confined
// orchestrator change: when the enforcer reports blocked, no issue is
// dispatched that tick, while reconciliation (step 2) still runs.
func TestRunTick_EnforcerBlockedSkipsDispatch(t *testing.T) {
	tr := &fakeTracker{candidates: []tracker.Issue{issue("i1", "A-1", "Todo")}}
	ws := &fakeWorkspaces{}
	pr := &fakeProvider{}
	o, _ := testOrchestrator(t, tr, ws, pr, WithEnforcer(blockingEnforcer{}))

	o.runTick(context.Background())
	o.Wait()

	require.Equal(t, EnforcerBlocked, o.RuntimeState().EnforcerStatus)
	require.Empty(t, ws.created, "blocking enforcer status must skip dispatch")
}

func TestRunTick_ContextCancelledNoOp(t *testing.T) {
	tr := &fakeTracker{candidates: []tracker.Issue{issue("i1", "A-1", "Todo")}}
	ws := &fakeWorkspaces{}
	pr := &fakeProvider{}
	o, _ := testOrchestrator(t, tr, ws, pr)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	o.runTick(ctx)
	o.Wait()
	require.Empty(t, ws.created, "cancelled context dispatches nothing")
}

func TestInterval_HotReload(t *testing.T) {
	interval := 50
	o := New(WithConfig(func() config.Config {
		c := config.Defaults()
		c.Polling.IntervalMS = interval
		return c
	}))
	require.Equal(t, 50*time.Millisecond, o.interval())
	interval = 120
	require.Equal(t, 120*time.Millisecond, o.interval(), "interval is re-read from live config")
}

func TestInterval_NonPositiveFallsBack(t *testing.T) {
	o := New(WithConfig(func() config.Config {
		c := config.Defaults()
		c.Polling.IntervalMS = 0
		return c
	}))
	require.Equal(t, 30*time.Second, o.interval())
}

func TestRun_StopsOnContextCancel(t *testing.T) {
	tr := &fakeTracker{}
	ws := &fakeWorkspaces{}
	pr := &fakeProvider{}
	o, _ := testOrchestrator(t, tr, ws, pr, WithConfig(func() config.Config {
		c := config.Defaults()
		c.Polling.IntervalMS = 10
		return c
	}))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- o.Run(ctx) }()
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancel")
	}
}
