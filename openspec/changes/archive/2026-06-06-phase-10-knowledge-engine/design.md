## Context

The Knowledge Engine is the first of the "context engine" phases. It depends only on substrate
already on `main`: `config.Knowledge` (every field already declared in
[internal/config/types.go](../../../internal/config/types.go)), `provider.Adapter` (for
summarization + embedding calls), `audit.Writer`, `internal/db`, and the workspace repo paths.
Nothing in the orchestrator poll loop has to change for this phase to be useful — the engine is a
standalone background service plus a CLI, exactly as SPEC §8.1 frames it.

SPEC §8 is the contract: §8.2 (seven-stage pipeline), §8.3 (incremental re-index, 500 ms
debounce), §8.4 (four query types), §8.5 (context block format), §8.6 (layer violations), plus
§4.1.6/§4.1.7 (node/edge model) and §23.5 (errors). The codebase conventions to match (Phases
2–6): constructor + functional `Option`s, injected clock for deterministic tests, sentinel errors
whose string values equal the SPEC verbatim, recorded fixtures instead of live API calls, and a
storage abstraction behind an interface so the backend is swappable.

This is a Wave A worktree. The design deliberately keeps all logic inside `internal/knowledge` so
the only shared-file edits are the two append-only integration points, done in a single final
commit.

## Goals / Non-Goals

**Goals:**

- A correct, incremental indexing pipeline producing a persisted node/edge graph from a workspace.
- Hybrid search returning ranked nodes for a natural-language seed, plus structural and dependency
  queries.
- A `KnowledgeStore` interface with a `sqlite_vec` implementation; `qdrant` behind a build tag.
- Deterministic tests: parser + chunker over a fixture tree, incremental re-index, each query
  type, layer-violation detection, formatter — all with a fake provider (no live embeddings).
- `CheckLayerViolations` ready for Phase 12 to consume.

**Non-Goals:**

- In-session agent tool (`conductor_knowledge_search`) — Phase 13.
- Doc nodes — Phase 11. Live turn-prompt wiring — Phase 7/13. Graph UI — Phase 14/15.

## Decisions

### Storage behind a `KnowledgeStore` interface, sqlite_vec default

Node metadata lives in a relational table; embeddings in a sqlite-vec virtual table keyed by node
ID. A `KnowledgeStore` interface (`Upsert`, `DeleteByPath`, `SearchSemantic`, `QueryStructural`,
`Neighbors`, `AllForLayerCheck`) hides the backend.

- **Why:** SPEC §8.2 Stage 7 names both `sqlite_vec` and `qdrant`. An interface keeps the pipeline
  backend-agnostic and lets `qdrant` sit behind a build tag (no CGo/daemon dependency in default
  CI). sqlite_vec reuses the Phase 1 `internal/db` connection pattern.

### Parser registry keyed by language, AST or chunker

A `parserFor(path) parser` registry returns a tree-sitter-backed parser for the ten supported
languages when `use_ast`, else the 200/50 line chunker. Each parser returns `[]parsedUnit`
(symbol or chunk) plus raw import/reference edges.

- **Why:** SPEC §8.2 Stage 2 enumerates the languages and the fallback. A registry isolates the
  CGo tree-sitter grammars so the chunker path (and most unit tests) need no native deps. Symbol
  IDs follow SPEC §4.1.6: `hash(<project_id>/<relative_path>[#<symbol_name>])`.

### Summary + embedding caching keyed by checksum

Each unit's summary and embedding are cached against the file checksum (SPEC §4.1.6 `checksum`).
Re-index regenerates only when the checksum changes.

- **Why:** SPEC §8.2 Stage 3 ("only regenerated when file content changes") and §8.3. This is what
  makes incremental re-index cheap and keeps embedding-call volume (and test fixture count) low.

### Provider calls go through the existing `provider.Adapter`

Summarization uses the `embedding_provider` config as a normal turn; embeddings use the adapter's
embedding path. Tests inject a fake provider returning deterministic vectors.

- **Why:** reuses Phase 3 infrastructure and its recorded-fixture testing style; no second HTTP
  client. `embedding_request_failed` (SPEC §23.5) maps provider errors at this boundary.

### Incremental watcher mirrors the Phase 2 harness watcher

An fsnotify watcher over workspace repo roots, debounced 500 ms, calling the same
parse→embed→upsert path for Write/Create and `DeleteByPath` for Remove.

- **Why:** SPEC §8.3 verbatim, and the debounce-watcher shape is already proven in
  `internal/harness`. Reuse the pattern rather than invent a new one.

### Hybrid ranking = semantic recall re-ranked by structural signal

Hybrid search runs the semantic query for candidates, then re-ranks by structural relevance
(layer match, path proximity, node type weight) and optionally expands one dependency hop when
`include_dependencies`.

- **Why:** SPEC §8.4 defines hybrid as "semantic results re-ranked by structural relevance"; doing
  the re-rank in Go over a candidate set keeps it backend-independent.

## Risks / Trade-offs

- **[tree-sitter CGo build/test burden]** → The AST path is isolated behind the parser registry and
  a build consideration; the chunker fallback and all non-parser tests run without native grammars.
  AST parsers are exercised by a small fixture per language, not the whole matrix.
- **[Embedding cost / nondeterminism in tests]** → A fake provider returns fixed vectors; no live
  calls in CI. Caching keyed by checksum bounds real-world call volume.
- **[Large repos / index time]** → Discovery streams files and respects `exclude_patterns`;
  indexing is incremental by checksum so only changed files are reprocessed. `knowledge index`
  reports progress; `index_on_startup` is opt-in via config.
- **[sqlite-vec availability on Windows]** → Default backend is the same SQLite driver Phase 1 uses
  with the vec extension; if unavailable, the engine degrades to structural-only search and logs a
  warning rather than failing the binary. (Confirm during implementation; fall back documented.)
- **[Layer direction ambiguity]** → SPEC §8.6 derives direction from `layer_definitions` key order
  (lower index = lower layer); custom `harness_rules` override. Implemented exactly as specified;
  unmatched nodes land in `unknown` and are excluded from violation checks.

## Migration Plan

Additive only. New `internal/knowledge` package and one new CLI file. The two integration-point
edits (`root.go` AddCommand, `start.go` engine construction) land in the final commit per the Wave
A integration-commit rule, keeping the merge surface to append-only lines. New persisted state
lives in its own knowledge store file/tables (no change to existing schemas). Rollback is reverting
the package + the two wiring lines. `knowledge.enabled = false` (config default) leaves the binary
behaving exactly as before this phase.

## Open Questions

- **sqlite-vec vs. a pure-Go vector fallback** for environments without the extension — start with
  sqlite-vec; add a brute-force cosine fallback only if a target platform lacks it.
- **Where context injection is wired into the turn** — formatter ships now; the actual prompt-
  assembly insertion (SPEC §16.1 step 4) is owned by Phase 7/13 and deferred.
- **Embedding model dimensionality config** — assume the `embedding_model` fixes the dimension;
  revisit if multiple models must coexist in one store.
