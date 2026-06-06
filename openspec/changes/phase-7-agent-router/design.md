## Context

The Agent Router is the one Wave A phase that touches `internal/orchestrator`, but the surface is
small and pre-planned: Phase 6 already stored `pipeline` / `pipeline_index` on `RunAttempt`, exposed
the `Classifier` seam (`WithClassifier` in
[orchestrator.go](../../../internal/orchestrator/orchestrator.go), no-op default in
[seams.go](../../../internal/orchestrator/seams.go)), and isolated the single-role dispatch in
[dispatch.go](../../../internal/orchestrator/dispatch.go) behind the `coderRole` constant. Phase 7
replaces that constant with a router-selected pipeline and supplies the `Classifier` implementation —
it does **not** touch [tick.go](../../../internal/orchestrator/tick.go) (the poll loop) or the
runtime state. This keeps it off the files Phase 12 would later touch and confines the orchestrator
edit to the dispatch path.

SPEC anchors: §12.2 (classification), §12.3 (selection), §12.4 (execution), §12.5 (continuation),
§4.1.4 (roles + per-role provider), §16.1 (assembly order), §16.2 (Liquid variables), §16.3
(continuation prompts). All routing config (`Routing`, `RoutingRule`, `RoutingMatch`) and provider
resolution already exist in [internal/config/types.go](../../../internal/config/types.go); harness
role templates + Liquid render exist from Phase 2. Conventions to match (Phases 2–6): constructor +
functional `Option`s, fake provider/tracker in tests, sentinel reasons matching SPEC strings, audit
via a small helper.

## Goals / Non-Goals

**Goals:**

- Classification on first dispatch that sets `task_type` and is wired via the existing seam.
- Rule evaluation with first-match precedence and `routing.pipeline` fallback.
- Role-by-role execution with per-role provider resolution and output hand-off.
- Continuation handling honoring `max_turns` and active-state re-fetch.
- A SPEC §16 prompt builder: correct section order, Liquid variables, bottom-up truncation, omit
  absent sections.
- Confine the orchestrator change to the dispatch path; leave the poll loop and lifecycle untouched.

**Non-Goals:**

- Knowledge/Doc/Memory/Harness context sections (their engines) — slots left, omitted until wired.
- The Validation Pipeline implementation (Phase 8) — called at step 5 when present.
- Role-output streaming over HTTP/WS (Phase 14).

## Decisions

### Router implements the `Classifier` seam; dispatch asks the router for the pipeline

The router exposes `Classify(ctx, candidates)` (the seam) and `SelectPipeline(issue) []string`.
Dispatch calls `SelectPipeline` to set `RunAttempt.pipeline` instead of the hardcoded `[coder]`.

- **Why over rewriting the poll loop:** Phase 6 built these exact seams so Phase 7 is a wiring +
  dispatch change, not a control-flow rewrite. Classification stays in the poll-loop step 6 seam;
  pipeline selection happens at dispatch where the issue is claimed. This is the minimal edit and it
  avoids `tick.go` entirely.

### Pipeline execution as a role loop driving the existing worker turn

The router owns a `RunPipeline(ctx, runCtx)` that iterates roles: resolve `ProviderConfig` (role
override → `default`), build the prompt, run the turn via `provider.Adapter`, optionally run
validation (Phase 8) at step 5, capture output, advance `pipeline_index`, emit
`PipelineRoleStarted`/`PipelineRoleEnded`. A failed role returns a turn-failure to the orchestrator's
existing retry/backoff.

- **Why over per-role goroutines:** SPEC §12.4 is strictly sequential with output hand-off; a simple
  loop preserves ordering and audit causality. Concurrency stays at the issue level (Phase 6
  semaphore), not the role level.

### Output hand-off via `## Output from Previous Role (<role>)`

Each role's captured output is passed forward as a named section the next role's prompt includes
(SPEC §12.4 / §16.1 step 3).

- **Why:** verbatim SPEC; keeping it a prompt section (not a structured channel) means the assembly
  builder owns all cross-role context uniformly.

### Prompt builder is a pure, section-ordered assembler

`BuildPrompt(inputs) (string, error)` renders the role template with the §16.2 Liquid variables, then
appends sections 2–8 in order, omitting any whose source is disabled/empty, then truncates from the
bottom (sections 7–8 first) if the result exceeds `context_budget * 0.7`. Unknown variable/filter →
`template_render_error` (reuses the Phase 2 render error, failing the attempt per §16.2).

- **Why:** SPEC §16.1/§16.2 fix the order, variables, and truncation. A pure builder is fully
  unit-testable and lets later phases (knowledge/docs/memory) supply their section content through the
  same inputs struct without changing the loop.

### Continuation re-fetches issue state, never re-sends the original prompt

After the last role, `RunPipeline` re-fetches the issue; if still in an active state and `turn_count <
max_turns`, it runs another iteration with the continuation template (HARNESS `## continuation`
section or the built-in default, SPEC §16.3); otherwise it exits and the orchestrator schedules the
1-second continuation retry (already in Phase 6).

- **Why:** SPEC §12.5 verbatim; the §16.3 rule "original prompt is never re-sent" is enforced by using
  the continuation template on turns after the first.

## Risks / Trade-offs

- **[Orchestrator dispatch edit collides with another phase]** → The change is confined to
  `dispatch.go` (swap `coderRole`/`[coder]` for router output) and the `start.go` wiring; it does not
  touch `tick.go`, runtime state, or reconciliation, so it stays off Phase 12's later surface. This is
  the only Wave A phase that edits `internal/orchestrator`; merge it accordingly.
- **[Classification cost / latency on every issue]** → Classification runs only on first dispatch of an
  unclassified issue (SPEC §12.2) and the result is stored on the issue; subsequent dispatches skip
  it. Tests assert it fires once.
- **[Routing rule misconfiguration → wrong pipeline]** → First-match-wins with an explicit
  `routing.pipeline` fallback; rule evaluation is a pure function with table tests covering precedence,
  multi-condition conjunction, and fallback. Invalid regex in `title_matches` is surfaced at config
  validation, not at dispatch.
- **[Prompt truncation dropping critical content]** → Truncation is bottom-up (lowest-priority sections
  7–8 first) per SPEC §16.1, preserving the role template and validation/previous-role output; covered
  by a builder test that asserts which sections survive at the budget.
- **[Continuation infinite loop]** → Bounded by `max_turns` and the active-state re-fetch; a turn that
  fails goes through retry/backoff (capped) rather than continuation.

## Migration Plan

Additive plus a confined orchestrator dispatch edit. New `internal/router` package; `dispatch.go`
swaps the hardcoded role for the router pipeline and the router implements the `Classifier` seam. The
`start.go` wiring (`WithClassifier` + router construction) lands in the final commit per the Wave A
integration-commit rule. No DB or config schema change (routing + per-role provider already modeled).
Rollback is reverting the router package and restoring the `coderRole` dispatch + removing the
`WithClassifier` wiring. With a single-role `routing.pipeline: [coder]` and no rules, behavior is
equivalent to Phase 6.

## Open Questions

- **Where validation is invoked (step 5)** — if Phase 8 is merged first, the router calls
  `validation.Run` at SPEC §12.4 step 5; if not, the step is skipped behind a nil-check. Confirm the
  call signature against the Phase 8 runner during whichever merges second.
- **`turn_count` source of truth** — track on the run attempt vs. derive from audit; start by tracking
  on the in-memory attempt (consistent with Phase 6 lifecycle), revisit if Phase 14 needs it
  persisted.
- **Classification provider failure handling** — on classification error, default `task_type` to
  `unknown` and proceed with the fallback pipeline (so a flaky classifier never blocks dispatch);
  confirm this matches desired operator behavior.
