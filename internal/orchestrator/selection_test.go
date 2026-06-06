package orchestrator

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/tracker"
)

func defaultSelCfg() selectionConfig {
	return selectionConfig{
		maxConcurrent:  10,
		maxByState:     map[string]int{},
		activeStates:   []string{"Todo", "In Progress"},
		terminalStates: []string{"Done", "Cancelled", "Closed"},
	}
}

func emptySnap() RuntimeState {
	return RuntimeState{Running: map[string]*RunAttempt{}, Claimed: map[string]struct{}{}, RetryQueued: map[string]retryEntry{}}
}

func ids(issues []tracker.Issue) []string {
	out := make([]string, len(issues))
	for i, is := range issues {
		out[i] = is.ID
	}
	return out
}

func TestSelect_MissingFieldsIneligible(t *testing.T) {
	now := time.Now()
	cfg := defaultSelCfg()
	cases := map[string]tracker.Issue{
		"no id":         {Identifier: "A-1", Title: "t", State: "Todo"},
		"no identifier": {ID: "1", Title: "t", State: "Todo"},
		"no title":      {ID: "1", Identifier: "A-1", State: "Todo"},
		"no state":      {ID: "1", Identifier: "A-1", Title: "t"},
	}
	for name, iss := range cases {
		t.Run(name, func(t *testing.T) {
			require.False(t, isEligible(iss, emptySnap(), now, cfg))
		})
	}
}

func TestSelect_StateGating(t *testing.T) {
	now := time.Now()
	cfg := defaultSelCfg()
	require.True(t, isEligible(issue("1", "A-1", "Todo"), emptySnap(), now, cfg))
	require.True(t, isEligible(issue("1", "A-1", "in progress"), emptySnap(), now, cfg), "case-insensitive active")
	require.False(t, isEligible(issue("1", "A-1", "Done"), emptySnap(), now, cfg), "terminal excluded")
	require.False(t, isEligible(issue("1", "A-1", "Backlog"), emptySnap(), now, cfg), "non-active excluded")
}

func TestSelect_NotRunningOrClaimed(t *testing.T) {
	now := time.Now()
	cfg := defaultSelCfg()
	snap := emptySnap()
	snap.Running["1"] = &RunAttempt{IssueID: "1"}
	require.False(t, isEligible(issue("1", "A-1", "Todo"), snap, now, cfg))

	snap2 := emptySnap()
	snap2.Claimed["2"] = struct{}{}
	require.False(t, isEligible(issue("2", "A-2", "Todo"), snap2, now, cfg))
}

func TestSelect_RetryTimerGate(t *testing.T) {
	cfg := defaultSelCfg()
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	snap := emptySnap()
	snap.RetryQueued["1"] = retryEntry{NextEligible: now.Add(time.Minute)}
	require.False(t, isEligible(issue("1", "A-1", "Todo"), snap, now, cfg), "blocked before timer")
	require.True(t, isEligible(issue("1", "A-1", "Todo"), snap, now.Add(time.Minute), cfg), "eligible after timer")
}

func TestSelect_TodoBlockerRule(t *testing.T) {
	now := time.Now()
	cfg := defaultSelCfg()
	blocked := issue("1", "A-1", "Todo")
	blocked.BlockedBy = []tracker.BlockerRef{{State: strp("In Progress")}}
	require.False(t, isEligible(blocked, emptySnap(), now, cfg), "non-terminal blocker blocks")

	blocked.BlockedBy = []tracker.BlockerRef{{State: strp("Done")}}
	require.True(t, isEligible(blocked, emptySnap(), now, cfg), "terminal blocker clears")

	blocked.BlockedBy = []tracker.BlockerRef{{State: nil}}
	require.False(t, isEligible(blocked, emptySnap(), now, cfg), "unknown blocker state blocks")

	// The blocker rule only applies to Todo.
	inprog := issue("2", "A-2", "In Progress")
	inprog.BlockedBy = []tracker.BlockerRef{{State: strp("In Progress")}}
	require.True(t, isEligible(inprog, emptySnap(), now, cfg), "blocker rule is Todo-only")
}

func TestSelect_GlobalConcurrencyCap(t *testing.T) {
	now := time.Now()
	cfg := defaultSelCfg()
	cfg.maxConcurrent = 2
	cands := []tracker.Issue{issue("1", "A-1", "Todo"), issue("2", "A-2", "Todo"), issue("3", "A-3", "Todo")}
	got := selectDispatch(cands, emptySnap(), now, cfg)
	require.Len(t, got, 2, "global cap limits to 2")
}

func TestSelect_GlobalCapCountsRunning(t *testing.T) {
	now := time.Now()
	cfg := defaultSelCfg()
	cfg.maxConcurrent = 2
	snap := emptySnap()
	snap.Running["r1"] = &RunAttempt{IssueID: "r1", State: "Todo"}
	cands := []tracker.Issue{issue("1", "A-1", "Todo"), issue("2", "A-2", "Todo")}
	got := selectDispatch(cands, snap, now, cfg)
	require.Len(t, got, 1, "one running leaves one global slot")
}

func TestSelect_PerStateCap(t *testing.T) {
	now := time.Now()
	cfg := defaultSelCfg()
	cfg.maxConcurrent = 10
	cfg.maxByState = map[string]int{"Todo": 1}
	cands := []tracker.Issue{
		issue("1", "A-1", "Todo"),
		issue("2", "A-2", "Todo"),
		issue("3", "A-3", "In Progress"),
	}
	got := selectDispatch(cands, emptySnap(), now, cfg)
	// One Todo (cap 1) + the In Progress issue (no cap) = 2.
	require.ElementsMatch(t, []string{"1", "3"}, ids(got))
}

func TestSelect_DispatchSortOrder(t *testing.T) {
	now := time.Now()
	cfg := defaultSelCfg()
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)

	// Priorities: lower first, nil last. Then oldest created_at. Then identifier.
	a := tracker.Issue{ID: "a", Identifier: "Z-9", Title: "t", State: "Todo", Priority: intp(1), CreatedAt: timep(t1)}
	b := tracker.Issue{ID: "b", Identifier: "B-2", Title: "t", State: "Todo", Priority: intp(1), CreatedAt: timep(t0)}
	c := tracker.Issue{ID: "c", Identifier: "C-3", Title: "t", State: "Todo", Priority: nil, CreatedAt: timep(t0)}
	d := tracker.Issue{ID: "d", Identifier: "A-1", Title: "t", State: "Todo", Priority: intp(1), CreatedAt: timep(t0)}

	got := selectDispatch([]tracker.Issue{a, b, c, d}, emptySnap(), now, cfg)
	// b and d share priority 1 + t0; identifier A-1 < B-2, so d before b. a has
	// later created_at. c has nil priority → last.
	require.Equal(t, []string{"d", "b", "a", "c"}, ids(got))
}
