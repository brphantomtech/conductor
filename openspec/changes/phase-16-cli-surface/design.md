## Context

Phase 16 completes the operator CLI from SPEC §19.1. It depends only on substrate on `main`: the
workspace manager (Phase 5 — `Resolve`/`Remove`/layout), `config` (load + defaults), `db`/`audit`
(reachability checks), and the tracker adapter. The constraint that shapes this phase is that the
**live service control channel** — the HTTP API and WebSocket of SPEC §18 — is **Phase 14**, which
is not built. Commands that must talk to a running orchestrator (`stop`, live `status`, live
`dispatch`/`cancel`) therefore cannot be fully realized here.

Rather than fake them, this phase ships every command's surface and implements the offline-capable
behavior, returning a clear, structured "requires the running service (Phase 14)" result for the
parts that need the control channel. This keeps the CLI honest and lets the offline-valuable
commands (`workspace *`, `init`, static `status`) ship now.

Conventions to match (Phases 2–10): Cobra commands constructed by `newXCommand()` in their own
files, text/JSON output via a `--format` flag where it fits, config loaded via the existing loader,
no global state.

This is a Wave B worktree; each command is its own `cmd_*.go` file, so the only shared edit is the
`root.AddCommand` registrations in the final commit.

## Goals / Non-Goals

**Goals:**

- `workspace list/remove/open` operating on `workspace.root` via the workspace manager.
- `init --profile local|team|cloud` scaffolding a starter HARNESS.md (+ compose/deploy stub) with a
  no-overwrite guard.
- `dispatch`/`cancel`/`status` command surfaces with offline-capable behavior and an explicit
  "requires Phase 14" path for live operations.

**Non-Goals:**

- Live orchestrator control / live runtime snapshot / `stop` — Phase 14. Dashboard `status` — Phase
  15.

## Decisions

### `workspace` commands go through the workspace manager, not raw FS

`list` enumerates `workspace.root` and reports each workspace's key/issue/path/repos; `remove` calls
the manager's `Remove` (so `before_remove` hooks and the §14.2 safety invariants run); `open` resolves
the path and launches `$EDITOR`.

- **Why:** SPEC §14.2 invariants and hooks must hold for destructive operations; reusing the manager
  keeps that centralized instead of re-implementing path safety in the CLI. `list` is read-only over
  the same layout the manager creates.

### `init` scaffolds from profile templates with a no-overwrite guard

`--profile local|team|cloud` selects a template set: `local` writes a minimal HARNESS.md; `team`/
`cloud` additionally write a `docker-compose.yml` / deployment stub (SPEC §3.2 profiles). Existing
files are never overwritten without `--force`.

- **Why:** SPEC §19.1 + §3.2. A no-overwrite guard prevents clobbering an operator's work; templates
  encode the required fields (so the scaffolded HARNESS.md passes `config.Validate` once secrets are
  filled in).

### Live operations degrade explicitly, not silently

`dispatch`/`cancel`/`stop`/live-`status` detect the absence of a control channel and return a
structured, documented "requires the running service (Phase 14)" error/notice. Where an action is
feasible offline (e.g. a `status` config+workspace+DB snapshot), it is performed.

- **Why:** the SPEC §19.1 surface should exist now for discoverability and scripting, but honesty
  (the project's reporting principle) means not pretending to dispatch when nothing is listening. When
  Phase 14 lands, these commands gain their live path without changing their surface.

### `status` prints the statically derivable snapshot

Config summary (project, tracker kind, providers, polling), workspace inventory, and DB/audit
reachability — the parts knowable without the in-memory orchestrator state.

- **Why:** gives operators a useful pre-flight/health view now; the live runtime snapshot
  (`OrchestratorRuntimeState`) is exposed by Phase 14's API and grafts on later.

## Risks / Trade-offs

- **[Partial commands could confuse operators]** → Every command's help text and the
  "requires Phase 14" notice state exactly what works now vs. what needs the running service; `status`
  labels which sections are static. No silent no-ops.
- **[`workspace remove` is destructive]** → Routed through the manager so `before_remove` hooks and
  the §14.2 invariants run; a confirmation flag (`--yes`) guards non-interactive deletion; covered by
  tests over a temp root.
- **[`init` clobbering files]** → No-overwrite unless `--force`; tested.
- **[`$EDITOR` not set for `open`]** → Falls back to printing the resolved path with a clear message
  instead of failing.
- **[Profile templates drifting from config schema]** → Templates are built from the same
  `config` types/defaults so a scaffolded file validates; a test runs `config.Validate` on each
  profile's scaffold (with placeholder secrets) to catch drift.

## Migration Plan

Additive only. New `cmd_*.go` files; registrations appended to `root.go` in the final commit per the
Wave B integration-commit rule. No package, DB, or config schema changes. Rollback is reverting the
new files + the registrations. Nothing changes for existing commands or `start`.

## Open Questions

- **How much of `dispatch`/`cancel` is feasible offline** — e.g. `dispatch` could enqueue a hint the
  next `start` poll honors, or it could be purely a Phase-14 live op. Default to the explicit
  "requires running service" path and revisit if a safe offline enqueue is justified.
- **`init` cloud profile contents** — how complete the Helm/Dockerfile stub should be vs. a pointer;
  start with a minimal compose + Dockerfile and a README note, expand under Phase 17/Profile C.
- **`status` output format** — text default with `--format json`; confirm the JSON shape aligns with
  what Phase 14's `status` endpoint will later return so scripts stay stable.
