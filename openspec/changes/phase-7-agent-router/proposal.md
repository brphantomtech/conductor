# Phase 7 — Agent Router & Pipelines

## Why

Phase 6 dispatches a single hardcoded `coder` turn. Real work needs more than one role: a planner to
decompose an issue, a coder to implement, a verifier to check, a reviewer to write up the change. The
Agent Router selects the right pipeline per issue (by classification and routing rules) and drives it
role-by-role, passing each role's output to the next and handling multi-turn continuation. This turns
Conductor from a one-shot coder into the multi-role orchestration the SPEC describes, and it replaces
the Phase 6 `coderRole` constant with router-selected pipelines without touching the orchestrator's
lifecycle (the `RunAttempt.pipeline` / `pipeline_index` fields were built in Phase 6 for exactly
this).

This is a **Wave A** change (see [docs/parallel-execution-plan.md](../../../docs/parallel-execution-plan.md)):
it lands an independent `internal/router` package and plugs into the orchestrator's existing
`Classifier` seam, buildable in its own worktree concurrently with Phases 8, 9, and 10.

Authoritative scope: [docs/phases.md](../../../docs/phases.md) → "Phase 7 — Agent Router &
Pipelines"; SPEC §12 (Agent Router), §4.1.4 (AgentRole), §5.3.7 (routing config), §16 (Prompt
Construction).

## What Changes

- **New `internal/router` package** implementing pipeline selection and execution:
  - **Issue classification** (SPEC §12.2) — on first dispatch of an unclassified issue, invoke the
    `default` provider with the classification prompt, set `issue.task_type` (one of `feature`,
    `bug`, `refactor`, `investigation`, `docs`, `gc_task`, `unknown`), and record an audit event.
    Supplied to the orchestrator via the existing `Classifier` seam (`WithClassifier`).
  - **Pipeline selection** (SPEC §12.3) — evaluate `routing.rules` in order against the issue
    (labels, any_label, task_type, state, title regex, complexity); first match wins; fall back to
    `routing.pipeline`.
  - **Pipeline execution** (SPEC §12.4) — for each role in the selected pipeline: resolve the role's
    `ProviderConfig` (fall back to `default`), build the turn prompt, execute the turn, run the
    Validation Pipeline if enabled, capture the role's output as `## Output from Previous Role
    (<role>)` for the next role, and advance; a failed role fails the attempt (orchestrator
    retry/backoff applies).
  - **Continuation handling** (SPEC §12.5, §16.3) — after the last role, re-fetch the issue; if still
    active and `turn_count < max_turns`, run another pipeline iteration using continuation prompts;
    otherwise exit. The full original prompt is never re-sent on continuation turns.
- **Prompt construction** (SPEC §16) — the assembly-order builder (role template → validation →
  previous-role output → codebase context → docs → memory → harness violations) with the SPEC §16.2
  Liquid variables (`pipeline`, `pipeline_index`, `pipeline_length`, `agent_role`, `attempt`, etc.)
  and bottom-up truncation at `context_budget * 0.7`. Sections whose source engine is not yet wired
  (knowledge/docs/memory/enforcer) are simply omitted, exactly as SPEC §16.1 specifies.
- **Per-role provider resolution** (SPEC §4.1.4) — each role may carry its own `ProviderConfig`.
- **Audit events** (already declared): `PipelineRoleStarted`, `PipelineRoleEnded` per role; the
  classification audit event.
- **Unit tests** with fakes: classification call + task_type set, rule evaluation precedence and
  fallback, role-by-role execution with output hand-off, continuation start/stop conditions, prompt
  assembly order + truncation, per-role provider resolution. No live API calls in CI.

## Capabilities

### New Capabilities

- `agent-router`: issue classification, routing-rule evaluation with fallback, role-by-role pipeline
  execution with output hand-off, multi-turn continuation handling, the SPEC §16 prompt-construction
  builder with Liquid variables and budget truncation, per-role provider resolution, and the
  `PipelineRoleStarted`/`PipelineRoleEnded` audit events.

### Modified Capabilities

- `orchestrator-core`: dispatch uses the router-selected pipeline instead of the hardcoded
  `[coder]`, and classification is wired through the existing `Classifier` seam. The state machine,
  runtime state, retry/backoff, and reconciliation are unchanged. (Delta in
  `specs/orchestrator-core/spec.md`.)

## Impact

- **Affected specs**: new capability `agent-router`; modified `orchestrator-core` (router-selected
  dispatch + classification seam).
- **Affected code**: `internal/router/` (new). `internal/orchestrator/` — dispatch swaps the
  hardcoded `coderRole`/`pipeline=[coder]` for the router-provided pipeline; the `Classifier` seam is
  implemented by the router. (These edits are confined to the dispatch path; the poll loop, runtime
  state, and lifecycle are untouched.)
- **Integration points (last commit only, append-only)** per the Wave A integration-commit rule:
  - `cmd/conductor/cmd/start.go` `runOrchestrator` — construct the router and wire
    `orchestrator.WithClassifier(...)` plus the router-driven dispatch.
- **Consumes (unchanged)**: `internal/config` (`Routing`, `RoutingRule`, `Providers`,
  `ProviderConfig`), `internal/provider` (turn execution + classification call), `internal/harness`
  (role templates + Liquid render), `internal/audit`. Optionally calls `internal/validation` (Phase
  8) at SPEC §12.4 step 5 when present.

### Non-goals

- The Knowledge / Doc / Memory / Harness context sections of the prompt (Phases 10/11/9/12) — the
  assembly builder leaves their slots in place and omits them until those engines are wired, per SPEC
  §16.1.
- The Validation Pipeline implementation itself (Phase 8) — the router calls it at step 5 when
  available but does not implement it.
- Complexity estimation by a planner beyond setting `task_type` (full `estimated_complexity` flows
  from the planner role's output; routing on `complexity` is supported when present).
- The HTTP/WebSocket streaming of role output (Phase 14).
