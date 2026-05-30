# Workspace Management — Spec Delta

## ADDED Requirements

### Requirement: Workspace Creation And Layout

The system SHALL create an isolated working directory for each issue, rooted at the
configured workspace root, using a sanitized workspace key, and SHALL initialize the
`.conductor/` skeleton within it (SPEC §14.1, §5.3.4).

#### Scenario: Create a workspace for an issue

- **WHEN** the workspace manager is asked to create a workspace for an issue key
- **THEN** a directory is created at `<workspace_root>/<sanitized_key>/`
- **AND** a `.conductor/` skeleton is initialized (`audit.jsonl` sink, `validation/`
  directory, workspace metadata)
- **AND** a workspace-created audit event is written

#### Scenario: Reject a key that escapes the workspace root

- **WHEN** an issue key contains path separators, traversal sequences (`..`), or an
  absolute path component
- **THEN** the key is sanitized to a safe slug
- **AND** the resolved workspace path is verified to remain inside the configured root
- **AND** creation fails with a workspace error if confinement cannot be guaranteed

### Requirement: Multi-Repo Checkout

The system SHALL materialize every repository listed in `workspace.repos` into the
workspace under a stable, distinct subdirectory (SPEC §5.3.4).

#### Scenario: Workspace with multiple repositories

- **WHEN** `workspace.repos` declares more than one repository
- **THEN** each repository is checked out into its own subdirectory within the workspace
- **AND** the workspace layout records each repository location

### Requirement: Lifecycle Hook Execution

The system SHALL execute the configured lifecycle hooks — `after_create`, `before_run`,
`after_run`, `after_turn`, `before_remove`, and `on_harness_violation` — with the
workspace as the working directory, per-hook timeout enforcement, and audit logging
(SPEC §5.3.5).

#### Scenario: Hook completes within its timeout

- **WHEN** a configured hook is invoked and exits successfully before its timeout
- **THEN** its stdout/stderr are captured
- **AND** a hook-execution audit event recording success is written

#### Scenario: Hook exceeds its timeout

- **WHEN** a configured hook runs longer than its configured timeout
- **THEN** the hook process (and its process group) is terminated
- **AND** the failure is classified using the SPEC §23.4 error identifiers
- **AND** a hook-execution audit event recording the timeout is written

#### Scenario: Hook exits non-zero

- **WHEN** a configured hook exits with a non-zero status
- **THEN** the failure is classified using the SPEC §23.4 error identifiers
- **AND** a hook-execution audit event recording the failure is written

### Requirement: Filesystem Safety Invariants

The system SHALL enforce the four workspace safety invariants of SPEC §14.2 before any
create, remove, or hook operation, refusing any operation that would act outside the
configured workspace root.

#### Scenario: Removal is confined to the workspace root

- **WHEN** a workspace removal is requested
- **THEN** the target path is verified to resolve inside the configured workspace root
- **AND** removal is refused with a workspace error if it resolves outside the root

#### Scenario: Hooks run only within an established workspace

- **WHEN** a lifecycle hook is invoked
- **THEN** its working directory is the established workspace directory
- **AND** the invariant guard rejects invocation if no valid workspace is present

### Requirement: Subprocess Agent Isolation

The system SHALL run agent processes as subprocesses scoped to the workspace directory.
Container-based isolation is out of scope for this phase (SPEC §14.3; deferred to
Phase 17).

#### Scenario: Agent runs as a workspace-scoped subprocess

- **WHEN** an agent process is started for a workspace
- **THEN** it is launched as a subprocess with the workspace directory as its working
  directory

### Requirement: Workspace Error Classification

The system SHALL classify workspace and run-attempt failures using the SPEC §23.4 error
identifiers, exposed as Go sentinel errors whose string values match the spec
identifiers exactly.

#### Scenario: Workspace creation failure is classified

- **WHEN** workspace creation or a lifecycle hook fails
- **THEN** the returned error wraps the corresponding SPEC §23.4 sentinel
