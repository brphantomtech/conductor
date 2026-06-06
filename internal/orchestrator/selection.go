package orchestrator

import (
	"sort"
	"strings"
	"time"

	"github.com/conductor-sh/conductor/internal/tracker"
)

// selectionConfig is the slice of config the candidate selector reads. It is
// rebuilt from the live config each tick so hot-reloaded concurrency caps and
// state lists take effect immediately (SPEC §13.2).
type selectionConfig struct {
	maxConcurrent  int
	maxByState     map[string]int
	activeStates   []string
	terminalStates []string
}

// selectDispatch returns the dispatch-eligible candidates in dispatch-priority
// order, respecting global and per-state concurrency slots (SPEC §13.3). The
// returned slice never exceeds the available global slots: per-state and
// global caps are consumed as candidates are selected so a single tick cannot
// over-dispatch.
func selectDispatch(candidates []tracker.Issue, snap RuntimeState, now time.Time, cfg selectionConfig) []tracker.Issue {
	eligible := make([]tracker.Issue, 0, len(candidates))
	for _, iss := range candidates {
		if isEligible(iss, snap, now, cfg) {
			eligible = append(eligible, iss)
		}
	}
	sortDispatch(eligible)

	globalRunning := snap.RunningCount()
	stateRunning := map[string]int{}
	for _, a := range snap.Running {
		stateRunning[strings.ToLower(a.State)]++
	}

	out := make([]tracker.Issue, 0, len(eligible))
	for _, iss := range eligible {
		if cfg.maxConcurrent > 0 && globalRunning >= cfg.maxConcurrent {
			break // no global slots remain
		}
		key := strings.ToLower(iss.State)
		if limit, ok := cfg.maxByState[iss.State]; ok && limit > 0 && stateRunning[key] >= limit {
			continue // this state is saturated; another may still fit
		}
		out = append(out, iss)
		globalRunning++
		stateRunning[key]++
	}
	return out
}

// isEligible applies every SPEC §13.3 eligibility predicate except the
// incremental concurrency accounting, which selectDispatch performs as it
// consumes slots.
func isEligible(iss tracker.Issue, snap RuntimeState, now time.Time, cfg selectionConfig) bool {
	// Required fields must all be present.
	if iss.ID == "" || iss.Identifier == "" || iss.Title == "" || iss.State == "" {
		return false
	}
	// State must be active and not terminal.
	if !containsState(cfg.activeStates, iss.State) || containsState(cfg.terminalStates, iss.State) {
		return false
	}
	// Not already running or claimed.
	if _, ok := snap.Running[iss.ID]; ok {
		return false
	}
	if _, ok := snap.Claimed[iss.ID]; ok {
		return false
	}
	// A pending retry timer that has not elapsed holds the issue in
	// RetryQueued; it must not be re-dispatched early (SPEC §13.4).
	if e, ok := snap.RetryQueued[iss.ID]; ok && now.Before(e.NextEligible) {
		return false
	}
	// Blocker rule: a Todo issue dispatches only when every blocker is
	// terminal (SPEC §13.3).
	if strings.EqualFold(iss.State, "todo") && !blockersClear(iss, cfg.terminalStates) {
		return false
	}
	return true
}

// blockersClear reports whether every blocked_by entry resolves to a terminal
// state. A blocker whose state is unknown (nil) is treated as not-terminal so
// dispatch waits rather than racing ahead.
func blockersClear(iss tracker.Issue, terminalStates []string) bool {
	for _, b := range iss.BlockedBy {
		if b.State == nil || !containsState(terminalStates, *b.State) {
			return false
		}
	}
	return true
}

// sortDispatch orders issues by the SPEC §13.3 dispatch sort: priority
// ascending (nil last), then created_at oldest first (nil last), then
// identifier lexicographically.
func sortDispatch(issues []tracker.Issue) {
	sort.SliceStable(issues, func(i, j int) bool {
		a, b := issues[i], issues[j]
		if c := comparePriority(a.Priority, b.Priority); c != 0 {
			return c < 0
		}
		if c := compareCreatedAt(a.CreatedAt, b.CreatedAt); c != 0 {
			return c < 0
		}
		return a.Identifier < b.Identifier
	})
}

// comparePriority orders two nullable priorities with nil sorting last.
// Returns -1 if a precedes b, +1 if b precedes a, 0 if equal.
func comparePriority(a, b *int) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return 1 // nil sorts last
	case b == nil:
		return -1
	case *a < *b:
		return -1
	case *a > *b:
		return 1
	default:
		return 0
	}
}

// compareCreatedAt orders two nullable timestamps oldest-first with nil last.
func compareCreatedAt(a, b *time.Time) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return 1
	case b == nil:
		return -1
	case a.Before(*b):
		return -1
	case a.After(*b):
		return 1
	default:
		return 0
	}
}

// containsState reports whether target appears in states, compared after
// lowercasing per SPEC §4.2.
func containsState(states []string, target string) bool {
	for _, s := range states {
		if equalState(s, target) {
			return true
		}
	}
	return false
}

// equalState compares two tracker states case-insensitively (SPEC §4.2).
func equalState(a, b string) bool { return strings.EqualFold(a, b) }
