## MODIFIED Requirements

### Requirement: Run Attempt Lifecycle

The orchestrator SHALL model each execution of an issue as a Run Attempt (SPEC §4.1.12)
carrying at minimum a unique attempt ID, the issue reference, the `pipeline` (list of
agent roles), `pipeline_index`, attempt number, start time, terminal outcome, and a
`validation_results` slot. A `RunAttemptStarted` audit event SHALL be written when an
attempt begins and a `RunAttemptEnded` event when it terminates, regardless of outcome.

The `pipeline` SHALL be the pipeline selected by the Agent Router for the issue, and
`pipeline_index` SHALL advance as each role executes. When routing produces a single-role
pipeline, the attempt behaves as the earlier single-`coder` path did.

#### Scenario: Attempt records start and end

- **WHEN** an issue is dispatched and its pipeline completes
- **THEN** the run attempt has a non-empty ID, a `pipeline` equal to the router-selected roles, and
  exactly one `RunAttemptStarted` and one `RunAttemptEnded` audit event are written with the end
  event recording the outcome

#### Scenario: Failed attempt still ends cleanly

- **WHEN** a dispatched turn fails
- **THEN** a `RunAttemptEnded` event is still written with a failure outcome and the
  run-attempt error sentinel matching the failure cause

### Requirement: Single Coder Dispatch Path

Dispatch SHALL execute the pipeline selected by the Agent Router (replacing the Phase 6 hardcoded
single `coder` role). Dispatching an issue SHALL: claim the issue, write an `IssueDispatched` audit
event, create or reuse the issue workspace via `internal/workspace`, classify the issue when needed
and select its pipeline via the router, render each role's prompt template via `internal/harness`,
execute each role's turn via `internal/provider` in pipeline order, record the run-attempt lifecycle,
and on terminal state release the claim with an `IssueReleased` audit event. Issue classification
SHALL be wired through the orchestrator's existing classification seam.

#### Scenario: End-to-end router-driven dispatch

- **WHEN** an eligible issue is dispatched
- **THEN** a workspace is created, the issue's pipeline is selected by the router, each role's prompt
  is rendered and its turn executed in order, and on completion the claim is released with
  `IssueDispatched` and `IssueReleased` audit events recorded

#### Scenario: Single-role pipeline matches prior behavior

- **WHEN** routing selects a single-role `[coder]` pipeline
- **THEN** dispatch renders and runs exactly one `coder` turn, equivalent to the Phase 6 dispatch
  path

#### Scenario: Workspace creation failure is classified

- **WHEN** workspace creation fails during dispatch
- **THEN** the attempt ends with the `workspace_creation_failed` sentinel and the claim is
  released without a stuck `running` entry
