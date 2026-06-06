# Phase 16 — CLI Surface Completion

## Why

Conductor's CLI today covers `version`, `harness validate`, `start`, and the per-engine commands
(`memory`, `knowledge`, `validation`, and — once Wave B lands — `docs`, `harness check`). The
operator-facing surface from SPEC §19.1 is still incomplete: there is no way to inspect or manage
workspaces from the CLI, force-dispatch or cancel an issue, or scaffold a starter project. Phase 16
fills the gaps that are reachable without a running-service control channel, so a fresh user can go
from empty directory to a valid configuration and can manage the on-disk workspace state.

This is a **Wave B** change (see [docs/parallel-execution-plan.md](../../../docs/parallel-execution-plan.md)):
it depends only on the workspace manager and config (Phases 1/5, on `main`) and lands its commands
in their own `cmd_*.go` files, buildable concurrently with Phases 11, 12, and 17.

Authoritative scope: [docs/phases.md](../../../docs/phases.md) → "Phase 16 — CLI Surface
Completion"; SPEC §19 (CLI).

## What Changes

- **`conductor workspace list / remove / open`** (SPEC §19.1): list workspaces under
  `workspace.root` (key, issue, path, repos), remove a workspace by key via the workspace manager
  (running `before_remove` hooks + safety invariants), and open a workspace in `$EDITOR`.
- **`conductor init --profile local|team|cloud`** (SPEC §19.1, §3.2): scaffold a starter
  `HARNESS.md` (with the right front matter for the chosen profile) and, for `team`/`cloud`, a
  `docker-compose.yml` / deployment stub, into the current directory; refuse to overwrite existing
  files without `--force`.
- **`conductor dispatch <id>` / `conductor cancel <id>`** (SPEC §19.1): force-dispatch and cancel by
  issue identifier. Because the live control channel (HTTP API / IPC) is Phase 14, this phase
  implements the **offline-capable** form — operating against the persisted state/tracker where it
  can — and clearly reports when a live service is required, returning a structured "not yet
  supported without the API (Phase 14)" message rather than silently no-oping. (See Non-goals.)
- **`conductor status`** (SPEC §19.1): print a runtime snapshot. Without the Phase 14 surface this
  phase prints the **statically derivable** snapshot (config summary, workspace inventory, DB/audit
  reachability); the live in-memory orchestrator runtime state requires Phase 14 and is a non-goal
  here.
- Each command lives in its own `cmd_*.go` file so the only shared edit is the `root.AddCommand`
  registrations.
- **Unit tests**: workspace list/remove against a temp `workspace.root`; `init` scaffolding per
  profile (and the no-overwrite guard); `status` static snapshot; `dispatch`/`cancel` argument
  parsing and the "requires running service" path.

## Capabilities

### New Capabilities

- `cli-surface`: the `workspace list/remove/open` commands, `init --profile`, the offline-capable
  `dispatch`/`cancel` and static `status` commands, with their argument/flag surfaces matching SPEC
  §19.1.

### Modified Capabilities

None. The workspace manager, config, and existing command tree are consumed unchanged; new commands
are additive.

## Impact

- **Affected specs**: new capability `cli-surface`.
- **Affected code**: new `cmd/conductor/cmd/cmd_workspace.go`, `cmd_init.go`, `cmd_dispatch.go`,
  `cmd_cancel.go`, `cmd_status.go` (one file per command group).
- **Integration points (last commit only, append-only)** per the Wave B integration-commit rule:
  - `cmd/conductor/cmd/root.go` — `root.AddCommand(...)` for each new command.
  - `AGENTS.md` — navigation note for the completed CLI surface.
- **Consumes (unchanged)**: `internal/workspace` (list/remove/resolve), `internal/config` (load +
  defaults + scaffolding templates), `internal/db`/`internal/audit` (status reachability),
  `internal/tracker` (dispatch/cancel where offline-feasible).

### Non-goals

- **Live orchestrator control** for `dispatch`/`cancel`/`stop` and the **live runtime snapshot** for
  `status` — these require the HTTP API / IPC control channel delivered in Phase 14. This phase ships
  the command surface and the offline-capable behavior, and explicitly reports when the running
  service is required rather than pretending to act.
- `conductor stop` against a running service — same Phase 14 dependency; deferred.
- The web dashboard equivalent of `status` (Phase 15).
