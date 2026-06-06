# Phase 6 — Atomic Tasks (Orchestrator Core)

Phase 6 goal (per [docs/phases.md](../../../docs/phases.md)): end-to-end poll → claim →
dispatch a single hardcoded `coder` run → retry on failure → reconcile → release. SPEC
§13, §4.1.11, §4.1.12, §23.4. Each task is sized for one focused session.

## 1. Runtime State & Run Attempt

- [x] 1.1 Define `OrchestratorRuntimeState` (SPEC §4.1.11): `running` map, `claimed` set, `retry_attempts` map, running count, and status enums (`enforcer_status`, `knowledge_index_status`, `pending_gc_tasks`, `doc_store_sync_status`) with inert defaults.
- [x] 1.2 Wrap the state in a mutex-guarded owner type exposing only copy-returning accessors plus serialized mutators (claim, release, mark-running, record-retry, snapshot).
- [x] 1.3 Define `RunAttempt` (SPEC §4.1.12): ID, issue ref, `pipeline`, `pipeline_index`, attempt number, start time, outcome, `validation_results` slot.
- [x] 1.4 Unit tests: concurrent claim/release/mark-running keep state consistent; snapshot is a coherent copy; defaults are inert.

## 2. Orchestrator Skeleton & Wiring

- [x] 2.1 Define the `Orchestrator` struct + `New(...Option)` constructor accepting `tracker.Adapter`, `workspace.Manager`, `provider.Adapter`, harness renderer, `audit.Writer`, config accessor, and injected `clock`.
- [x] 2.2 Add an `audit` `emit` helper and confirm all needed orchestrator event-type constants exist in `internal/audit` (`IssueDispatched`, `IssueReleased`, `IssueCancelled`, `RunAttemptStarted`, `RunAttemptEnded`, `RetryScheduled`, `SessionStalled`); add any missing.
- [x] 2.3 Define no-op seam interfaces for the not-yet-built poll-loop steps (enforcer pre-dispatch, preflight, doc-store sync, classification) with default no-op implementations.

## 3. Candidate Selection & Dispatch Sort

- [x] 3.1 Implement the dispatch-eligibility predicate (SPEC §13.3): field presence, active/non-terminal state (case-insensitive), not running, not claimed, global + per-state concurrency slot, and the `Todo` blocker rule against terminal `blocked_by`.
- [x] 3.2 Implement the dispatch sort: `priority` asc (null last) → `created_at` oldest → `identifier` lexicographic.
- [x] 3.3 Unit tests: each ineligibility cause; blocker rule before/after blockers terminal; global + per-state cap exhaustion; full sort ordering with mixed inputs.

## 4. Single Coder Dispatch Path

- [x] 4.1 Implement `dispatch(issue)`: claim, write `IssueDispatched`, create/reuse workspace via `workspace.Manager`, set `pipeline=[coder]`.
- [x] 4.2 Render the `coder` prompt template via `internal/harness`; map render failure → `ErrPromptRenderFailed`.
- [x] 4.3 Start a provider turn via `provider.Adapter` inside a worker goroutine bounded by a `max_concurrent_agents` semaphore; own the workspace subprocess via `AgentCommand`.
- [x] 4.4 Drive the run-attempt lifecycle: `RunAttemptStarted` on begin, `RunAttemptEnded` (with outcome + sentinel) on terminal, release claim + `IssueReleased` on terminal active-exit.
- [x] 4.5 Map each failure mode to the SPEC §23.4 sentinel (`workspace_creation_failed`, `hook_failed`, `prompt_render_failed`, `turn_timeout`, `turn_failed`, `turn_cancelled`); ensure every failure path releases the claim with no stuck `running` entry.
- [x] 4.6 Unit tests with fakes: happy-path end-to-end dispatch; each failure mode classified and claim released.

## 5. Retry & Backoff

- [x] 5.1 Implement continuation retry (fixed 1000 ms on clean worker exit) and failure backoff `min(10000 * 2^(attempt-1), max_retry_backoff_ms)`; write `RetryScheduled`.
- [x] 5.2 Hold retry-queued issues in `RetryQueued` state until the timer elapses; prevent early re-dispatch.
- [x] 5.3 Unit tests (injected clock): continuation delay; exponential growth; cap enforcement; no re-dispatch before timer.

## 6. Poll Loop

- [x] 6.1 Implement `runTick(ctx)` ordering per SPEC §13.2 (seams 1/3/4/6 no-op, real 2/5/7/8/9), including the "preflight fails → skip 5–8, run 1–2 + 9" branch.
- [x] 6.2 Implement the production loop driver: `time.Ticker` re-reading `polling.interval_ms` each tick (hot-reload aware); stop promptly on context cancellation.
- [x] 6.3 Unit tests: manual `runTick` dispatches sorted eligible candidates; interval change takes effect next tick; context cancellation stops the loop.

## 7. Reconciliation

- [x] 7.1 Part A — stall detection: terminate no-progress workers past the stall window, write `SessionStalled`, classify `stall_timeout`, apply retry/backoff.
- [x] 7.2 Part B — tracker state refresh via `FetchIssueStatesByIDs`/`FetchIssuesByStates`; cancel runs whose issue left active state with reason `cancelled_by_reconciliation`; release claim.
- [x] 7.3 Part C — define the memory post-processing no-op seam invoked on every terminal run (Phase 9 fills it).
- [x] 7.4 Unit tests: stalled worker terminated + retried; issue moved out of active state cancelled + released.

## 8. Startup Terminal Cleanup

- [x] 8.1 Implement startup cleanup (SPEC §13.6): query terminal-state issues, remove each workspace, clear stale runtime state; log+continue on per-workspace failure.
- [x] 8.2 Unit tests: terminal workspaces removed and state cleared; single-workspace failure does not abort startup.

## 9. CLI Wiring

- [x] 9.1 Rewire `cmd/conductor/cmd/start.go` to construct and run the orchestrator from loaded config + collaborators (replace the placeholder); wire graceful shutdown on signal.
- [x] 9.2 Honor `--dry-run`: complete config/boot path and exit without dispatching or creating a workspace.
- [x] 9.3 Update `AGENTS.md` navigation note for `internal/orchestrator` and refresh any phase pointers.

## 10. Verification

- [x] 10.1 `go build ./...` and `go vet ./...` clean; `golangci-lint` (or repo linter) passes for `internal/orchestrator`.
- [x] 10.2 `go test ./internal/orchestrator/...` passes; package coverage ≥ 70% (matching prior phases' target).
- [x] 10.3 Smoke: `conductor start --dry-run` exits cleanly; a fake-tracker integration test runs one full tick dispatching a single coder turn.
