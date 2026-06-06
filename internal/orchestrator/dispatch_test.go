package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/audit"
	"github.com/conductor-sh/conductor/internal/config"
	"github.com/conductor-sh/conductor/internal/provider"
	"github.com/conductor-sh/conductor/internal/workspace"
)

// testOrchestrator builds an Orchestrator wired with the supplied fakes and a
// capture sink, plus a fixed clock.
func testOrchestrator(t *testing.T, tr Tracker, ws Workspaces, pr Provider, opts ...Option) (*Orchestrator, *captureSink) {
	t.Helper()
	w, sink := newCaptureWriter()
	clk := newFakeClock(time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC))
	base := []Option{
		WithTracker(tr),
		WithWorkspaces(ws),
		WithProvider(pr, config.ProviderConfig{Provider: "anthropic"}),
		WithAudit(w),
		WithConfig(func() config.Config { return config.Defaults() }),
		WithClock(clk.Now),
		WithLogger(nopLogger()),
	}
	o := New(append(base, opts...)...)
	return o, sink
}

func TestDispatch_HappyPathReleasesClaim(t *testing.T) {
	tr := &fakeTracker{}
	ws := &fakeWorkspaces{}
	pr := &fakeProvider{result: provider.TurnResult{Text: "done"}}
	o, sink := testOrchestrator(t, tr, ws, pr)

	require.True(t, o.dispatch(issue("i1", "A-1", "Todo")))
	o.Wait()

	snap := o.RuntimeState()
	require.Equal(t, 0, snap.RunningCount(), "no stuck running entry")
	require.NotContains(t, snap.Claimed, "i1", "claim released")
	require.Empty(t, snap.RetryQueued, "success does not schedule a retry")

	require.Equal(t, []string{"A-1"}, ws.created)
	require.Equal(t, 1, sink.count(audit.EventIssueDispatched))
	require.Equal(t, 1, sink.count(audit.EventRunAttemptStarted))
	require.Equal(t, 1, sink.count(audit.EventRunAttemptEnded))
	require.Equal(t, 1, sink.count(audit.EventIssueReleased))
}

func TestDispatch_DuplicateClaimRejected(t *testing.T) {
	tr := &fakeTracker{}
	ws := &fakeWorkspaces{}
	// Block the turn so the first dispatch stays running.
	pr := &fakeProvider{block: make(chan struct{})}
	o, _ := testOrchestrator(t, tr, ws, pr)

	require.True(t, o.dispatch(issue("i1", "A-1", "Todo")))
	require.False(t, o.dispatch(issue("i1", "A-1", "Todo")), "second dispatch of same issue rejected")

	close(pr.block)
	o.Wait()
}

func TestDispatch_WorkspaceCreationFailure(t *testing.T) {
	tr := &fakeTracker{}
	ws := &fakeWorkspaces{createErr: workspace.ErrWorkspaceCreationFailed}
	pr := &fakeProvider{}
	o, sink := testOrchestrator(t, tr, ws, pr)

	o.dispatch(issue("i1", "A-1", "Todo"))
	o.Wait()

	snap := o.RuntimeState()
	require.Equal(t, 0, snap.RunningCount(), "claim released on failure")
	require.NotContains(t, snap.Claimed, "i1")
	require.Contains(t, snap.RetryQueued, "i1", "failure schedules a retry")
	require.Equal(t, ErrWorkspaceCreationFailed.Error(), snap.RetryQueued["i1"].LastReason)
	require.Equal(t, 1, sink.count(audit.EventRunAttemptEnded))
	require.Equal(t, 1, sink.count(audit.EventRetryScheduled))
}

func TestDispatch_HookFailureClassified(t *testing.T) {
	tr := &fakeTracker{}
	ws := &fakeWorkspaces{createErr: workspace.ErrHookFailed}
	pr := &fakeProvider{}
	o, _ := testOrchestrator(t, tr, ws, pr)

	o.dispatch(issue("i1", "A-1", "Todo"))
	o.Wait()

	snap := o.RuntimeState()
	require.Equal(t, ErrHookFailed.Error(), snap.RetryQueued["i1"].LastReason)
}

func TestDispatch_RenderFailureClassified(t *testing.T) {
	tr := &fakeTracker{}
	ws := &fakeWorkspaces{}
	pr := &fakeProvider{}
	o, _ := testOrchestrator(t, tr, ws, pr,
		WithRenderer(func(string, map[string]any) (string, error) { return "", context.Canceled }),
	)

	o.dispatch(issue("i1", "A-1", "Todo"))
	o.Wait()

	snap := o.RuntimeState()
	require.Equal(t, ErrPromptRenderFailed.Error(), snap.RetryQueued["i1"].LastReason)
}

func TestDispatch_TurnFailureClassified(t *testing.T) {
	tr := &fakeTracker{}
	ws := &fakeWorkspaces{}
	pr := &fakeProvider{result: provider.TurnResult{Err: context.DeadlineExceeded}}
	// A provider that surfaces a generic (non-context) error → turn_failed.
	pr2 := &fakeProvider{result: provider.TurnResult{Err: errStub("boom")}}
	o, _ := testOrchestrator(t, tr, ws, pr2)

	o.dispatch(issue("i1", "A-1", "Todo"))
	o.Wait()
	require.Equal(t, ErrTurnFailed.Error(), o.RuntimeState().RetryQueued["i1"].LastReason)

	_ = pr // referenced for clarity that DeadlineExceeded is covered elsewhere
}

type errStub string

func (e errStub) Error() string { return string(e) }
