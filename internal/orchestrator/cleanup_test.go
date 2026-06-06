package orchestrator

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/tracker"
)

func TestStartupCleanup_RemovesTerminalWorkspaces(t *testing.T) {
	tr := &fakeTracker{byStatesAll: []tracker.Issue{
		issue("i1", "A-1", "Done"),
		issue("i2", "A-2", "Cancelled"),
	}}
	ws := &fakeWorkspaces{}
	pr := &fakeProvider{}
	o, _ := testOrchestrator(t, tr, ws, pr)

	// Seed stale runtime state to prove it is cleared.
	o.store.claim("i1")

	o.startupCleanup(context.Background())

	require.ElementsMatch(t, []string{"A-1", "A-2"}, ws.removedIdentifiers())
	require.NotContains(t, o.RuntimeState().Claimed, "i1", "stale claim cleared")
}

func TestStartupCleanup_RemoveFailureDoesNotAbort(t *testing.T) {
	tr := &fakeTracker{byStatesAll: []tracker.Issue{
		issue("i1", "A-1", "Done"),
		issue("i2", "A-2", "Done"),
	}}
	ws := &fakeWorkspaces{removeErr: errors.New("disk gone")}
	pr := &fakeProvider{}
	o, _ := testOrchestrator(t, tr, ws, pr)

	// Should not panic or abort; both issues are attempted.
	require.NotPanics(t, func() { o.startupCleanup(context.Background()) })
}

func TestStartupCleanup_FetchFailureLogged(t *testing.T) {
	tr := &fakeTracker{byStatesErr: errors.New("tracker down")}
	ws := &fakeWorkspaces{}
	pr := &fakeProvider{}
	o, _ := testOrchestrator(t, tr, ws, pr)

	require.NotPanics(t, func() { o.startupCleanup(context.Background()) })
	require.Empty(t, ws.removedIdentifiers())
}
