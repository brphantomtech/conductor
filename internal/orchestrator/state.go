package orchestrator

import (
	"sync"
	"time"
)

// State is one of the internal orchestration states from SPEC §13.1. These
// are distinct from tracker states: they describe where an issue sits inside
// the orchestrator's own lifecycle. Phase 6 exercises the subset reachable
// without the router (Classifying), validation engine (Validating), and
// enforcer (EnforcerBlocked); those states are defined here so the shape is
// stable across phases.
type State string

// The eight orchestration states defined by SPEC §13.1.
const (
	StateUnclaimed       State = "unclaimed"
	StateClassifying     State = "classifying"
	StateClaimed         State = "claimed"
	StateRunning         State = "running"
	StateValidating      State = "validating"
	StateRetryQueued     State = "retry_queued"
	StateEnforcerBlocked State = "enforcer_blocked"
	StateReleased        State = "released"
)

// Outcome is the terminal classification of a run attempt.
type Outcome string

// Run-attempt outcomes. OutcomePending is the non-terminal zero value.
const (
	OutcomePending   Outcome = ""
	OutcomeSucceeded Outcome = "succeeded"
	OutcomeFailed    Outcome = "failed"
	OutcomeCancelled Outcome = "cancelled"
	OutcomeStalled   Outcome = "stalled"
	OutcomeTimedOut  Outcome = "timed_out"
)

// EnforcerStatus mirrors SPEC §4.1.11 `enforcer_status`.
type EnforcerStatus string

// Enforcer status values. Phase 6 holds Clear until Phase 12 wires the
// Harness Enforcer.
const (
	EnforcerClear             EnforcerStatus = "clear"
	EnforcerViolationsPresent EnforcerStatus = "violations_present"
	EnforcerBlocked           EnforcerStatus = "blocked"
)

// KnowledgeIndexStatus mirrors SPEC §4.1.11 `knowledge_index_status`.
type KnowledgeIndexStatus string

// Knowledge index status values. Phase 6 holds Disabled until Phase 10 wires
// the Knowledge Engine.
const (
	KnowledgeReady    KnowledgeIndexStatus = "ready"
	KnowledgeIndexing KnowledgeIndexStatus = "indexing"
	KnowledgeStale    KnowledgeIndexStatus = "stale"
	KnowledgeDisabled KnowledgeIndexStatus = "disabled"
)

// SyncStatus is a per-doc-store sync state placeholder (SPEC §4.1.11
// `doc_store_sync_status`). The Doc Store Manager (Phase 11) owns the
// canonical values; Phase 6 leaves the map empty.
type SyncStatus string

// RunAttempt is one execution attempt for one issue (SPEC §4.1.12). Phase 6
// always uses the single hardcoded pipeline `[coder]`; the Pipeline and
// PipelineIndex fields exist so Phase 7's router can populate them without a
// shape change. ValidationResults is the slot Phase 8 fills.
type RunAttempt struct {
	// ID is the unique attempt identifier (16-byte hex).
	ID string
	// IssueID is the tracker-internal stable id (runtime map key).
	IssueID string
	// Identifier is the human-readable ticket key (logs, workspace naming).
	Identifier string
	// State is the tracker state the issue held when the attempt began.
	// Used by per-state concurrency accounting during reconciliation.
	State string
	// Pipeline is the ordered agent roles for this attempt. Phase 6: [coder].
	Pipeline []string
	// PipelineIndex is the current role index. Phase 6: always 0.
	PipelineIndex int
	// Attempt is the 1-based attempt number for this issue.
	Attempt int
	// StartedAt is when the attempt began (injected clock).
	StartedAt time.Time
	// EndedAt is when the attempt terminated; zero while running.
	EndedAt time.Time
	// Outcome is the terminal classification; OutcomePending while running.
	Outcome Outcome
	// FailureReason is the SPEC §23.4 sentinel identifier on failure.
	FailureReason string
	// ValidationResults is the slot Phase 8 populates per turn (SPEC §4.1.12).
	ValidationResults []any
}

// clone returns a deep-enough copy of the attempt so a snapshot consumer
// cannot mutate the live runtime state.
func (a *RunAttempt) clone() *RunAttempt {
	if a == nil {
		return nil
	}
	cp := *a
	if a.Pipeline != nil {
		cp.Pipeline = append([]string(nil), a.Pipeline...)
	}
	if a.ValidationResults != nil {
		cp.ValidationResults = append([]any(nil), a.ValidationResults...)
	}
	return &cp
}

// retryEntry records the pending retry timer for one issue (SPEC §13.4).
type retryEntry struct {
	// Attempt is the attempt number that just failed (drives the backoff).
	Attempt int
	// NextEligible is the earliest time the issue may be re-dispatched.
	NextEligible time.Time
	// LastReason is the SPEC §23.4 sentinel that triggered the retry.
	LastReason string
}

// RuntimeState is the SPEC §4.1.11 snapshot view of the orchestrator's
// authoritative state. It is a value type returned by RuntimeStore.Snapshot;
// callers own their copy and cannot affect the live state by mutating it.
type RuntimeState struct {
	// Running maps issue id → the in-flight run attempt.
	Running map[string]*RunAttempt
	// Claimed is the set of issue ids reserved against duplicate dispatch.
	Claimed map[string]struct{}
	// RetryQueued maps issue id → its pending retry timer.
	RetryQueued map[string]retryEntry
	// EnforcerStatus is Clear in Phase 6 (Phase 12 owns transitions).
	EnforcerStatus EnforcerStatus
	// KnowledgeIndexStatus is Disabled in Phase 6 (Phase 10 owns it).
	KnowledgeIndexStatus KnowledgeIndexStatus
	// PendingGCTasks is 0 in Phase 6 (Phase 12 owns it).
	PendingGCTasks int
	// DocStoreSyncStatus is empty in Phase 6 (Phase 11 owns it).
	DocStoreSyncStatus map[string]SyncStatus
}

// RunningCount returns the number of in-flight attempts.
func (s RuntimeState) RunningCount() int { return len(s.Running) }

// RunningCountByState returns the number of in-flight attempts whose issue
// held the given tracker state at dispatch, compared case-insensitively.
func (s RuntimeState) RunningCountByState(state string) int {
	n := 0
	for _, a := range s.Running {
		if equalState(a.State, state) {
			n++
		}
	}
	return n
}

// RuntimeStore is the single authoritative owner of the orchestrator runtime
// state (SPEC §4.1.11). Every mutation is serialized under one mutex so no
// two goroutines — the poll loop, per-run workers, reconciliation — can tear
// the state. Reads are served as deep-copied snapshots, so the live maps are
// never exposed.
type RuntimeStore struct {
	mu          sync.Mutex
	running     map[string]*RunAttempt
	claimed     map[string]struct{}
	retryQueued map[string]retryEntry

	enforcerStatus EnforcerStatus
	knowledgeIndex KnowledgeIndexStatus
	pendingGCTasks int
	docStoreSync   map[string]SyncStatus
}

// newRuntimeStore constructs an empty store with the SPEC §4.1.11 inert
// defaults for the engine-owned status fields.
func newRuntimeStore() *RuntimeStore {
	return &RuntimeStore{
		running:        map[string]*RunAttempt{},
		claimed:        map[string]struct{}{},
		retryQueued:    map[string]retryEntry{},
		enforcerStatus: EnforcerClear,
		knowledgeIndex: KnowledgeDisabled,
		pendingGCTasks: 0,
		docStoreSync:   map[string]SyncStatus{},
	}
}

// claim reserves an issue against duplicate dispatch. It returns false when
// the issue is already claimed or running, leaving the state untouched.
func (s *RuntimeStore) claim(issueID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.claimed[issueID]; ok {
		return false
	}
	if _, ok := s.running[issueID]; ok {
		return false
	}
	s.claimed[issueID] = struct{}{}
	return true
}

// markRunning promotes a claimed issue to running, recording its attempt.
// The claim is consumed. Safe to call only after a successful claim.
func (s *RuntimeStore) markRunning(a *RunAttempt) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.claimed, a.IssueID)
	s.running[a.IssueID] = a
}

// release removes any claim and running entry for the issue (SPEC §13.1
// Released). Idempotent.
func (s *RuntimeStore) release(issueID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.claimed, issueID)
	delete(s.running, issueID)
}

// recordRetry queues a retry timer for the issue and drops any claim/running
// entry (the failed attempt is finished). The issue is now RetryQueued.
func (s *RuntimeStore) recordRetry(issueID string, e retryEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.claimed, issueID)
	delete(s.running, issueID)
	s.retryQueued[issueID] = e
}

// retryFor returns the pending retry entry for the issue, if any.
func (s *RuntimeStore) retryFor(issueID string) (retryEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.retryQueued[issueID]
	return e, ok
}

// clearRetry removes a pending retry timer (the issue is being re-dispatched
// or released).
func (s *RuntimeStore) clearRetry(issueID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.retryQueued, issueID)
}

// retryReady reports whether the issue has a retry timer that has elapsed by
// now. A RetryQueued issue whose timer has not elapsed must not be
// re-dispatched (SPEC §13.4).
func (s *RuntimeStore) retryReady(issueID string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.retryQueued[issueID]
	if !ok {
		return false
	}
	return !now.Before(e.NextEligible)
}

// runningCount returns the number of in-flight attempts.
func (s *RuntimeStore) runningCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.running)
}

// runningIssueIDs returns the ids of all in-flight attempts.
func (s *RuntimeStore) runningIssueIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.running))
	for id := range s.running {
		out = append(out, id)
	}
	return out
}

// setEnforcerStatus updates the enforcer status enum (Phase 12 hook).
func (s *RuntimeStore) setEnforcerStatus(v EnforcerStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enforcerStatus = v
}

// SetPendingGCTasks records the count of open GC tasks the enforcer has created
// (SPEC §4.1.11 pending_gc_tasks). The scheduled GC worker (Phase 12) calls it
// after a GC run so RuntimeState reflects outstanding enforcement debt.
func (s *RuntimeStore) SetPendingGCTasks(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n < 0 {
		n = 0
	}
	s.pendingGCTasks = n
}

// PendingGCTasks reports the orchestrator's exported pending GC task count so
// the enforcer can accumulate across GC runs.
func (o *Orchestrator) SetPendingGCTasks(n int) { o.store.SetPendingGCTasks(n) }

// snapshot returns a deep copy of the runtime state. Mutating the returned
// value cannot affect the live store.
func (s *RuntimeStore) snapshot() RuntimeState {
	s.mu.Lock()
	defer s.mu.Unlock()

	running := make(map[string]*RunAttempt, len(s.running))
	for id, a := range s.running {
		running[id] = a.clone()
	}
	claimed := make(map[string]struct{}, len(s.claimed))
	for id := range s.claimed {
		claimed[id] = struct{}{}
	}
	retry := make(map[string]retryEntry, len(s.retryQueued))
	for id, e := range s.retryQueued {
		retry[id] = e
	}
	docSync := make(map[string]SyncStatus, len(s.docStoreSync))
	for id, st := range s.docStoreSync {
		docSync[id] = st
	}
	return RuntimeState{
		Running:              running,
		Claimed:              claimed,
		RetryQueued:          retry,
		EnforcerStatus:       s.enforcerStatus,
		KnowledgeIndexStatus: s.knowledgeIndex,
		PendingGCTasks:       s.pendingGCTasks,
		DocStoreSyncStatus:   docSync,
	}
}
