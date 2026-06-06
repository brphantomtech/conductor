# Phase 9 — Memory Manager

## Why

Stateless agent orchestration has one dominant failure mode: agents repeat the same mistakes
because they carry no memory across sessions. They re-try APIs that were removed, re-make decisions
that were already rejected, and re-learn project conventions every run. The Memory Manager gives
Conductor institutional memory — a three-layer persistent store (episodic, semantic, procedural)
that survives restarts and is retrieved into each turn's prompt. It also completes the context-
budget compaction work started in Phase 3, closing the provider-layer loop on long sessions.

This is a **Wave A** change (see [docs/parallel-execution-plan.md](../../../docs/parallel-execution-plan.md)):
it lands an independent `internal/memory` package plus a self-contained finish to provider
compaction, buildable in its own worktree concurrently with Phases 7, 8, and 10.

Authoritative scope: [docs/phases.md](../../../docs/phases.md) → "Phase 9 — Memory Manager";
SPEC §9 (Memory Manager), §4.1.5 (MemoryEntry), §5.3.9 (memory config), §7.4 (context budget and
compaction), §23.5 (memory errors).

## What Changes

- **New `internal/memory` package** implementing the three layers (SPEC §9.2):
  - **Episodic** — session-scoped facts keyed by `project_id/issue_id`, TTL-bounded
    (`episodic_ttl_days`, default 90).
  - **Semantic** — project-learned facts keyed by `project_id`, no TTL until retired.
  - **Procedural** — "how to do things well" keyed by `project_id/task_type`, no TTL until
    superseded.
- **`MemoryEntry` entity** (SPEC §4.1.5) with `layer`, scoping fields, `content`, optional
  `embedding`, `tags`, `source`, `created_at`, `expires_at`, and retrieval-time `relevance_score`.
- **Storage backend** keyed off `memory.store_backend` / `store_path`, reusing the Phase 1
  `internal/db` pattern; embeddings via `provider.Adapter` for semantic similarity.
- **Memory retrieval** (SPEC §9.3) — episodic (3 most recent for the issue), semantic (top 3 by
  similarity to the turn intent), procedural (1 for the task type), ranked/deduplicated, capped by
  `max_context_memories`, formatted as the `## Relevant Memory` block.
- **Memory writing** (SPEC §9.4) — the three write paths: agent-initiated (backs the
  `conductor_memory_write` tool wired in Phase 13), auto-extraction (`MEMORY:` lines and
  `<memory type="...">` blocks from agent output; validation failures → episodic), and
  post-session consolidation input.
- **TTL enforcement** on episodic entries and a **consolidation worker** (SPEC §9.5) running every
  `consolidation_interval_hours`: cluster episodic by similarity, synthesize clusters of 3+ via the
  `consolidation_provider` into semantic memories, synthesize procedural memories from successful
  turn sequences by task type, and delete expired episodic entries.
- **Reconciliation Part C implementation** — supply the `MemoryPostProcessor` seam the orchestrator
  already exposes (`WithMemoryPostProcessor`), writing a session-end episodic memory per terminal
  run.
- **Context-budget compaction** (SPEC §7.4) — finish the Phase 3 work: at 95% of `context_budget`
  apply `summarize` (provider summarization → restart context, `ContextCompacted` event) or
  `sliding_window` (drop oldest pairs, `ContextSlid` event), targeting ~60% usage.
- **`conductor memory list / retire / consolidate` subcommands** in their own `cmd_memory.go`.
- **Audit events** (already declared): `MemoryRead`, `MemoryWritten`, `MemoryConsolidated`,
  `ContextCompacted`. **Error classification** — SPEC §23.5 `memory_read_failed`,
  `memory_write_failed`.
- **Unit tests** with an injected clock and fake provider: layer scoping + TTL, retrieval ranking
  and dedup, the formatter, each write path, the consolidation worker (cluster→synthesize→promote),
  and both compaction strategies. No live API calls in CI.

## Capabilities

### New Capabilities

- `memory-manager`: the three-layer store, MemoryEntry model, retrieval + `## Relevant Memory`
  formatter, the three write paths, TTL enforcement, the consolidation worker, the reconciliation
  Part C post-processor, the `memory` subcommands, and SPEC §23.5 memory error classification.

### Modified Capabilities

- `provider-adapter-layer`: context-budget compaction (`summarize`, `sliding_window`) is completed
  per SPEC §7.4, finishing the accounting/warning groundwork laid in Phase 3. (Delta in
  `specs/provider-adapter-layer/spec.md`.)

## Impact

- **Affected specs**: new capability `memory-manager`; modified `provider-adapter-layer`
  (compaction).
- **Affected code**: `internal/memory/` (new). `internal/provider/` (compaction completion). New
  `cmd/conductor/cmd/cmd_memory.go`.
- **Integration points (last commit only, append-only)** per the Wave A integration-commit rule:
  - `cmd/conductor/cmd/root.go` — `root.AddCommand(newMemoryCommand())`.
  - `cmd/conductor/cmd/start.go` `runOrchestrator` — construct the manager and wire it via
    `orchestrator.WithMemoryPostProcessor(...)`.
- **Consumes (unchanged)**: `internal/config` (`Memory`, `ProviderConfig`), `internal/provider`
  (embedding + consolidation/summarization calls), `internal/audit`, `internal/db`,
  `internal/orchestrator` (the `MemoryPostProcessor` seam + `RunAttempt`).

### Non-goals

- The `conductor_memory_read` / `conductor_memory_write` agent tools (Phase 13) — this phase ships
  the store + CLI, not in-session tool injection.
- Wiring the `## Relevant Memory` block into live turn prompt assembly (Phase 7/13) — the formatter
  ships and is unit-tested; the prompt-assembly insertion is a later seam.
- Embedding-similarity clustering quality tuning beyond the SPEC §9.5 algorithm.
- The Knowledge Engine (Phase 10) — memory embeddings are independent of the codebase graph.
