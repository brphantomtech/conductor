## Context

Phase 6 is the keystone of Conductor: the Tier-4 coordinator that finally drives the
substrate built in Phases 1–5. Everything it needs already exists behind stable
interfaces — `tracker.Adapter` (candidate fetch + state refresh), `workspace.Manager`
(create/remove + `AgentCommand`), `provider.Adapter` (`StartTurn`/`ContinueTurn`),
`harness` (prompt render), `config` (polling/agent/routing/tracker), `audit.Writer`, and
`internal/db`. The package currently holds only `doc.go` and `errors.go` (the SPEC §23.4
sentinels are already declared). `cmd/conductor/cmd/start.go` prints a placeholder.

The SPEC anchors the contract: §13 (state machine, poll loop, candidate selection,
retry/backoff, reconciliation, startup cleanup), §4.1.11 (runtime state), §4.1.12 (run
attempt), §17.2 (audit events), §23.4 (error sentinels). Several SPEC §13.2 poll-loop
steps belong to engines that do not exist yet (Harness Enforcer → Phase 12, doc-store
sync → Phase 11, classification/router → Phase 7); the design must preserve their
ordering as inert seams without faking behavior.

The codebase convention (visible in Phases 2–5) is: a constructor with functional
`Option`s, injected clock for deterministic tests, injected fakes for collaborators,
sentinel errors whose string values match the SPEC verbatim, and audit emission through a
small `emit` helper. Phase 6 follows the same shape.

## Goals / Non-Goals

**Goals:**

- A correct, testable poll-loop state machine that polls, claims, dispatches a single
  `coder` turn, retries with the SPEC backoff, reconciles, and releases — end-to-end.
- A single authoritative, serialized runtime state with an accessor for later phases
  (API in Phase 14) to read snapshots.
- Deterministic tests via an injected clock and a manual tick driver, with fake
  tracker/provider/workspace collaborators — no real network, no real subprocess.
- Preserve SPEC §13.2 step ordering with clearly marked no-op seams for not-yet-built
  engines.

**Non-Goals:**

- Agent Router, classification, routing rules, multi-role pipelines (Phase 7).
- Validation Pipeline execution (Phase 8), Memory post-processing (Phase 9), Knowledge
  indexing (Phase 10), Doc-store sync (Phase 11), Harness Enforcer (Phase 12).
- HTTP/WebSocket surface (Phase 14) — only an in-process state accessor.
- Container isolation (Phase 17) — subprocess isolation only, via the existing
  `workspace.Manager.AgentCommand`.

## Decisions

### Single-goroutine state ownership with a serialized command channel

The runtime state is owned by one goroutine; all mutations (claim, release, mark-running,
record-retry) flow through it. Reads are served by snapshot copies.

- **Why over a `sync.Mutex` around a shared struct:** the poll loop, per-run worker
  goroutines, and reconciliation all mutate overlapping state; a single owner makes the
  serialization invariant (SPEC §4.1.11 "single authoritative state") structural rather
  than convention, and keeps audit ordering deterministic. A mutex is the fallback if the
  channel indirection proves heavy — both satisfy the spec. Decision: start with a mutex
  guarding a `state` struct (simplest, matches existing packages' style) and expose only
  copy-returning accessors so the choice is encapsulated and swappable.

### Injected clock + manual tick for determinism

The orchestrator takes a `clock func() time.Time` and a tick scheduler that, in tests, is
driven manually (`runTick(ctx)`), and in production is driven by a `time.Ticker` whose
period is re-read from config each tick (honoring hot reload).

- **Why:** retry/backoff and stall detection are time-dependent; tests must assert exact
  delays (1000 ms continuation, `10000 * 2^(attempt-1)` capped). Matches the Phase 2
  watcher and Phase 5 manager test style.

### Per-run worker goroutine bounded by a semaphore

Each dispatch spawns a worker goroutine that owns the workspace + provider turn, bounded
by `max_concurrent_agents` (and per-state caps checked at selection time). The worker
reports terminal outcome back to the state owner.

- **Why over a worker pool:** SPEC §14.3 specifies "goroutine + subprocess" with
  goroutines bounded by `max_concurrent_agents`. A semaphore (buffered channel of size
  `max_concurrent_agents`) is the minimal faithful implementation. Per-state caps are
  enforced in candidate selection (where the running counts are known) rather than as
  separate semaphores, since caps are dynamic via hot reload.

### Poll-loop seams as typed no-op hooks

Steps 1 (enforcer pre-dispatch), 3 (preflight), 4 (doc-store sync), and 6
(classification) are represented as small interface fields defaulting to no-op
implementations. Phase 6 wires only reconcile (2), fetch (5), sort (7), dispatch (8),
notify (9).

- **Why over deleting the steps:** keeps the SPEC §13.2 ordering literal and makes future
  phases a wiring change, not a control-flow rewrite. The §13.2 rule "if preflight fails,
  skip 5–8 but run 1–2 and 9" is implemented now against the no-op preflight (always
  passes), so the branch exists and is testable.

### Single hardcoded `coder` pipeline, not the router

Dispatch sets `pipeline = [coder]`, renders the `coder` template via `harness`, and runs
one provider turn. No classification call, no routing evaluation.

- **Why:** Phase 6's stated goal. The `RunAttempt.pipeline`/`pipeline_index` fields exist
  so Phase 7 swaps the hardcoded list for router output without touching the lifecycle.

### Reconciliation Part C is an explicit no-op seam

Memory post-processing (session-end episodic memory) is a named extension point invoked
on every terminal run, defaulting to no-op until Phase 9.

- **Why:** the call site is the hard part to retrofit; defining it now means Phase 9 only
  supplies an implementation.

## Risks / Trade-offs

- **[Goroutine leaks / stuck `running` entries on error paths]** → Every dispatch failure
  path (workspace create, render, turn start) must release the claim and decrement
  running; covered by explicit "failure releases claim" scenarios in the spec and unit
  tests asserting no stuck `running` entry after each failure mode.
- **[Subprocess isolation only — no container sandbox]** → Accepted per phase plan;
  `workspace.Manager.AgentCommand` already enforces the §14.2 invariants
  (cwd == workspace_path, path within root). Container isolation is Phase 17.
- **[Time-based flakiness in tests]** → Mitigated by the injected clock and manual tick
  driver; no real `time.Sleep` in tests. Production ticker is the only real-time path and
  is covered by a thin integration smoke test.
- **[Hot-reload races on config read]** → The interval and concurrency caps are read fresh
  each tick from a config accessor; reads are snapshot copies so a mid-tick reload cannot
  tear a value. The reload itself is owned by the Phase 2 watcher; Phase 6 only consumes
  the latest snapshot.
- **[Tracker pagination / partial fetch]** → `FetchCandidateIssues` already caps and logs
  internally; the orchestrator treats a capped page as a normal (incomplete) candidate
  set and picks them up on the next tick. No correctness dependency on a full fetch.
- **[Provider turn cancellation semantics]** → Stall detection and reconciliation cancel
  via context; the worker must propagate cancellation to the provider turn and the
  subprocess. Mapped to `stall_timeout` / `cancelled_by_reconciliation` sentinels.

## Migration Plan

Additive only. New implementation lands in `internal/orchestrator`; `conductor start` is
rewired from placeholder to real orchestrator construction. No DB schema change (audit
events reuse the Phase 1 `audit_events` table; any missing orchestrator event-type
constants are added to `internal/audit`). No config schema change. Rollback is reverting
the package + the `start.go` wiring; `--dry-run` remains a safe no-dispatch boot check
throughout.

## Open Questions

- **State ownership primitive:** start with a mutex-guarded struct (chosen above) and
  revisit a command-channel owner only if contention or audit-ordering tests demand it.
- **Where per-state concurrency is enforced:** selection-time check (chosen) vs. per-state
  semaphores — selection-time is simpler under hot-reloadable caps; confirm during
  implementation that it composes cleanly with the global semaphore.
- **Run-attempt persistence:** Phase 6 keeps run attempts in memory + audit trail; whether
  a dedicated `run_attempts` table is needed before Phase 14 is deferred (the audit log is
  sufficient to reconstruct lifecycle for now).
