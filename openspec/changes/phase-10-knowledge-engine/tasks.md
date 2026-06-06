# Phase 10 — Atomic Tasks (Knowledge Engine)

Phase 10 goal (per [docs/phases.md](../../../docs/phases.md)): index the codebase semantically +
structurally and serve hybrid RAG. SPEC §8, §4.1.6, §4.1.7, §5.3.8, §23.5. Wave A worktree —
keep all logic in `internal/knowledge`; touch the two integration points only in the final commit
(see [docs/parallel-execution-plan.md](../../../docs/parallel-execution-plan.md)). Each task is
sized for one focused session.

## 1. Domain Model & Errors

- [ ] 1.1 Define `KnowledgeNode` (SPEC §4.1.6) and `KnowledgeEdge` (SPEC §4.1.7) types, node-type and edge-type enums, and the stable ID hash `<project_id>/<relative_path>[#<symbol_name>]`.
- [ ] 1.2 Declare SPEC §23.5 sentinel errors (`knowledge_index_failed`, `knowledge_search_failed`, `embedding_request_failed`) with string values matching the SPEC verbatim.
- [ ] 1.3 Unit tests: ID stability for the same path/symbol; enum round-trip; sentinel string values.

## 2. Storage Abstraction

- [ ] 2.1 Define the `KnowledgeStore` interface (`Upsert`, `DeleteByPath`, `SearchSemantic`, `QueryStructural`, `Neighbors`, `AllForLayerCheck`).
- [ ] 2.2 Implement the `sqlite_vec` backend: relational node/edge tables + virtual vector table keyed by node ID, reusing the Phase 1 `internal/db` connection pattern; idempotent upsert on node ID.
- [ ] 2.3 Add the `qdrant` backend behind a build tag (metadata payload + vector); not built in default CI.
- [ ] 2.4 Unit tests (sqlite_vec): idempotent upsert; delete-by-path removes nodes + edges; structural filter by type/layer/language/path.

## 3. Discovery & Parsing

- [ ] 3.1 Implement discovery: recursive traversal of workspace repos, `include_patterns`/`exclude_patterns` filtering, per-file checksum, skip-unchanged against the stored index.
- [ ] 3.2 Implement the parser registry `parserFor(path)`: tree-sitter AST parser (symbols, imports, references) for the SPEC §8.2 languages when `use_ast`; isolate CGo grammars behind a build consideration.
- [ ] 3.3 Implement the line-chunker fallback (200-line chunks, 50-line overlap) for unsupported languages, config, and docs.
- [ ] 3.4 Unit tests with a small fixture tree: AST symbols + import edges for one language; chunker boundaries/overlap; exclude-pattern respected; checksum skip.

## 4. Summarization & Embedding

- [ ] 4.1 Implement summarization via the `embedding_provider` (SPEC §8.2 Stage 3 prompt), cached by checksum, regenerated only on content change.
- [ ] 4.2 Implement embedding of `summary + "\n" + content_snippet` (truncated to model limit) through `provider.Adapter`; map provider failure → `embedding_request_failed`.
- [ ] 4.3 Unit tests with a fake provider (deterministic vectors): summary/embedding cache hit on unchanged checksum; cache miss + regenerate on change; embedding error classified.

## 5. Graph, Layers & Persistence

- [ ] 5.1 Build directed edges (`imports`, `calls`, `extends`, `implements`) from parsed imports/references.
- [ ] 5.2 Implement layer assignment: match node path against `knowledge.layer_definitions`; unmatched → `unknown`.
- [ ] 5.3 Wire the full pipeline (discovery → … → persistence) into an `Index(ctx, workspace)` entry point; emit `KnowledgeIndexed`; map failure → `knowledge_index_failed`; report `knowledge_index_status`.
- [ ] 5.4 Unit tests: end-to-end index of the fixture tree produces persisted nodes+edges with layers assigned; index emits the audit event.

## 6. Incremental Re-indexing

- [ ] 6.1 Implement the fsnotify watcher over workspace repo roots with a 500 ms debounce (mirror the Phase 2 harness watcher shape).
- [ ] 6.2 On Write/Create: re-parse + re-embed + upsert the affected file; on Remove: `DeleteByPath`.
- [ ] 6.3 Unit tests (injected clock / manual debounce flush): changed file re-indexed; removed file purged; burst of events coalesced within the window.

## 7. Hybrid Search

- [ ] 7.1 Implement semantic search (embedding similarity over node embeddings) returning ranked nodes.
- [ ] 7.2 Implement structural search (filter on type/layer/language/path) and dependency search (graph traversal: importers / imports).
- [ ] 7.3 Implement hybrid ranking: semantic candidates re-ranked by structural relevance; `include_dependencies` expands one hop; `top_k` defaults to 10; accept the full `conductor_knowledge_search` parameter set; map failure → `knowledge_search_failed`.
- [ ] 7.4 Unit tests: semantic ranking order; structural filter restricts results; dependency expansion includes neighbors; top_k bound.

## 8. Context Injection & Layer Violations

- [ ] 8.1 Implement the `## Codebase Context` formatter (SPEC §8.5) over top-k nodes for an issue title+description seed; truncate to `context_budget`.
- [ ] 8.2 Implement `CheckLayerViolations` (SPEC §8.6): direction from `layer_definitions` key order, `harness_rules` override, `unknown` layer excluded.
- [ ] 8.3 Unit tests: formatter shape + budget truncation; upward dependency reported, allowed downward not reported, custom-rule override.

## 9. CLI Wiring

- [ ] 9.1 Implement `conductor knowledge index` and `conductor knowledge search` in a new `cmd/conductor/cmd/cmd_knowledge.go`.
- [ ] 9.2 **Integration commit (final, append-only):** register `newKnowledgeCommand()` in `cmd/conductor/cmd/root.go`; construct and wire the engine in `cmd/conductor/cmd/start.go` `runOrchestrator` (report `knowledge_index_status`); update `AGENTS.md` navigation for `internal/knowledge`.

## 10. Verification

- [ ] 10.1 `go build ./...` and `go vet ./...` clean; repo linter passes for `internal/knowledge`.
- [ ] 10.2 `go test ./internal/knowledge/...` passes; package coverage ≥ 70% (matching prior phases).
- [ ] 10.3 Smoke: `conductor knowledge index` then `conductor knowledge search "<query>"` against a fixture workspace returns ranked results and writes a `KnowledgeIndexed` audit event.
