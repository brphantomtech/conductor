# Phase 8 — Validation Pipeline

## Why

Conductor runs agent turns (Phase 6) but takes their output on faith: nothing checks whether the
code compiles, lints, or passes tests before the next turn or a PR. The Validation Pipeline is the
"shift feedback left" mechanism from Harness Engineering — implemented as infrastructure rather than
a team convention. After each turn it runs the project's configured shell checks in the workspace,
persists the results, and injects them into the next turn's prompt so the agent sees and fixes its
own failures. It also gives the orchestrator a real turn-failure signal (`fail_on_severity`) so
retry/backoff fires on broken output instead of only on crashes.

This is a **Wave A** change (see [docs/parallel-execution-plan.md](../../../docs/parallel-execution-plan.md)):
it lands an independent `internal/validation` package, buildable in its own worktree concurrently
with Phases 7, 9, and 10.

Authoritative scope: [docs/phases.md](../../../docs/phases.md) → "Phase 8 — Validation Pipeline";
SPEC §15 (Validation Pipeline), §5.3.13 (validation config), §4.1.12 (`validation_results` on Run
Attempt).

## What Changes

- **New `internal/validation` package** implementing the pipeline runner (SPEC §15.2): for each
  check in `validation.checks`, execute the command in the workspace root with a per-check
  `timeout_ms`, capture stdout/stderr/exit code, classify (exit 0 → `passed`, non-zero → `failed`,
  timeout → `timeout`), and truncate output to `output_max_bytes`.
- **`ValidationResult` / `ValidationPipelineResult` types** populating the `RunAttempt.validation_results`
  slot already defined in Phase 6 (SPEC §4.1.12).
- **Per-turn persistence** (SPEC §15.2 step 5) — write results to
  `.conductor/validation/<turn_index>.json` in the workspace.
- **Severity-based turn failure** (SPEC §15.3) — when `validation.fail_on_severity` is set, any
  check at or above the threshold that fails or times out marks the turn failed with reason
  `validation_pipeline_failed:<check_id>`, which the orchestrator treats like any other turn failure
  (retry/backoff applies).
- **Context-injection formatter** (SPEC §15.4) — when `inject_results_into_context` is true, render
  the `## Validation Results (Previous Turn)` block (status glyphs + per-check failure detail,
  truncated) for prepending to the next turn prompt.
- **`ValidationCheckRun` audit event** (already declared in `internal/audit`) emitted per check.
- **`conductor validation run` subcommand** for ad-hoc execution against a workspace, in its own
  `cmd_validation.go` file.
- **Unit tests** with a temp workspace and fake/fixture commands: each classification (pass / fail /
  timeout), output truncation, per-turn JSON persistence, the `fail_on_severity` decision across
  thresholds, and the formatter output. No reliance on a specific external toolchain in CI.

## Capabilities

### New Capabilities

- `validation-pipeline`: the check runner with per-check timeout and classification, the
  ValidationResult model populating `RunAttempt.validation_results`, per-turn JSON persistence,
  severity-based turn-failure decision, the `## Validation Results` context formatter, the
  `validation run` subcommand, and `ValidationCheckRun` audit emission.

### Modified Capabilities

None. The orchestrator, workspace, and config layers are consumed unchanged; the `validation_results`
slot and `Validating` state already exist from Phase 6.

## Impact

- **Affected specs**: new capability `validation-pipeline` (delta in
  `specs/validation-pipeline/spec.md`).
- **Affected code**: `internal/validation/` (new). New `cmd/conductor/cmd/cmd_validation.go`.
- **Integration points (last commit only, append-only)** per the Wave A integration-commit rule:
  - `cmd/conductor/cmd/root.go` — `root.AddCommand(newValidationCommand())`.
  - `cmd/conductor/cmd/start.go` `runOrchestrator` — construct the pipeline so it is available to
    the turn loop. (The actual per-turn invocation point is owned by the Phase 7 router turn loop /
    Phase 6 worker; this phase ships the runner + ad-hoc CLI and a clean entry point for that
    wiring.)
- **Consumes (unchanged)**: `internal/config` (`Validation`, `ValidationCheck`), `internal/workspace`
  (workspace root + `.conductor/` path), `internal/audit`, `internal/orchestrator` (the
  `RunAttempt.validation_results` slot and turn-failure reason).

### Non-goals

- The `conductor_validation_run` agent tool (Phase 13) — this phase ships the runner + the
  `conductor validation run` CLI, not in-session tool injection.
- Wiring validation into the live per-turn loop end-to-end — the per-turn call site lands with the
  Phase 7 router (SPEC §12.4 step 5) which owns the role-by-role turn execution; this phase delivers
  the runner, persistence, formatter, and failure decision it will call.
- Harness rule enforcement (Phase 12) — validation checks are project shell commands, distinct from
  `harness_rules`.
- Auto-saving validation failures as episodic memory (Phase 9 owns that write path).
