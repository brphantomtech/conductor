## Context

The Memory Manager depends only on substrate already on `main`: `config.Memory` (every field
already declared in [internal/config/types.go](../../../internal/config/types.go)),
`provider.Adapter` (embeddings + consolidation/summarization calls), `audit.Writer`,
`internal/db`, and the orchestrator's `MemoryPostProcessor` seam plus `RunAttempt` type (both
already present from Phase 6 — `WithMemoryPostProcessor` exists in
[internal/orchestrator/orchestrator.go](../../../internal/orchestrator/orchestrator.go) and the
no-op default in [seams.go](../../../internal/orchestrator/seams.go)). Nothing in the poll loop
changes; this phase supplies an implementation of an existing seam.

SPEC anchors: §9.2 (three layers + scopes + TTL), §9.3 (retrieval order and `## Relevant Memory`
format), §9.4 (three write paths + auto-extraction patterns), §9.5 (consolidation algorithm),
§4.1.5 (MemoryEntry), §7.4 (compaction), §23.5 (errors). Conventions to match (Phases 2–6):
constructor + functional `Option`s, injected clock for deterministic TTL/consolidation tests,
sentinel errors equal to the SPEC strings, fake provider with recorded behavior, audit emission via
a small helper.

This is a Wave A worktree. All memory logic stays in `internal/memory`; the compaction completion
is contained within `internal/provider`; the only shared-file edits are the two append-only
integration points done in the final commit.

## Goals / Non-Goals

**Goals:**

- A correct three-layer store with proper scoping and TTL semantics.
- Retrieval that returns the SPEC §9.3 mix (3 episodic + 3 semantic + 1 procedural), ranked,
  deduplicated, capped by `max_context_memories`, and formatted.
- The three write paths and the consolidation worker, all deterministic under an injected clock and
  a fake provider.
- A `MemoryPostProcessor` implementation wired into reconciliation Part C.
- Completed provider compaction (`summarize` + `sliding_window`) per SPEC §7.4.

**Non-Goals:**

- Agent tools (`conductor_memory_read/write`) — Phase 13. Live prompt wiring — Phase 7/13.
- Knowledge graph integration — Phase 10.

## Decisions

### Store behind a `MemoryStore` interface, SQLite default

A `MemoryStore` interface (`Put`, `Query`, `Retire`, `DeleteExpired`, layer-scoped getters) with a
SQLite implementation reusing the Phase 1 `internal/db` pattern; embeddings stored as a blob column
with brute-force cosine for similarity (memory volume is far smaller than the codebase graph).

- **Why over reusing the Knowledge sqlite_vec store:** memory is a separate concern with different
  scoping (project/issue/task_type) and lifecycle (TTL, retire, consolidate). Keeping it in its own
  package and store avoids a cross-phase dependency on Phase 10 and keeps Wave A truly parallel. A
  brute-force cosine over a small memory set is simpler than a vec extension and has no native dep.

### Injected clock for TTL and consolidation cadence

The manager takes `clock func() time.Time`. `expires_at = created_at + episodic_ttl_days` is
computed from the clock; the consolidation worker's interval is driven by a ticker re-reading
`consolidation_interval_hours`, manually drivable in tests.

- **Why:** TTL expiry and "runs every N hours" are time-dependent; tests must assert expiry and
  cadence exactly. Matches the Phase 2 watcher / Phase 6 orchestrator test style.

### Retrieval ranking is layer-quota then relevance

Retrieval pulls the SPEC §9.3 per-layer quotas (episodic 3 by recency, semantic 3 by similarity,
procedural 1), merges, dedupes by content hash, ranks by `relevance_score`, then truncates to
`max_context_memories`. `relevance_score` is computed at retrieval time and never persisted (SPEC
§4.1.5).

- **Why:** SPEC §9.3 specifies both the per-layer limits and a final cap; applying quotas first
  preserves layer diversity before the global cap, matching the formatted example's three sections.

### Write paths share one `Write` core; auto-extraction is a parser

Agent-initiated, auto-extracted, and consolidation writes all funnel through one validated `Write`.
Auto-extraction is a pure function over agent output text: `MEMORY:` lines → semantic,
`<memory type="...">…</memory>` blocks → typed, validation failures → episodic (SPEC §9.4).

- **Why:** one write path keeps `source` tagging and audit emission consistent; a pure extractor is
  trivially unit-testable and reused by both the post-turn hook and the Phase 13 tool later.

### Reconciliation Part C = a thin `MemoryPostProcessor`

The post-processor implements the existing orchestrator seam: on every terminal `RunAttempt`, write
a session-end episodic memory summarizing the attempt outcome.

- **Why:** the call site already exists (Phase 6 built the seam precisely so Phase 9 only supplies
  the implementation); this is a wiring change, not a control-flow change.

### Compaction completes inside the provider session

At 95% of `context_budget`, `summarize` invokes the provider with the SPEC §7.4 summarization
prompt and restarts the session context with the summary as a system message (`ContextCompacted`);
`sliding_window` drops the oldest message pairs keeping the system message and last K turns
(`ContextSlid`). Both target ~60% usage.

- **Why:** Phase 3 already tracks cumulative tokens and emits the 80% warning; SPEC §7.4 places the
  actual compaction here. Keeping it in the provider session avoids leaking token bookkeeping into
  the memory package — memory and compaction are delivered together by this phase but live in their
  natural homes.

## Risks / Trade-offs

- **[Embedding nondeterminism / cost in tests]** → Fake provider returns fixed vectors; no live
  calls in CI. Real call volume is bounded by retrieval limits and the consolidation interval.
- **[Consolidation deleting useful episodic memory]** → Deletion is strictly TTL-driven (SPEC §9.5
  step 5); synthesis promotes before expiry. Tests assert that pre-TTL entries survive and only
  expired ones are deleted.
- **[Unbounded memory growth]** → Episodic is TTL-bounded; semantic/procedural grow only via
  consolidation (clusters of 3+) and are retireable via `conductor memory retire`. `list` surfaces
  counts per layer.
- **[Auto-extraction false positives]** → Patterns are explicit (`MEMORY:` prefix, typed XML
  blocks) per SPEC §9.4; extraction is conservative and tagged `auto_extracted` so it is
  distinguishable and retireable.
- **[Compaction data loss on summarize]** → `summarize` preserves the summary as a system message
  and targets 60% to leave headroom; `ContextCompacted` records the event for provenance. Strategy
  is per-`ProviderConfig`, defaulting to `summarize` (SPEC §4.1.3).

## Migration Plan

Additive plus one contained modification. New `internal/memory` package, one new CLI file, and the
compaction completion inside `internal/provider` (extends Phase 3 code paths; no interface break).
The two integration-point edits (`root.go` AddCommand, `start.go`
`WithMemoryPostProcessor`) land in the final commit per the Wave A integration-commit rule. New
persisted state lives in the memory store's own tables. Rollback is reverting the package, the
provider compaction commit, and the two wiring lines. `memory.enabled = false` (config default)
leaves behavior unchanged; with memory disabled the post-processor is a no-op.

## Open Questions

- **Brute-force cosine vs. a vector index for semantic memory** — start brute-force (small N);
  revisit only if memory volume per project grows beyond practical scan size.
- **Auto-extraction wiring point** — the extractor ships now; whether it runs in the orchestrator's
  after-turn path or only via the Phase 13 tool is confirmed when the turn loop gains a memory hook
  (Phase 7/13). This phase delivers and tests the pure extractor either way.
- **Procedural synthesis trigger** — SPEC §9.5 groups "successful turn sequences"; the exact
  success signal (validation pass vs. terminal active-exit) is settled against the Phase 8 result
  shape during implementation, defaulting to terminal active-exit which exists today.
