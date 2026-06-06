package orchestrator

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/config"
	"github.com/conductor-sh/conductor/internal/provider"
)

func TestFailureBackoff_ExponentialAndCapped(t *testing.T) {
	maxBackoff := 300_000 * time.Millisecond
	require.Equal(t, 10*time.Second, failureBackoff(1, maxBackoff))
	require.Equal(t, 20*time.Second, failureBackoff(2, maxBackoff))
	require.Equal(t, 40*time.Second, failureBackoff(3, maxBackoff))
	require.Equal(t, 80*time.Second, failureBackoff(4, maxBackoff))
	// Eventually saturates at the cap and never exceeds it.
	require.Equal(t, maxBackoff, failureBackoff(20, maxBackoff))
	require.LessOrEqual(t, failureBackoff(100, maxBackoff), maxBackoff)
}

func TestFailureBackoff_ZeroAttemptTreatedAsFirst(t *testing.T) {
	require.Equal(t, 10*time.Second, failureBackoff(0, 0))
}

func TestScheduleRetry_ContinuationUsesFixedDelay(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC))
	o := New(
		WithConfig(func() config.Config { return config.Defaults() }),
		WithClock(clk.Now),
	)
	a := &RunAttempt{IssueID: "i1", Identifier: "A-1", Attempt: 1}
	o.scheduleRetry(a, "turn_failed", true)

	e, ok := o.store.retryFor("i1")
	require.True(t, ok)
	require.Equal(t, clk.Now().Add(continuationDelay), e.NextEligible)
}

func TestScheduleRetry_FailureUsesBackoff(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC))
	o := New(
		WithConfig(func() config.Config { return config.Defaults() }),
		WithClock(clk.Now),
	)
	a := &RunAttempt{IssueID: "i1", Identifier: "A-1", Attempt: 3}
	o.scheduleRetry(a, "turn_failed", false)

	e, _ := o.store.retryFor("i1")
	require.Equal(t, clk.Now().Add(40*time.Second), e.NextEligible, "attempt 3 → 40s")
}

func TestRetry_NoEarlyRedispatch(t *testing.T) {
	clk := newFakeClock(time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC))
	tr := &fakeTracker{}
	ws := &fakeWorkspaces{}
	pr := &fakeProvider{result: provider.TurnResult{Err: errStub("boom")}}
	w, _ := newCaptureWriter()
	o := New(
		WithTracker(tr), WithWorkspaces(ws),
		WithProvider(pr, config.ProviderConfig{Provider: "anthropic"}),
		WithAudit(w),
		WithConfig(func() config.Config { return config.Defaults() }),
		WithClock(clk.Now), WithLogger(nopLogger()),
	)

	// First dispatch fails and schedules a retry (10s for attempt 1).
	o.dispatch(issue("i1", "A-1", "Todo"))
	o.Wait()
	e, ok := o.store.retryFor("i1")
	require.True(t, ok)
	require.Equal(t, 1, e.Attempt)

	// Before the timer elapses, the issue is not dispatch-eligible.
	cfg := selectionConfigFrom(config.Defaults())
	require.False(t, isEligible(issue("i1", "A-1", "Todo"), o.RuntimeState(), clk.Now(), cfg))

	// After the backoff elapses, it is eligible and the next attempt is 2.
	clk.Advance(10 * time.Second)
	require.True(t, isEligible(issue("i1", "A-1", "Todo"), o.RuntimeState(), clk.Now(), cfg))
	require.Equal(t, 2, o.nextAttemptNumber("i1"))
}
