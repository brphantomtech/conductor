## Context

The Validation Pipeline depends only on substrate already on `main`: `config.Validation` /
`config.ValidationCheck` (every field already declared in
[internal/config/types.go](../../../internal/config/types.go)), the workspace root and `.conductor/`
layout from Phase 5, `audit.Writer`, and the `RunAttempt.validation_results` slot plus the
turn-failure path from Phase 6. It is the smallest of the Wave A engines: a deterministic shell-
command runner with classification, persistence, a failure decision, and a formatter — no provider
or embedding calls.

SPEC anchors: §15.2 (execution + classification + persistence), §15.3 (turn failure on severity),
§15.4 (context injection format), §5.3.13 (config), §4.1.12 (`validation_results`). Conventions to
match (Phases 2–6): constructor + functional `Option`s, injected clock where time matters (timeout),
sentinel/typed failure reasons matching the SPEC strings, table-driven tests over a temp workspace,
and audit emission via a small helper.

This is a Wave A worktree. All logic stays in `internal/validation`; the only shared-file edits are
the two append-only integration points done in the final commit. The per-turn invocation itself is
deliberately left as a clean entry point for the Phase 7 router (which owns the turn loop) so this
phase does not race Phase 7 on the worker code.

## Goals / Non-Goals

**Goals:**

- A correct check runner: per-check command execution in the workspace root, per-check timeout,
  exit-code classification, output truncation.
- Per-turn JSON persistence at `.conductor/validation/<turn_index>.json`.
- A `fail_on_severity` decision producing `validation_pipeline_failed:<check_id>` for the
  orchestrator's existing turn-failure path.
- The `## Validation Results (Previous Turn)` formatter for next-turn injection.
- A `conductor validation run` CLI for ad-hoc execution.

**Non-Goals:**

- The agent tool (`conductor_validation_run`) — Phase 13.
- The live per-turn call site — Phase 7 router turn loop.
- Harness rule enforcement — Phase 12. Episodic-memory write of failures — Phase 9.

## Decisions

### Process execution via the workspace, not raw `exec`

Checks run through the workspace's command construction (the same path the orchestrator uses for
agent subprocesses) so cwd is pinned to the workspace root and the SPEC §14.2 invariants hold.

- **Why over a bare `exec.Command`:** SPEC §15.2 says "execute command in workspace root"; reusing
  the workspace command path keeps the cwd/safety invariants centralized and consistent with how
  Phase 6 already runs subprocesses. The runner takes a small command-factory interface so tests
  inject fake commands without spawning real processes.

### Per-check timeout via context, classification by outcome

Each check runs under a `context.WithTimeout(check.timeout_ms)`. Outcome maps: exit 0 → `passed`,
non-zero → `failed`, context deadline → `timeout` (SPEC §15.2 step 3). stdout+stderr are captured
and truncated to `output_max_bytes` (step 4).

- **Why:** matches SPEC §15.2 verbatim; context timeout is the idiomatic Go cancellation already
  used by the Phase 6 worker, so a stalled check cannot hang the pipeline.

### Results are a value type written atomically per turn

The pipeline returns a `ValidationPipelineResult` (the per-check `ValidationResult` slice) that both
populates `RunAttempt.validation_results` and is marshaled to
`.conductor/validation/<turn_index>.json`. The file write is atomic (temp + rename).

- **Why:** SPEC §15.2 step 5 + §4.1.12. One value type serves both the in-memory slot and the
  on-disk record; atomic write avoids a torn file if the process dies mid-turn.

### Severity decision is a pure function over results + threshold

`failTurn(results, fail_on_severity) (failed bool, reason string)` returns
`validation_pipeline_failed:<check_id>` for the first check at/above the threshold that failed or
timed out (SPEC §15.3).

- **Why:** a pure decision is trivially testable across thresholds and severities, and the
  orchestrator already knows how to handle an arbitrary turn-failure reason (Phase 6), so this is a
  string the existing retry/backoff path consumes unchanged.

### Formatter is a pure function with glyphs + truncation

`FormatContext(results, maxBytes) string` renders the SPEC §15.4 block (✅/❌/⚠️ per check plus
failure detail) truncated to the budget.

- **Why:** SPEC §15.4 fixes the shape; keeping it pure means Phase 7/13 can call it at the prompt-
  assembly insertion point (SPEC §16.1 step 2) without this phase touching the turn loop.

## Risks / Trade-offs

- **[CI toolchain dependence]** → Tests use injected fake commands and small scripts (e.g. `exit 0`
  / `exit 1` / a sleep for timeout), not a real linter/test runner, so CI does not depend on a
  project toolchain. The `conductor validation run` smoke uses a trivial check.
- **[Long-running / hanging checks]** → Each check is bounded by its own `timeout_ms` via context;
  `timeout` is a first-class classification, not an error, so one hung check fails its turn without
  blocking the others or the loop.
- **[Large check output]** → Truncated to `output_max_bytes` before persistence and before
  formatting, bounding both disk and prompt size.
- **[Per-turn file path collisions across concurrent runs]** → The file lives under the issue's own
  workspace `.conductor/validation/`, which is per-issue isolated by Phase 5, so concurrent runs for
  different issues do not collide; `<turn_index>` disambiguates within a run.
- **[Ordering vs. Phase 7 turn loop]** → The per-turn invocation is left as an entry point, not wired
  into the worker, so this phase and Phase 7 do not edit the same turn-loop code. Phase 7 (or a
  later integration) calls `Run` at SPEC §12.4 step 5.

## Migration Plan

Additive only. New `internal/validation` package and one new CLI file. The two integration-point
edits (`root.go` AddCommand, `start.go` pipeline construction) land in the final commit per the Wave
A integration-commit rule. New persisted state lives under the per-issue workspace
`.conductor/validation/` directory (no schema change). Rollback is reverting the package + the two
wiring lines. `validation.enabled = false` (config default) means the pipeline never runs and
behavior is unchanged from before this phase.

## Open Questions

- **Where the per-turn call is wired** — runner ships now; the live insertion at SPEC §12.4 step 5 /
  §16.1 step 2 is owned by Phase 7. Confirm the call signature against the router turn loop during
  Phase 7 implementation; the entry point is designed to be called with `(ctx, workspace, turnIndex)`.
- **Concurrent check execution** — SPEC §15.2 says "in order"; start sequential. Revisit parallel
  execution only if check latency becomes a bottleneck (would change the audit ordering).
- **Validation failure → episodic memory** — that write path belongs to Phase 9; this phase only
  produces the results Phase 9 consumes.
