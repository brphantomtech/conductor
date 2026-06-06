# Phase 12 — Atomic Tasks (Harness Enforcer)

Phase 12 goal (per [docs/phases.md](../../../docs/phases.md)): enforce architectural invariants
pre-dispatch and on a schedule. SPEC §11, §4.1.8, §5.3.11–§5.3.12, §8.6. Wave B worktree — keep
enforcer logic in `internal/harness` (new files; do not rewrite the Phase 2 loader); confine any
orchestrator edit to honoring `enforcer_status == blocked`, never `tick.go` control flow; touch the
integration points only in the final commit (see
[docs/parallel-execution-plan.md](../../../docs/parallel-execution-plan.md)). Each task is sized for
one focused session.

## 1. Rule Runner

- [ ] 1.1 Define the violation record type (rule id, name, category, severity, summary, fix_hint, affected files) and a command-factory interface (inject fakes in tests) pinned to the workspace root.
- [ ] 1.2 Implement the rule runner: execute each `HarnessRule.check` under a timeout; exit 0 → pass, non-zero → violation at the rule's severity; emit `HarnessViolationDetected`.
- [ ] 1.3 Unit tests (temp workspace, fake commands): pass vs violation; severity tagging; timeout → violation; audit event per violation.

## 2. Pre-Dispatch Check (EnforcerCheck seam)

- [ ] 2.1 Implement `PreDispatch(ctx) (EnforcerStatus, error)`: when `drift_check_on_dispatch`, run rules, build debt/architectural sections, return `blocked` when a blocking violation exists and `blocking_violations_halt_dispatch` is set; emit `HarnessEnforcerBlocked` on block.
- [ ] 2.2 Confine the orchestrator change to honoring `enforcer_status == blocked` (skip dispatch that tick); do NOT modify `tick.go` control flow, runtime-state shape, or reconciliation. Keep existing orchestrator tests green.
- [ ] 2.3 Unit tests: blocking + halt → blocked + no dispatch + event; warning/error only → proceed; drift check disabled → `clear`, no run.

## 3. Scheduled GC

- [ ] 3.1 Implement the robfig/cron job on `gc_schedule_cron` (injected clock): run rules; for each `auto_fix` violation create a tracker issue (title/description/label/state per SPEC §11.3) deduped against open GC issues for the same `rule_id`; emit `GCTaskCreated`; report `pending_gc_tasks`.
- [ ] 3.2 Unit tests (fake tracker + clock): issue created for auto_fix violation; dedup prevents duplicate on rerun; non-auto_fix violation creates nothing.

## 4. Layer Violation Translation & Formatters

- [ ] 4.1 Translate Knowledge Engine `CheckLayerViolations` output into `dependency`-category violation records.
- [ ] 4.2 Implement the pure `FormatTechnicalDebt` (`## Known Technical Debt`) and `FormatArchitecturalIssues` (`## Architectural Issues You Must Fix`) formatters.
- [ ] 4.3 Unit tests: layer violation → dependency-category record; formatter shapes.

## 5. CLI Wiring

- [ ] 5.1 Implement `conductor harness check` (on-demand rule run, grouped by severity) — extend the existing `harness` command in a new `cmd/conductor/cmd/cmd_harness_check.go`.
- [ ] 5.2 **Integration commit (final, append-only):** register the `check` subcommand under the existing `harness` command; construct the enforcer and wire `orchestrator.WithEnforcer(...)` plus the GC cron in `cmd/conductor/cmd/start.go` `runOrchestrator`; update `AGENTS.md` navigation for the enforcer.

## 6. Verification

- [ ] 6.1 `go build ./...` and `go vet ./...` clean; repo linter passes for the new `internal/harness` enforcer files; existing harness/orchestrator tests still green.
- [ ] 6.2 `go test ./internal/harness/... ./internal/orchestrator/...` passes; new enforcer code coverage ≥ 70%.
- [ ] 6.3 Smoke: `conductor harness check` against a workspace with a failing rule reports the violation grouped by severity.
