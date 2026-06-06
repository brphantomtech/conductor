## ADDED Requirements

### Requirement: Orchestrator Runtime State

The orchestrator SHALL own a single authoritative in-memory runtime state (SPEC §4.1.11)
that is the only source of truth for which issues are claimed, running, or queued for
retry. All mutations to this state MUST be serialized so that no two goroutines observe
or modify it concurrently without synchronization.

The state SHALL track at minimum: the `running` map (issue ID → active run attempt), the
`claimed` set, the `retry_attempts` map (issue ID → attempt count and next-eligible
time), the global running count, and the status enums `enforcer_status`
(`clear`|`violations_present`|`blocked`), `knowledge_index_status`
(`ready`|`indexing`|`stale`|`disabled`), `pending_gc_tasks`, and `doc_store_sync_status`
(per-store map). Fields owned by engines not yet built (enforcer, knowledge, doc store)
SHALL hold their inert default (`clear`, `disabled`, `0`, empty) until those phases
populate them.

#### Scenario: State mutations are serialized

- **WHEN** multiple dispatch and reconciliation operations run concurrently
- **THEN** the runtime state remains internally consistent (no lost claims, no duplicate
  `running` entries) and a runtime accessor returns a coherent snapshot

#### Scenario: Engine status fields default inert

- **WHEN** the orchestrator starts with no enforcer, knowledge, or doc-store engine wired
- **THEN** `enforcer_status` is `clear`, `knowledge_index_status` is `disabled`,
  `pending_gc_tasks` is `0`, and `doc_store_sync_status` is empty

### Requirement: Run Attempt Lifecycle

The orchestrator SHALL model each execution of an issue as a Run Attempt (SPEC §4.1.12)
carrying at minimum a unique attempt ID, the issue reference, the `pipeline` (list of
agent roles), `pipeline_index`, attempt number, start time, terminal outcome, and a
`validation_results` slot. A `RunAttemptStarted` audit event SHALL be written when an
attempt begins and a `RunAttemptEnded` event when it terminates, regardless of outcome.

For Phase 6 the `pipeline` SHALL be the single hardcoded `[coder]` and `pipeline_index`
SHALL remain `0`.

#### Scenario: Attempt records start and end

- **WHEN** an issue is dispatched and its turn completes
- **THEN** the run attempt has a non-empty ID, `pipeline == [coder]`, and exactly one
  `RunAttemptStarted` and one `RunAttemptEnded` audit event are written with the end event
  recording the outcome

#### Scenario: Failed attempt still ends cleanly

- **WHEN** a dispatched turn fails
- **THEN** a `RunAttemptEnded` event is still written with a failure outcome and the
  run-attempt error sentinel matching the failure cause

### Requirement: Poll Loop

The orchestrator SHALL run a poll loop that ticks on the `polling.interval_ms` cadence.
Each tick SHALL, in order: reconcile active runs, fetch candidate issues from the
tracker, sort candidates by dispatch priority, dispatch eligible issues until concurrency
slots are exhausted, and notify observability consumers. Poll-loop steps for engines not
yet built (Harness Enforcer pre-dispatch check, doc-store sync, issue classification)
SHALL be present as no-op extension points so the ordering in SPEC §13.2 is preserved.

The interval SHALL be read fresh each tick so that a hot-reloaded `polling.interval_ms`
takes effect on the next tick without restart. The loop SHALL stop promptly when its
context is cancelled.

#### Scenario: Tick dispatches eligible candidates

- **WHEN** a tick runs and the tracker returns eligible candidates with free concurrency
  slots
- **THEN** the orchestrator dispatches candidates in sorted order until no slot or no
  eligible candidate remains

#### Scenario: Interval change takes effect without restart

- **WHEN** `polling.interval_ms` is changed via hot reload
- **THEN** the next tick is scheduled using the new interval and no restart is required

#### Scenario: Context cancellation stops the loop

- **WHEN** the orchestrator's context is cancelled
- **THEN** the poll loop returns promptly and dispatches no further work

### Requirement: Candidate Selection

The orchestrator SHALL treat an issue as dispatch-eligible if and only if all of the
following hold (SPEC §13.3): `id`, `identifier`, `title`, and `state` are all present;
the state is in `active_states` and not in `terminal_states`; the issue is not in the
`running` map; the issue is not in the `claimed` set; a global concurrency slot is
available (`max_concurrent_agents - len(running) > 0`); a per-state concurrency slot is
available; and, when the state is `Todo`, every `blocked_by` entry resolves to a terminal
state. State comparisons SHALL be case-insensitive.

#### Scenario: Missing required field is ineligible

- **WHEN** a candidate issue is missing its `title` (or `id`, `identifier`, or `state`)
- **THEN** it is excluded from dispatch

#### Scenario: Todo blocker rule

- **WHEN** a `Todo` issue has a `blocked_by` entry that is not in a terminal state
- **THEN** the issue is not dispatched; once all blockers are terminal it becomes eligible

#### Scenario: Concurrency caps respected

- **WHEN** the global running count equals `max_concurrent_agents`, or the per-state cap
  for the candidate's state is reached
- **THEN** no further candidate is dispatched for that constraint until a slot frees

### Requirement: Dispatch Sort Order

The orchestrator SHALL sort dispatch-eligible candidates by `priority` ascending with
null priority sorting last, then by `created_at` oldest first, then by `identifier`
lexicographically as a final tiebreaker (SPEC §13.3).

#### Scenario: Priority then age then identifier

- **WHEN** candidates have mixed priorities, creation times, and identifiers
- **THEN** they are dispatched in ascending priority (nulls last), oldest-first within
  equal priority, and lexicographic identifier order within equal priority and time

### Requirement: Retry and Backoff

The orchestrator SHALL schedule retries with a fixed 1000 ms delay after a clean worker
exit (continuation retry) and an exponential backoff of
`min(10000 * 2^(attempt-1), max_retry_backoff_ms)` after a failure-driven retry
(SPEC §13.4). A `RetryScheduled` audit event SHALL be written when a retry timer is
queued. While a retry timer is active the issue SHALL be in the `RetryQueued` state and
not re-dispatched before the timer elapses.

#### Scenario: Continuation retry uses fixed delay

- **WHEN** a worker exits cleanly and a continuation retry is scheduled
- **THEN** the next attempt becomes eligible after 1000 ms

#### Scenario: Failure backoff grows exponentially and is capped

- **WHEN** failure-driven retries occur on attempts 1, 2, 3, …
- **THEN** the delays follow `10000 * 2^(attempt-1)` and never exceed
  `max_retry_backoff_ms`

### Requirement: Reconciliation

On each tick the orchestrator SHALL reconcile active runs (SPEC §13.5). Part A (stall
detection) SHALL terminate any worker that has made no progress within the configured
stall window, writing a `SessionStalled` audit event and applying retry/backoff. Part B
(tracker state refresh) SHALL refresh the tracker state of running issues and cancel any
run whose issue has moved out of an active state, classifying the cancellation as
`cancelled_by_reconciliation`. Part C (memory post-processing) SHALL be a no-op extension
point in Phase 6.

#### Scenario: Stalled worker is terminated and retried

- **WHEN** a running worker exceeds the stall window with no progress
- **THEN** it is terminated, a `SessionStalled` event is written, and the issue is
  re-queued under retry/backoff

#### Scenario: Issue moved out of active state is cancelled

- **WHEN** reconciliation observes a running issue whose tracker state is no longer active
- **THEN** the run is cancelled with reason `cancelled_by_reconciliation` and the claim is
  released

### Requirement: Startup Terminal Cleanup

On startup the orchestrator SHALL query the tracker for issues in terminal states, remove
the workspace directory for each, and clear any stale runtime state (SPEC §13.6). A
failure to clean any single workspace SHALL be logged as a warning and SHALL NOT abort
startup.

#### Scenario: Terminal workspaces removed on boot

- **WHEN** the orchestrator starts and the tracker reports issues in terminal states with
  existing workspaces
- **THEN** those workspaces are removed and their runtime state is cleared

#### Scenario: Cleanup failure does not block startup

- **WHEN** removing one terminal workspace fails
- **THEN** a warning is logged and startup proceeds

### Requirement: Single Coder Dispatch Path

Dispatch SHALL execute a single hardcoded `coder` role for Phase 6 (no routing,
classification, or multi-role pipelines). Dispatching an issue SHALL: claim the issue,
write an `IssueDispatched` audit event, create or reuse the issue workspace via
`internal/workspace`, render the `coder` prompt template via `internal/harness`, start a
provider turn via `internal/provider`, record the run-attempt lifecycle, and on terminal
state release the claim with an `IssueReleased` audit event.

#### Scenario: End-to-end single dispatch

- **WHEN** an eligible issue is dispatched
- **THEN** a workspace is created, the `coder` prompt is rendered, a provider turn is
  started, and on completion the claim is released with `IssueDispatched` and
  `IssueReleased` audit events recorded

#### Scenario: Workspace creation failure is classified

- **WHEN** workspace creation fails during dispatch
- **THEN** the attempt ends with the `workspace_creation_failed` sentinel and the claim is
  released without a stuck `running` entry

### Requirement: Run Attempt Error Classification

The orchestrator SHALL classify run-attempt failures using the SPEC §23.4 sentinel
identifiers: `workspace_creation_failed`, `hook_failed`, `prompt_render_failed`,
`turn_timeout`, `turn_failed`, `turn_cancelled`, `stall_timeout`,
`validation_pipeline_failed`, and `cancelled_by_reconciliation`. The failure reason
recorded on `RunAttemptEnded` and used to drive retry/backoff SHALL match the originating
cause.

#### Scenario: Failure maps to the correct sentinel

- **WHEN** a turn exceeds its deadline
- **THEN** the attempt fails with `turn_timeout`; a hook failure yields `hook_failed`; a
  render failure yields `prompt_render_failed`; each mapping to its SPEC §23.4 identifier

### Requirement: Orchestrator Wiring in `conductor start`

The `conductor start` command SHALL construct the orchestrator from loaded config and
collaborators and run its poll loop, replacing the prior placeholder. With `--dry-run`,
`conductor start` SHALL complete the config/boot path and exit without dispatching any
issue.

#### Scenario: Start runs the orchestrator

- **WHEN** `conductor start` is invoked with valid config and a reachable tracker
- **THEN** the orchestrator poll loop runs until the process is signalled to stop

#### Scenario: Dry run does not dispatch

- **WHEN** `conductor start --dry-run` is invoked
- **THEN** config loads, the orchestrator is not started into its dispatch loop, and the
  process exits cleanly without creating a workspace
