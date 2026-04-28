// Package orchestrator owns the top-level Conductor poll loop (SPEC §13):
// polling the tracker, claiming candidate issues, dispatching them through
// the Agent Router, applying retry/backoff, performing reconciliation
// (stall detection + tracker-state refresh + memory post-processing), and
// holding the singleton OrchestratorRuntimeState. It is the only owner of
// that state.
//
// Tier 4 (top-level coordinator). Imports Tier 0 through Tier 3.
package orchestrator
