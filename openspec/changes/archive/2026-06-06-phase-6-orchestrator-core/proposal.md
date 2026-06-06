# Phase 6 — Orchestrator Core

## Why

Phases 1–5 delivered every substrate the runtime needs — configuration, persistence,
the HARNESS.md loader, provider adapters, tracker adapters, and isolated workspaces —
but nothing yet drives them. There is no loop that polls the tracker, claims an issue,
dispatches an agent run, retries on failure, and releases the claim on a terminal state.
`conductor start` still prints "orchestrator not yet implemented". Phase 6 supplies that
missing top-level coordinator end-to-end with a single hardcoded `coder` pipeline (the
multi-role Agent Router lands in Phase 7), making Conductor capable of doing real work
for the first time.

Authoritative scope: [docs/phases.md](../../../docs/phases.md) → "Phase 6 — Orchestrator
Core"; SPEC §13 (Orchestrator), §4.1.11 (Orchestrator Runtime State), §4.1.12 (Run
Attempt), §23.4 (run-attempt errors).

## What Changes

- **New `internal/orchestrator` package (Tier 4)** that owns the top-level run loop and
  the single authoritative runtime state:
  - **Orchestrator runtime state** (SPEC §4.1.11): the singleton in-memory state holding
    `running`, `claimed`, `retry_attempts`, concurrency counts, and the enum status
    fields (`enforcer_status`, `knowledge_index_status`, `doc_store_sync_status`,
    `pending_gc_tasks`). This phase populates the fields it owns; later phases fill the
    enforcer/knowledge/docstore values.
  - **Run attempt entity + lifecycle** (SPEC §4.1.12): the `RunAttempt` record (with
    `pipeline`, `pipeline_index`, `validation_results` fields) and the state transitions
    that create, dispatch, retry, and release it.
  - **Internal state machine** (SPEC §13.1): the eight orchestration states
    (`Unclaimed`, `Classifying`, `Claimed`, `Running`, `Validating`, `RetryQueued`,
    `EnforcerBlocked`, `Released`) — note Phase 6 exercises the subset reachable without
    the router/enforcer/validation engines.
  - **Poll loop** (SPEC §13.2): the tick pipeline — reconcile active runs, fetch
    candidates, sort by dispatch priority, dispatch until slots are exhausted, notify
    observability — driven on the `polling.interval_ms` cadence with hot-reloadable
    interval. Steps for the not-yet-built engines (enforcer pre-dispatch, doc-store sync,
    classification) are stubbed as no-ops with clearly marked extension points.
  - **Candidate selection + dispatch sort** (SPEC §13.3): the dispatch-eligibility
    predicate (field presence, active/non-terminal state, not running, not claimed,
    global + per-state concurrency slots, the `Todo` blocker rule) and the
    `priority → created_at → identifier` sort order.
  - **Retry and backoff** (SPEC §13.4): fixed 1000 ms continuation retry on clean worker
    exit; `min(10000 * 2^(attempt-1), max_retry_backoff_ms)` exponential backoff on
    failure-driven retry.
  - **Reconciliation Parts A + B** (SPEC §13.5): stall detection and tracker-state
    refresh. Part C (memory post-processing) is stubbed behind an extension point since
    the Memory Manager is Phase 9.
  - **Startup terminal cleanup** (SPEC §13.6): on boot, query the tracker for terminal
    issues, remove their workspaces, clear stale runtime state, and continue on failure.
- **Single hardcoded `coder` dispatch path**: dispatch creates a workspace
  (`internal/workspace`), renders the `coder` prompt template (`internal/harness`),
  starts a provider turn (`internal/provider`), and applies the run-attempt lifecycle.
  No routing rules, classification, or multi-role pipelines yet (Phase 7).
- **Run-attempt error classification**: the SPEC §23.4 sentinels already declared in
  `internal/orchestrator/errors.go` are wired into the lifecycle so failures map to the
  correct identifiers and audit reasons.
- **Audit integration**: orchestrator lifecycle events (`IssueDispatched`,
  `IssueReleased`, `IssueCancelled`, `RunAttemptStarted`, `RunAttemptEnded`,
  `RetryScheduled`, `SessionStalled`) written through `internal/audit` (SPEC §17.2).
- **`conductor start` wired** to construct and run the orchestrator (replacing the
  placeholder), honoring `--dry-run` for a no-dispatch config/boot check.
- **Unit tests** covering candidate selection, dispatch sort, the blocker rule,
  concurrency caps, retry/backoff timing, reconciliation Parts A + B, and startup
  cleanup, using fake tracker/provider/workspace collaborators.

## Capabilities

### New Capabilities

- `orchestrator-core`: the poll-loop state machine, orchestrator runtime state, run
  attempt entity + lifecycle, candidate selection and dispatch sort, retry/backoff,
  reconciliation (stall detection + tracker-state refresh), startup terminal cleanup,
  the single hardcoded `coder` dispatch path, and SPEC §23.4 run-attempt error
  classification.

### Modified Capabilities

None. `harness-loader`, `provider-adapter-layer`, `tracker-adapters`, and
`workspace-management` are consumed unchanged.

## Impact

- **Affected specs**: new capability `orchestrator-core` (delta in
  `specs/orchestrator-core/spec.md`).
- **Affected code**: `internal/orchestrator/` (new implementation; currently only
  `doc.go` + `errors.go` stubs). `cmd/conductor/cmd/start.go` (wire the real
  orchestrator). `internal/audit` (add any missing orchestrator event constants). Config
  already models `polling`, `agent` (concurrency caps), `routing`, and `tracker`
  active/terminal states from Phase 1, so no config schema change is expected.
- **Consumes (unchanged)**: `internal/tracker` (candidate fetch, state refresh),
  `internal/workspace` (create/remove, agent subprocess), `internal/provider` (turn
  dispatch), `internal/harness` (`coder` prompt render), `internal/config`,
  `internal/db`, `internal/audit`.

### Non-goals

- The Agent Router, issue classification, routing-rule evaluation, and multi-role
  pipelines (Phase 7) — Phase 6 dispatches a single hardcoded `coder` role.
- The Validation Pipeline run after each turn (Phase 8) — the `Validating` state and
  `validation_results` field exist but no checks execute.
- The Harness Enforcer pre-dispatch check and scheduled GC (Phase 12) — `enforcer_status`
  stays `clear`; the poll-loop step is a no-op extension point.
- Memory post-processing / consolidation in reconciliation Part C (Phase 9).
- Doc-store sync in the poll loop (Phase 11) and Knowledge Engine indexing (Phase 10).
- The HTTP API / WebSocket observability surface (Phase 14) — Phase 6 exposes runtime
  state through an in-process accessor only.
