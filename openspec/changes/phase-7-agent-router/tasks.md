# Phase 7 — Atomic Tasks (Agent Router & Pipelines)

Phase 7 goal (per [docs/phases.md](../../../docs/phases.md)): multi-role pipelines (planner → coder →
verifier → reviewer) with rule-based routing. SPEC §12, §4.1.4, §5.3.7, §16. Wave A worktree — keep
router logic in `internal/router`; confine the orchestrator edit to the dispatch path
(`internal/orchestrator/dispatch.go`), never `tick.go`; touch the `start.go` integration point only in
the final commit (see [docs/parallel-execution-plan.md](../../../docs/parallel-execution-plan.md)).
This is the only Wave A phase that edits `internal/orchestrator` — merge it accordingly. Each task is
sized for one focused session.

## 1. Classification

- [ ] 1.1 Implement `Classify(ctx, candidates)` (the orchestrator `Classifier` seam): for each unclassified issue invoke the `default` provider with the SPEC §12.2 prompt, set `task_type` to a valid value, record a classification audit event; skip already-classified issues.
- [ ] 1.2 On classification provider failure, default `task_type` to `unknown` and proceed (never block dispatch).
- [ ] 1.3 Unit tests (fake provider): unclassified issue classified once + event; already-classified skipped; provider error → `unknown`.

## 2. Pipeline Selection

- [ ] 2.1 Implement `SelectPipeline(issue) []string`: evaluate `routing.rules` in order as a conjunction of present conditions (labels, any_label, task_type, state, title regex, complexity); first match wins; fall back to `routing.pipeline`.
- [ ] 2.2 Unit tests: first-match precedence; multi-condition conjunction; any_label vs labels; title regex; fallback when no rule matches.

## 3. Prompt Construction (SPEC §16)

- [ ] 3.1 Implement the Liquid variable set (SPEC §16.2): issue fields plus `attempt`, `agent_role`, `pipeline`, `pipeline_index`, `pipeline_length`, `memory_summary`, `knowledge_summary`.
- [ ] 3.2 Implement `BuildPrompt(inputs)`: assemble sections in SPEC §16.1 order (role template → validation → previous-role output → codebase context → docs → memory → known debt → architectural issues), omitting absent sources.
- [ ] 3.3 Implement bottom-up truncation at `context_budget * 0.7` (drop sections 7–8 first); map unknown variable/filter → `template_render_error` (reuse Phase 2 render error) failing the attempt.
- [ ] 3.4 Implement continuation prompt selection (SPEC §16.3): HARNESS `## continuation` section if present, else built-in; never re-send the original prompt on continuation turns.
- [ ] 3.5 Unit tests: section order with absent sections omitted; over-budget bottom-up truncation keeps the role template; unknown variable fails; continuation template chosen on later turns.

## 4. Pipeline Execution

- [ ] 4.1 Implement `RunPipeline(ctx, runCtx)`: iterate roles, resolve per-role `ProviderConfig` (role override → `default`), build prompt, execute the turn via `provider.Adapter`, capture output as `## Output from Previous Role (<role>)`, advance `pipeline_index`; emit `PipelineRoleStarted`/`PipelineRoleEnded`.
- [ ] 4.2 Call the Validation Pipeline at SPEC §12.4 step 5 when present (nil-safe when Phase 8 is not yet merged); a role turn failure (incl. validation failure) fails the attempt for orchestrator retry/backoff.
- [ ] 4.3 Implement continuation loop: after the last role re-fetch the issue; continue while active and `turn_count < max_turns`; otherwise exit (Phase 6 schedules the 1s continuation retry).
- [ ] 4.4 Unit tests (fakes): two-role execution with output hand-off + index advance; per-role provider resolution; failed role fails attempt; continuation start/stop conditions; turn cap.

## 5. Orchestrator Dispatch Integration

- [ ] 5.1 In `internal/orchestrator/dispatch.go`, replace the hardcoded `coderRole`/`pipeline=[coder]` with the router-selected pipeline; set `RunAttempt.pipeline` from `SelectPipeline`; drive `RunPipeline` in the worker. Do not modify `tick.go`, runtime state, or reconciliation.
- [ ] 5.2 Unit tests: dispatch sets the router pipeline; single-role `[coder]` pipeline reproduces Phase 6 behavior; workspace-creation failure still classified + claim released.

## 6. CLI Wiring

- [ ] 6.1 **Integration commit (final, append-only):** construct the router and wire `orchestrator.WithClassifier(...)` plus router-driven dispatch in `cmd/conductor/cmd/start.go` `runOrchestrator`; update `AGENTS.md` navigation for `internal/router`.

## 7. Verification

- [ ] 7.1 `go build ./...` and `go vet ./...` clean; repo linter passes for `internal/router` and the `internal/orchestrator` dispatch changes.
- [ ] 7.2 `go test ./internal/router/... ./internal/orchestrator/...` passes; `internal/router` coverage ≥ 70%; existing orchestrator tests still green.
- [ ] 7.3 Smoke: a fake-tracker integration test dispatches an issue through a multi-role pipeline (e.g. `[planner, coder]`) with classification and output hand-off, and a single-role config reproduces the Phase 6 path.
