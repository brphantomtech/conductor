# Phase 5 — Workspace Management

## Why

Conductor's orchestrator (Phase 6) needs a safe, isolated place to run each agent
pipeline. Today nothing creates per-issue working directories, executes lifecycle
hooks, or enforces the filesystem safety invariants the SPEC requires before any agent
process is spawned. Phases 1–4 delivered configuration, persistence, the HARNESS.md
loader, and the provider/tracker adapters; Phase 5 supplies the missing runtime
substrate so later phases can dispatch work into real, disposable workspaces.

Authoritative scope: [docs/phases.md](../../../docs/phases.md) → "Phase 5 — Workspace
Management"; SPEC §14 (workspace management), §5.3.4 (workspace config), §5.3.5 (hooks),
§23.4 (run-attempt errors related to workspace creation/hooks).

## What Changes

- **New `internal/workspace` package (Tier 1)** that owns the full workspace lifecycle:
  - Workspace **key sanitization** and deterministic **layout creation** rooted at the
    configured workspace root.
  - The **`.conductor/` skeleton** (per-workspace `audit.jsonl` sink location,
    `validation/` directory, workspace metadata) per SPEC §14.1.
  - **Multi-repo support** (`workspace.repos`) — multiple repositories checked out into
    one workspace.
  - **Lifecycle hook execution** for `after_create`, `before_run`, `after_run`,
    `after_turn`, `before_remove`, and `on_harness_violation`, each with **timeout
    enforcement** and **audit logging** (SPEC §5.3.5).
  - The **four §14.2 safety invariants**, enforced before every create / remove / hook
    operation.
  - **Subprocess-based agent isolation** (Docker/container isolation remains a later
    opt-in — Phase 17).
- **Error classification**: workspace / run-attempt error sentinels per SPEC §23.4 added
  to the package's `errors.go`, matching the spec identifiers exactly (consistent with the
  Phase 1 sentinel convention).
- **Audit integration**: workspace lifecycle and hook-execution events written through
  `internal/audit` (event types per SPEC §17.2).
- **Unit tests** covering key sanitization, path-escape rejection, layout creation,
  multi-repo checkout, and hook timeout + audit behavior.

## Capabilities

### New Capabilities

- `workspace-management`: per-issue workspace creation and layout, the `.conductor/`
  skeleton, multi-repo checkout, lifecycle hook execution with timeouts + audit, the
  §14.2 filesystem safety invariants, subprocess agent isolation, and SPEC §23.4 error
  classification.

### Modified Capabilities

None. `harness-loader`, `provider-adapter-layer`, and `tracker-adapters` are untouched.

## Impact

- **Affected specs**: new capability `workspace-management` (delta in
  `specs/workspace-management/spec.md`).
- **Affected code**: `internal/workspace/` (new package; currently only a `doc.go`
  stub). `internal/audit` (add any missing workspace/hook event constants). Config
  already models `workspace` and `hooks` from Phase 1, so no config schema change is
  expected.
- **No changes** to `internal/provider`, `internal/tracker`, or `internal/harness`.
  The orchestrator (Phase 6) will consume this package; this phase does not wire the
  run loop.

### Non-goals

- Container / Docker isolation and the cloud profile (Phase 17).
- Orchestrator-driven hook invocation timing and the poll/dispatch loop (Phase 6).
- `on_harness_violation` rule evaluation — Phase 12 supplies the violations; this phase
  only provides the hook entry point and executes it when invoked.
- Validation execution inside the `validation/` directory (Phase 8); Phase 5 only
  creates the directory skeleton.
