package orchestrator

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRuntimeStore_InertDefaults(t *testing.T) {
	s := newRuntimeStore()
	snap := s.snapshot()
	require.Equal(t, EnforcerClear, snap.EnforcerStatus)
	require.Equal(t, KnowledgeDisabled, snap.KnowledgeIndexStatus)
	require.Equal(t, 0, snap.PendingGCTasks)
	require.Empty(t, snap.DocStoreSyncStatus)
	require.Equal(t, 0, snap.RunningCount())
}

func TestRuntimeStore_ClaimIsExclusive(t *testing.T) {
	s := newRuntimeStore()
	require.True(t, s.claim("i1"))
	require.False(t, s.claim("i1"), "second claim must fail")

	s.markRunning(&RunAttempt{IssueID: "i1", State: "Todo"})
	require.False(t, s.claim("i1"), "claim must fail while running")
	require.Equal(t, 1, s.runningCount())

	s.release("i1")
	require.True(t, s.claim("i1"), "claim succeeds after release")
}

func TestRuntimeStore_SnapshotIsACopy(t *testing.T) {
	s := newRuntimeStore()
	s.claim("i1")
	s.markRunning(&RunAttempt{IssueID: "i1", State: "Todo", Pipeline: []string{"coder"}})

	snap := s.snapshot()
	// Mutating the snapshot must not affect the live store.
	snap.Running["i1"].State = "MUTATED"
	snap.Running["i2"] = &RunAttempt{IssueID: "i2"}
	delete(snap.Claimed, "i1")

	live := s.snapshot()
	require.Equal(t, "Todo", live.Running["i1"].State)
	require.NotContains(t, live.Running, "i2")
}

func TestRuntimeStore_RunningCountByState(t *testing.T) {
	s := newRuntimeStore()
	s.claim("a")
	s.markRunning(&RunAttempt{IssueID: "a", State: "Todo"})
	s.claim("b")
	s.markRunning(&RunAttempt{IssueID: "b", State: "todo"}) // case-insensitive
	s.claim("c")
	s.markRunning(&RunAttempt{IssueID: "c", State: "In Progress"})

	snap := s.snapshot()
	require.Equal(t, 2, snap.RunningCountByState("Todo"))
	require.Equal(t, 1, snap.RunningCountByState("in progress"))
}

func TestRuntimeStore_RetryLifecycle(t *testing.T) {
	s := newRuntimeStore()
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	s.recordRetry("i1", retryEntry{Attempt: 1, NextEligible: now.Add(time.Second), LastReason: "turn_failed"})

	e, ok := s.retryFor("i1")
	require.True(t, ok)
	require.Equal(t, 1, e.Attempt)

	require.False(t, s.retryReady("i1", now), "not ready before NextEligible")
	require.True(t, s.retryReady("i1", now.Add(time.Second)), "ready at NextEligible")

	s.clearRetry("i1")
	_, ok = s.retryFor("i1")
	require.False(t, ok)
}

func TestRuntimeStore_ConcurrentMutationsConsistent(t *testing.T) {
	s := newRuntimeStore()
	const n = 200
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := string(rune('a'+i%26)) + time.Duration(i).String()
			if s.claim(id) {
				s.markRunning(&RunAttempt{IssueID: id, State: "Todo"})
				s.release(id)
			}
		}(i)
	}
	wg.Wait()
	// Every claim was released — no stuck running or claimed entries.
	snap := s.snapshot()
	require.Equal(t, 0, snap.RunningCount())
	require.Empty(t, snap.Claimed)
}
