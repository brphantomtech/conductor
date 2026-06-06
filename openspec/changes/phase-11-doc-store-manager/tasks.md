# Phase 11 — Atomic Tasks (Doc Store Manager)

Phase 11 goal (per [docs/phases.md](../../../docs/phases.md)): index external documentation
alongside the codebase. SPEC §10, §4.1.9, §5.3.10, §20.4, §23.5. Wave B worktree — keep all logic
in `internal/docstore`; touch the two integration points only in the final commit (see
[docs/parallel-execution-plan.md](../../../docs/parallel-execution-plan.md)). Each task is sized for
one focused session.

## 1. Model & Backend Interface

- [x] 1.1 Define `DocRef` (SPEC §4.1.9) and the `DocStoreBackend` interface (`Sync`, `Fetch`, `List`) + a `DocFilter` type (SPEC §20.4).
- [x] 1.2 Declare the SPEC §23.5 sentinel `doc_store_sync_failed` with the verbatim string value.
- [x] 1.3 Unit tests: DocRef round-trip; interface compile-time assertions.

## 2. Backends

- [x] 2.1 Implement the `local_fs` backend (directory walk + checksum + content fetch).
- [x] 2.2 Implement the `git_repo` backend behind an injected git runner (clone/pull, token or SSH); no live network in tests.
- [x] 2.3 Implement the `s3` backend behind an injected object-store client interface; no live network in tests.
- [x] 2.4 Unit tests: `local_fs` list/fetch over a temp dir; `git_repo`/`s3` against fake runner/client.

## 3. Sync Scheduler

- [x] 3.1 Implement the manager (constructor + Options + injected clock): initial sync on startup unless recent; per-store interval ticker re-reading `sync_interval_minutes`.
- [x] 3.2 Implement checksum diff: download only changed docs; emit `DocStoreSynced`; on failure log warning + retain last-good; map failure → `doc_store_sync_failed`.
- [x] 3.3 Unit tests (injected clock): initial-sync gating; only-changed downloaded; failed sync retains prior set + classifies error.

## 4. Knowledge Integration

- [x] 4.1 Upsert changed docs as `doc`-type `KnowledgeNode`s via the Knowledge Engine store; remove on doc deletion.
- [x] 4.2 Unit tests (fake/real knowledge store): synced doc becomes a `doc` node returned by a `types:[doc]` hybrid search.

## 5. docs:// Resolver & Formatter

- [x] 5.1 Implement the `docs://<store>/<path>` resolver: fetch → cache locally (`.conductor/doc-cache/`) → return local path for the harness loader.
- [x] 5.2 Implement the `## Relevant Documentation` formatter (SPEC §10.5), truncated to budget.
- [x] 5.3 Unit tests: resolver caches and returns a local path; formatter shape + truncation.

## 6. CLI Wiring

- [x] 6.1 Implement `conductor docs sync` and `conductor docs search` in a new `cmd/conductor/cmd/cmd_docs.go`.
- [x] 6.2 **Integration commit (final, append-only):** register `newDocsCommand()` in `cmd/conductor/cmd/root.go`; construct the manager and wire `orchestrator.WithDocStoreSync(...)` in `cmd/conductor/cmd/start.go` `runOrchestrator`; update `AGENTS.md` navigation for `internal/docstore`.

## 7. Verification

- [x] 7.1 `go build ./...` and `go vet ./...` clean; repo linter passes for `internal/docstore`.
- [x] 7.2 `go test ./internal/docstore/...` passes; package coverage ≥ 70% (matching prior phases).
- [x] 7.3 Smoke: a `local_fs` store with two docs — `conductor docs sync` indexes them, `conductor docs search "<query>"` returns a ranked match, and a re-sync with one changed doc downloads only that one.
