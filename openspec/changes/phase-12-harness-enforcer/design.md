## Context

The Harness Enforcer depends only on substrate already on `main`: the Knowledge Engine's
`CheckLayerViolations` (Phase 10), `config.HarnessRules`/`config.Enforcement` (declared in
[internal/config/types.go](../../../internal/config/types.go)), the tracker adapter (for GC issue
creation), `workspace` (rule command execution), `audit.Writer`, and the orchestrator's
`EnforcerCheck` seam (`WithEnforcer` + no-op default already present from Phase 6). The poll loop is
untouched — Phase 6 built the seam so the enforcer plugs into pre-dispatch step 1 by supplying an
implementation.

SPEC anchors: §11.2 (pre-dispatch), §11.3 (scheduled GC + dedup), §11.4 (layer-violation
translation), §4.1.8 (HarnessRule), §5.3.11–§5.3.12 (config), §8.6 (layer direction). Conventions to
match (Phases 2–10): constructor + functional `Option`s, injected clock for the cron, fake
tracker/knowledge in tests, sentinel reasons equal to SPEC strings, audit via a small helper.

This is a Wave B worktree. Enforcer logic lives in `internal/harness` (extending the Phase 2 loader
package, not modifying its files); the only shared-file edits are the two append-only integration
points in the final commit.

## Goals / Non-Goals

**Goals:**

- A rule runner executing `check` commands in the workspace root with severity classification.
- The pre-dispatch check supplied via the `EnforcerCheck` seam, with the three severity behaviors
  including blocking-halt.
- Scheduled GC creating deduplicated tracker issues for `auto_fix` violations.
- Knowledge-Engine layer-violation translation into `dependency`-category violations.
- The `## Known Technical Debt` / `## Architectural Issues You Must Fix` formatters and a
  `conductor harness check` CLI.

**Non-Goals:**

- The `conductor_harness_check` agent tool (Phase 13); routing GC issues (Agent Router); live prompt
  wiring (Phase 13).

## Decisions

### Rule runner reuses the workspace command path

Each `HarnessRule.check` runs via the workspace command construction (cwd pinned to the workspace
root, SPEC §14.2 invariants) under a timeout, classifying exit 0 → pass, non-zero → violation at the
rule's `severity`.

- **Why:** SPEC §11.2 step 1 ("in the workspace root"); reuses the same execution path the
  Validation Pipeline and orchestrator already use, so cwd/safety is centralized. A small
  command-factory interface lets tests inject fakes (no real shell dependency in CI).

### Enforcer implements the `EnforcerCheck` seam; blocking-halt via the returned status

`PreDispatch(ctx) (EnforcerStatus, error)` runs the rules, builds the debt/architectural prompt
sections (stored for the dispatch to inject), and returns `blocked` when a blocking violation exists
and `blocking_violations_halt_dispatch` is set. The orchestrator already records the status; the
minimal halt behavior keys off `enforcer_status == blocked`.

- **Why:** Phase 6 built `EnforcerCheck` returning `EnforcerStatus` for exactly this. Keeping the
  decision in the enforcer (pure over rule results) makes it testable; the orchestrator change, if
  any, is confined to honoring `blocked` and stays off other Wave B phases' files.

### Scheduled GC via robfig/cron with tracker-side dedup

A cron job on `gc_schedule_cron` runs the rules; for each `auto_fix` violation it queries the
tracker for an open issue labeled `gc_issue_label` referencing the same `rule_id` and creates one
only if absent.

- **Why:** SPEC §11.3 verbatim, including the dedup step (3a). robfig/cron is the SPEC-named
  scheduler. Dedup keys on `rule_id` embedded in the issue (title/label/marker) so reruns don't spam
  the tracker. Injected clock + fake tracker make the cron deterministic in tests.

### Layer violations translated, not re-detected

The enforcer calls the Knowledge Engine's `CheckLayerViolations` and maps each result to a
`dependency`-category HarnessRule violation record.

- **Why:** SPEC §8.6/§11.4 — detection lives in the Knowledge Engine (which has the graph);
  the enforcer only translates, avoiding duplicate graph logic.

### Formatters are pure functions

`FormatTechnicalDebt(violations)` → `## Known Technical Debt`; `FormatArchitecturalIssues(violations)`
→ `## Architectural Issues You Must Fix`.

- **Why:** SPEC §16.1 steps 7–8 fix the section names; pure functions are unit-testable and let
  Phase 13 insert them at prompt-assembly time without this phase touching the turn loop.

## Risks / Trade-offs

- **[Blocking-halt wiring touches the orchestrator]** → Confined to honoring `enforcer_status ==
  blocked` at dispatch; no `tick.go` control-flow rewrite, no runtime-state shape change. If a code
  change is needed it is minimal and keeps existing orchestrator tests green. No other Wave B phase
  touches the orchestrator, so this is conflict-free.
- **[GC issue spam on flapping violations]** → Dedup queries the tracker for an existing open GC
  issue per `rule_id` before creating; covered by a fake-tracker test asserting no duplicate on
  rerun.
- **[Rule command latency / hangs]** → Each `check` runs under a timeout; a timed-out rule is a
  violation, not a hang, and never blocks the tick.
- **[Cron + live tracker in tests]** → Injected clock drives the schedule; a fake tracker records
  created issues. No real cron sleep, no live tracker call in CI.
- **[False blocking halts stopping all work]** → Halt only when both a `blocking` violation exists
  AND `blocking_violations_halt_dispatch` is true; default config leaves dispatch unblocked.

## Migration Plan

Additive plus a minimal, confined orchestrator dispatch guard (honor `blocked`). New enforcer files
in `internal/harness`; a `conductor harness check` subcommand. The two integration-point edits
(`start.go` `WithEnforcer` + GC cron, `root.go`/harness command registration) land in the final
commit per the Wave B integration-commit rule. No DB/config schema change (`HarnessRules`/
`Enforcement` already modeled). Rollback is reverting the enforcer files + the wiring.
`enforcement.enabled = false` and `drift_check_on_dispatch = false` leave behavior unchanged; the
no-op `EnforcerCheck` default still applies when the enforcer is not wired.

## Open Questions

- **Exactly where blocking-halt is enforced** — honoring `enforcer_status == blocked` in the
  dispatch path vs. also implementing the `PreflightCheck` seam to skip steps 5–8. Start with the
  dispatch guard; confirm against the Phase 6 tick semantics during implementation.
- **GC dedup marker** — encode `rule_id` in the issue label, title, or a hidden body marker; default
  to a body marker plus the configured label, revisit if the tracker adapter lacks a search filter.
- **Where debt/architectural sections are injected** — formatters ship now; live insertion at SPEC
  §16.1 steps 7–8 is owned by Phase 13.
