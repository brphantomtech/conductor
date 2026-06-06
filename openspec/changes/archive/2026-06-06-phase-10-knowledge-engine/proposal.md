# Phase 10 — Knowledge Engine

## Why

Conductor dispatches agents (Phase 6) but gives them no awareness of the codebase they are
editing. Every turn starts blind: the agent must rediscover structure, dependencies, and
conventions from scratch, which is exactly the failure mode Harness Engineering warns against.
The Knowledge Engine supplies a live, queryable model of the codebase — semantic + structural —
so each turn can be seeded with the files and symbols relevant to the issue. It is also the
substrate two later phases depend on: the Harness Enforcer (Phase 12) reads layer violations from
it, and the Doc Store Manager (Phase 11) stores Doc nodes alongside file/symbol nodes.

This is a **Wave A** change (see [docs/parallel-execution-plan.md](../../../docs/parallel-execution-plan.md)):
it lands an independent `internal/knowledge` package and is buildable in its own worktree
concurrently with Phases 7, 8, and 9.

Authoritative scope: [docs/phases.md](../../../docs/phases.md) → "Phase 10 — Knowledge Engine";
SPEC §8 (Knowledge Engine), §4.1.6 (KnowledgeNode), §4.1.7 (KnowledgeEdge), §5.3.8 (knowledge
config), §23.5 (knowledge errors).

## What Changes

- **New `internal/knowledge` package** implementing the seven-stage indexing pipeline (SPEC §8.2):
  - **Discovery** — recursive traversal of workspace repos filtered by `include_patterns` /
    `exclude_patterns`; checksum comparison against the stored index for incremental updates.
  - **Parsing** — tree-sitter AST extraction (symbols, imports, dependency edges) when
    `knowledge.use_ast == true` for Go/TS/JS/Python/Rust/C/C++/Java/Ruby/Elixir; a 200-line /
    50-overlap line chunker fallback for everything else.
  - **Summarization** — 1–3 sentence summary per unit via the configured `embedding_provider`,
    cached and only regenerated when content changes.
  - **Embedding** — embed `summary + "\n" + content_snippet` (truncated to the model limit).
  - **Graph construction** — directed edges (`imports`, `calls`, `extends`, `implements`).
  - **Layer assignment** — match node path against `knowledge.layer_definitions`; unmatched →
    `unknown` layer.
  - **Persistence** — upsert nodes + edges into the configured `store_backend`.
- **`KnowledgeNode` / `KnowledgeEdge` entities** (SPEC §4.1.6, §4.1.7) with stable hash IDs.
- **Storage backends** — `sqlite_vec` default (relational node metadata + virtual vec table);
  `qdrant` behind a build tag (metadata payload + vector).
- **Incremental re-indexing** (SPEC §8.3) — fsnotify watcher over workspace repos: re-parse on
  Write/Create, delete nodes on Remove, batched within a 500 ms debounce window.
- **Hybrid search** (SPEC §8.4) — semantic (embedding similarity), structural (filter on
  type/layer/language/path), dependency (graph traversal), and hybrid (semantic re-ranked by
  structural relevance). Backs the `conductor_knowledge_search` schema (the tool itself is wired
  in Phase 13).
- **Context injection formatter** (SPEC §8.5) — `## Codebase Context` block from the top-k nodes
  for an issue's title+description seed, truncated to the agent `context_budget`.
- **`CheckLayerViolations`** (SPEC §8.6) — returns layer-direction violations for the Harness
  Enforcer (Phase 12) to consume.
- **`KnowledgeIndexed` audit event** (already declared in `internal/audit`) emitted on index
  completion; `knowledge_index_status` reporting for the orchestrator runtime state.
- **`conductor knowledge index` / `conductor knowledge search` subcommands** in their own
  `cmd_knowledge.go` file.
- **Error classification** — SPEC §23.5 sentinels `knowledge_index_failed`,
  `knowledge_search_failed`, `embedding_request_failed`.
- **Unit tests** with recorded fixtures: parser on a small multi-language sample tree, incremental
  re-index on a changed/removed file, each hybrid query type, layer-violation detection, and the
  context-injection formatter. No live embedding API calls in CI.

## Capabilities

### New Capabilities

- `knowledge-engine`: the indexing pipeline, KnowledgeNode/KnowledgeEdge model, sqlite_vec (and
  optional qdrant) persistence, incremental re-indexing, hybrid search, context-injection
  formatter, layer-violation detection, the `knowledge` subcommands, and SPEC §23.5 knowledge
  error classification.

### Modified Capabilities

None. The orchestrator, provider, and config layers are consumed unchanged.

## Impact

- **Affected specs**: new capability `knowledge-engine` (delta in
  `specs/knowledge-engine/spec.md`).
- **Affected code**: `internal/knowledge/` (new). New `cmd/conductor/cmd/cmd_knowledge.go`.
- **Integration points (last commit only, append-only)** per the Wave A integration-commit rule:
  - `cmd/conductor/cmd/root.go` — `root.AddCommand(newKnowledgeCommand())`.
  - `cmd/conductor/cmd/start.go` `runOrchestrator` — construct the engine and wire it (the
    orchestrator already exposes the runtime-state status field; the Phase 13 tool and the
    Phase 6 poll loop seam consume it later).
- **Consumes (unchanged)**: `internal/config` (`Knowledge`, `ProviderConfig`), `internal/provider`
  (embedding + summarization calls), `internal/audit`, `internal/db`, `internal/workspace` (repo
  paths).

### Non-goals

- The `conductor_knowledge_search` tool surface for agents (Phase 13) — this phase ships the
  search engine + CLI, not the in-session tool injection.
- Doc nodes from external Doc Stores (Phase 11) — the `doc` node type exists in the model but is
  populated by the Doc Store Manager.
- Harness Enforcer rule evaluation and GC (Phase 12) — this phase exposes `CheckLayerViolations`;
  the enforcer consumes it later.
- Wiring Knowledge context into the live turn prompt assembly (Phase 7 router / Phase 13) — the
  formatter is delivered and unit-tested but the poll-loop/turn wiring is a later seam.
- The HTTP/WebSocket Knowledge Graph view (Phase 14/15).
