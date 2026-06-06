# Phase 9 — Atomic Tasks (Memory Manager)

Phase 9 goal (per [docs/phases.md](../../../docs/phases.md)): three-layer persistent memory with
cross-session retrieval, plus completion of provider context-budget compaction. SPEC §9, §4.1.5,
§5.3.9, §7.4, §23.5. Wave A worktree — keep memory logic in `internal/memory`, keep compaction in
`internal/provider`, and touch the two integration points only in the final commit (see
[docs/parallel-execution-plan.md](../../../docs/parallel-execution-plan.md)). Each task is sized for
one focused session.

## 1. Domain Model & Errors

- [ ] 1.1 Define `MemoryEntry` (SPEC §4.1.5) with `layer`/`source` enums, scope fields, `content`, optional `embedding`, `tags`, `created_at`, `expires_at`, and a retrieval-time `relevance_score` (not persisted).
- [ ] 1.2 Declare SPEC §23.5 sentinels `memory_read_failed`, `memory_write_failed` with verbatim string values.
- [ ] 1.3 Unit tests: enum round-trip; `relevance_score` excluded from persistence; sentinel strings.

## 2. Store Abstraction

- [ ] 2.1 Define the `MemoryStore` interface (`Put`, `Query`, `Retire`, `DeleteExpired`, layer-scoped getters).
- [ ] 2.2 Implement the SQLite backend reusing the Phase 1 `internal/db` pattern; embeddings as a blob column with brute-force cosine similarity.
- [ ] 2.3 Unit tests: scoped put/query per layer; retire removes from retrieval; restart reconstructs entries with layer + scope intact.

## 3. TTL & Clock

- [ ] 3.1 Wire an injected `clock`; compute episodic `expires_at = created_at + episodic_ttl_days`; semantic/procedural `expires_at = null`.
- [ ] 3.2 Implement `DeleteExpired` driven by the clock.
- [ ] 3.3 Unit tests (injected clock): episodic TTL set correctly; expired deleted, non-expired retained; semantic/procedural never expire.

## 4. Retrieval & Formatting

- [ ] 4.1 Implement retrieval: 3 recent episodic (by recency), 3 semantic (by similarity to turn intent, via fake-able embedding), 1 procedural (by task_type).
- [ ] 4.2 Merge, dedupe by content hash, rank by `relevance_score`, cap by `max_context_memories`; emit `MemoryRead`; map failure → `memory_read_failed`.
- [ ] 4.3 Implement the `## Relevant Memory` formatter (SPEC §9.3 section layout).
- [ ] 4.4 Unit tests: per-layer quotas + global cap; dedup; formatter section shape.

## 5. Writing & Auto-Extraction

- [ ] 5.1 Implement a single validated `Write` core that tags `source` and emits `MemoryWritten`; map failure → `memory_write_failed`.
- [ ] 5.2 Implement the pure auto-extractor: `MEMORY:` lines → semantic; `<memory type="...">…</memory>` blocks → typed layer; validation failures → episodic.
- [ ] 5.3 Unit tests: each write path tags the right `source`; extractor parses each pattern; validation failure → issue-scoped episodic.

## 6. Consolidation Worker

- [ ] 6.1 Implement the worker (ticker re-reading `consolidation_interval_hours`, manually drivable): cluster episodic by embedding similarity.
- [ ] 6.2 Synthesize clusters of 3+ via the `consolidation_provider` into semantic entries; synthesize procedural memories from successful turn sequences by task_type; delete expired episodic; emit `MemoryConsolidated`.
- [ ] 6.3 Unit tests (fake provider + clock): cluster of 3 synthesized to semantic; procedural synthesized by task_type; expired deleted; event emitted.

## 7. Reconciliation Part C

- [ ] 7.1 Implement a `MemoryPostProcessor` writing a session-end episodic memory per terminal `RunAttempt`.
- [ ] 7.2 Unit tests: terminal attempt writes an issue-scoped episodic entry; disabled memory → no-op.

## 8. Provider Compaction (SPEC §7.4)

- [ ] 8.1 In `internal/provider`, implement `summarize`: at 95% of `context_budget`, invoke the summarization prompt, restart context with the summary system message, emit `ContextCompacted`, target ~60% usage.
- [ ] 8.2 Implement `sliding_window`: drop oldest message pairs keeping system + last K turns, emit `ContextSlid`; implement `none`: emit `ContextLimitApproaching` and continue.
- [ ] 8.3 Unit tests (recorded fixtures): summarize restarts context + emits event; sliding window drops oldest; zero budget → no compaction.

## 9. CLI Wiring

- [ ] 9.1 Implement `conductor memory list / retire / consolidate` in a new `cmd/conductor/cmd/cmd_memory.go`.
- [ ] 9.2 **Integration commit (final, append-only):** register `newMemoryCommand()` in `cmd/conductor/cmd/root.go`; construct the manager and wire `orchestrator.WithMemoryPostProcessor(...)` in `cmd/conductor/cmd/start.go` `runOrchestrator`; update `AGENTS.md` navigation for `internal/memory`.

## 10. Verification

- [ ] 10.1 `go build ./...` and `go vet ./...` clean; repo linter passes for `internal/memory` and the `internal/provider` compaction additions.
- [ ] 10.2 `go test ./internal/memory/... ./internal/provider/...` passes; `internal/memory` coverage ≥ 70%.
- [ ] 10.3 Smoke: write entries across layers, `conductor memory list` shows per-layer counts, `conductor memory consolidate` promotes a cluster and writes `MemoryConsolidated`.
