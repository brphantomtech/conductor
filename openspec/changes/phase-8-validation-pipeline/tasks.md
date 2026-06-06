# Phase 8 — Atomic Tasks (Validation Pipeline)

Phase 8 goal (per [docs/phases.md](../../../docs/phases.md)): run shell checks after each turn and
inject results into the next turn's prompt. SPEC §15, §5.3.13, §4.1.12. Wave A worktree — keep all
logic in `internal/validation`; leave the per-turn call site as a clean entry point (Phase 7 owns
the turn loop); touch the two integration points only in the final commit (see
[docs/parallel-execution-plan.md](../../../docs/parallel-execution-plan.md)). Each task is sized for
one focused session.

## 1. Result Model

- [x] 1.1 Define `ValidationResult` (check id, name, status enum `passed|failed|timeout`, exit code, truncated output, duration) and `ValidationPipelineResult` (per-check slice) populating `RunAttempt.validation_results` (SPEC §4.1.12).
- [x] 1.2 Define the turn-failure reason format `validation_pipeline_failed:<check_id>`.
- [x] 1.3 Unit tests: status enum round-trip; result JSON shape matches the persisted schema.

## 2. Check Runner

- [x] 2.1 Define a command-factory interface (inject fakes in tests) that builds a check command pinned to the workspace root via the existing workspace command path.
- [x] 2.2 Implement per-check execution under `context.WithTimeout(timeout_ms)`; capture stdout/stderr/exit code; classify exit 0 → `passed`, non-zero → `failed`, deadline → `timeout`; truncate output to `output_max_bytes`; emit `ValidationCheckRun`.
- [x] 2.3 Implement the ordered pipeline `Run(ctx, workspace, turnIndex) ValidationPipelineResult` over `validation.checks`.
- [x] 2.4 Unit tests (temp workspace, fake/script commands): pass / fail / timeout classification; output truncation; ordered execution; per-check audit event.

## 3. Persistence

- [x] 3.1 Persist results to `.conductor/validation/<turn_index>.json` with an atomic temp+rename write.
- [x] 3.2 Unit tests: file written at the right path for a turn index; atomic write leaves no partial file on simulated failure; round-trips back to the result type.

## 4. Severity Decision

- [x] 4.1 Implement the pure `failTurn(results, fail_on_severity) (failed, reason)`: first check at/above threshold that failed or timed out → `validation_pipeline_failed:<check_id>`; unset threshold → never fail.
- [x] 4.2 Unit tests: at-threshold failure fails; below-threshold does not; timeout counts as failure; unset threshold never fails.

## 5. Context Formatter

- [x] 5.1 Implement the pure `FormatContext(results, maxBytes)` producing the SPEC §15.4 `## Validation Results (Previous Turn)` block (status glyphs + failure detail), truncated.
- [x] 5.2 Unit tests: mixed pass/fail/timeout layout; failure-detail section; budget truncation.

## 6. CLI Wiring

- [x] 6.1 Implement `conductor validation run` in a new `cmd/conductor/cmd/cmd_validation.go` (ad-hoc execution against a workspace).
- [ ] 6.2 **Integration commit (final, append-only):** register `newValidationCommand()` in `cmd/conductor/cmd/root.go`; construct the pipeline in `cmd/conductor/cmd/start.go` `runOrchestrator` and expose it for the turn loop; update `AGENTS.md` navigation for `internal/validation`.

## 7. Verification

- [x] 7.1 `go build ./...` and `go vet ./...` clean; repo linter passes for `internal/validation`.
- [x] 7.2 `go test ./internal/validation/...` passes; package coverage ≥ 70% (matching prior phases).
- [x] 7.3 Smoke: `conductor validation run` against a workspace with a trivial passing and a trivial failing check reports both classifications and writes `.conductor/validation/<turn_index>.json`.
