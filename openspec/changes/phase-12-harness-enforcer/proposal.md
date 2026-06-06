# Phase 12 — Harness Enforcer

## Why

Harness Engineering's central claim is that architectural governance must be operational, not a
convention people remember. The Harness Enforcer is that operational layer: it runs the project's
`harness_rules` before every dispatch (injecting debt/architectural findings into the agent's
prompt, optionally halting dispatch on blocking violations), on a cron schedule (creating
deduplicated GC tracker issues for persistent violations), and on demand. It also translates the
Knowledge Engine's layer-violation output (Phase 10) into rule violations, closing the loop on
dependency-direction governance.

This is a **Wave B** change (see [docs/parallel-execution-plan.md](../../../docs/parallel-execution-plan.md)):
it depends only on the Knowledge Engine and the orchestrator's `EnforcerCheck` seam (both on
`main`) and lands its logic in an extended `internal/harness`, buildable concurrently with Phases
11, 16, and 17.

Authoritative scope: [docs/phases.md](../../../docs/phases.md) → "Phase 12 — Harness Enforcer";
SPEC §11 (Harness Enforcer), §4.1.8 (HarnessRule), §5.3.11–§5.3.12 (rules + enforcement), §8.6
(layer violations).

## What Changes

- **`internal/harness` extended** with a `HarnessRule` runner (SPEC §4.1.8): execute each rule's
  `check` shell command in the workspace root, classify by `severity` (`warning`, `error`,
  `blocking`), and produce violation records with `fix_hint`.
- **Pre-dispatch check** (SPEC §11.2): when `enforcement.drift_check_on_dispatch`, run rules and
  collect by severity — `warning` → `## Known Technical Debt`, `error` → `## Architectural Issues
  You Must Fix` (formatters for prompt injection); `blocking` → when
  `blocking_violations_halt_dispatch`, set `enforcer_status = blocked`, skip dispatch for the tick,
  and emit `HarnessEnforcerBlocked`. Supplied via the orchestrator's existing `EnforcerCheck` seam
  (`WithEnforcer`).
- **Scheduled GC** (SPEC §11.3): a robfig/cron job on `enforcement.gc_schedule_cron` that runs
  rules and, for each `auto_fix` violation, creates a tracker issue (title `[GC] <name>:
  <summary>`, description with `fix_hint` + affected files, label `enforcement.gc_issue_label`,
  state `enforcement.gc_issue_state`) — **deduplicated** against existing open GC issues for the
  same `rule_id` — emitting `GCTaskCreated`.
- **Layer violation translation** (SPEC §8.6 / §11.4): convert the Knowledge Engine's
  `CheckLayerViolations` output into `dependency`-category HarnessRule violations.
- **`HarnessViolationDetected` audit event** per detected violation (already declared in audit).
- **`conductor harness check` subcommand** (extends the existing `harness` command) for on-demand
  rule execution.
- **`pending_gc_tasks` runtime-state reporting** via the enforcer.
- **Unit tests** with fakes: rule runner classification, the three severity behaviors, blocking
  halt, GC issue creation + dedup (fake tracker), layer-violation translation, and the formatters.

## Capabilities

### New Capabilities

- `harness-enforcer`: the HarnessRule runner, the pre-dispatch check with severity-based prompt
  formatters and blocking-halt, scheduled GC with deduplicated tracker-issue creation, Knowledge
  Engine layer-violation translation, the `harness check` subcommand, the `EnforcerCheck` seam
  implementation, and the `HarnessViolationDetected`/`HarnessEnforcerBlocked`/`GCTaskCreated` audit
  events.

### Modified Capabilities

None. The Knowledge Engine, tracker, config, and the orchestrator's `EnforcerCheck` seam are
consumed unchanged. (The seam was built in Phase 6 precisely so the enforcer plugs in without a
poll-loop rewrite.)

## Impact

- **Affected specs**: new capability `harness-enforcer`.
- **Affected code**: `internal/harness/` (extended — new enforcer files alongside the Phase 2
  loader). New `cmd/conductor/cmd/cmd_harness_check.go` (or extend the existing `harness` command
  file).
- **Integration points (last commit only, append-only)** per the Wave B integration-commit rule:
  - `cmd/conductor/cmd/root.go` — register the `harness check` subcommand if a new top-level wiring
    is needed (the `harness` parent command already exists).
  - `cmd/conductor/cmd/start.go` `runOrchestrator` — construct the enforcer and wire
    `orchestrator.WithEnforcer(...)` plus the GC cron.
- **Consumes (unchanged)**: `internal/knowledge` (`CheckLayerViolations`), `internal/tracker` (GC
  issue creation + dedup query), `internal/config` (`HarnessRules`, `Enforcement`),
  `internal/workspace` (rule command execution in the workspace root), `internal/audit`,
  `internal/orchestrator` (the `EnforcerCheck` seam + runtime `enforcer_status`/`pending_gc_tasks`).

### Non-goals

- The `conductor_harness_check` agent tool (Phase 13) — this phase ships the runner + the
  `conductor harness check` CLI, not in-session tool injection.
- Routing GC issues to a `gc_agent` pipeline — that is the Agent Router's job (the routing rule
  matching `gc_task` already exists); this phase only creates the GC issues.
- Wiring the debt/architectural prompt sections into the live turn assembly (Phase 13) — the
  formatters ship and are unit-tested; insertion at SPEC §16.1 steps 7–8 is a later seam.
