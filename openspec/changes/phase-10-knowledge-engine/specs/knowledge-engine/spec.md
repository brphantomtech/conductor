## ADDED Requirements

### Requirement: Codebase Indexing Pipeline

The Knowledge Engine SHALL index workspace repositories into a persistent knowledge graph through
the seven-stage pipeline of SPEC §8.2: discovery, parsing, summarization, embedding, graph
construction, layer assignment, and persistence. Discovery SHALL filter files by
`knowledge.include_patterns` and `knowledge.exclude_patterns` and SHALL compare each file's
checksum against the stored index so unchanged files are skipped. Parsing SHALL use a tree-sitter
AST parser for supported languages when `knowledge.use_ast` is true and a 200-line / 50-line
overlap chunker otherwise. Summaries and embeddings SHALL be cached and regenerated only when a
file's checksum changes.

#### Scenario: Full index of a fresh workspace

- **WHEN** the engine indexes a workspace with no prior index
- **THEN** each non-excluded file produces one or more nodes with a stable ID, a summary, an
  embedding, a `layer_id`, and a `checksum`, and the nodes plus their edges are persisted

#### Scenario: Excluded files are not indexed

- **WHEN** a file matches `knowledge.exclude_patterns`
- **THEN** no node is produced for it

#### Scenario: Unchanged files are skipped on re-index

- **WHEN** the engine re-indexes a workspace and a file's checksum is unchanged
- **THEN** its summary and embedding are reused from cache and no new embedding request is made

### Requirement: Knowledge Node and Edge Model

The engine SHALL represent the graph using KnowledgeNode (SPEC §4.1.6) and KnowledgeEdge (SPEC
§4.1.7). A node's ID SHALL be a stable hash of `<project_id>/<relative_path>[#<symbol_name>]`. Node
types SHALL include `file`, `symbol`, `module`, `layer`, `doc`, and `chunk`. Edge types SHALL
include `imports`, `calls`, `extends`, `implements`, and `references_doc`. The `doc` node type
SHALL exist in the model even though it is populated by a later phase.

#### Scenario: Stable symbol identity across re-index

- **WHEN** the same symbol is indexed twice from an unchanged file
- **THEN** both indexing runs produce the same node ID

#### Scenario: Dependency edges are directed

- **WHEN** file A imports file B
- **THEN** a directed `imports` edge from A's node to B's node is persisted

### Requirement: Persistent Store Backends

The engine SHALL persist nodes and edges through a storage abstraction with a `sqlite_vec` backend
as the default (relational node metadata plus a virtual vector table) and a `qdrant` backend
available behind a build tag. Upserts SHALL be idempotent on node ID.

#### Scenario: Upsert is idempotent

- **WHEN** the same node is upserted twice
- **THEN** the store contains exactly one record for that node ID with the latest content

#### Scenario: Default backend requires no external service

- **WHEN** the engine starts with the default `store_backend`
- **THEN** it persists to the local sqlite_vec store without contacting an external service

### Requirement: Incremental Re-indexing

When `knowledge.watch_for_changes` is true, the engine SHALL watch workspace repository directories
and update the index incrementally: re-parse and re-embed on Write or Create events, delete the
file's nodes and edges on Remove events, and batch events within a 500 ms debounce window.

#### Scenario: Changed file is re-indexed

- **WHEN** a watched file is modified
- **THEN** within the debounce window its nodes are re-parsed, re-embedded, and upserted

#### Scenario: Removed file is purged

- **WHEN** a watched file is deleted
- **THEN** its nodes and edges are removed from the store

### Requirement: Hybrid Search

The engine SHALL support semantic search (embedding similarity), structural search (filter on node
type, layer, language, and path pattern), dependency search (graph traversal for importers and
imports), and hybrid search (semantic candidates re-ranked by structural relevance). Search SHALL
accept the `conductor_knowledge_search` parameters (`query`, `types`, `layers`, `path_pattern`,
`include_dependencies`, `top_k`) and SHALL default `top_k` to 10.

#### Scenario: Semantic query returns ranked nodes

- **WHEN** a natural-language query is issued
- **THEN** the engine returns up to `top_k` nodes ordered by relevance

#### Scenario: Structural filter restricts results

- **WHEN** a query specifies `types` and `layers`
- **THEN** only nodes matching those types and layers are returned

#### Scenario: Dependency expansion includes neighbors

- **WHEN** a query sets `include_dependencies` to true
- **THEN** direct dependency nodes of the matched results are included

### Requirement: Context Injection Formatting

The engine SHALL format the top-k nodes for an issue's title-plus-description seed into a
`## Codebase Context` block as shown in SPEC §8.5, and SHALL truncate the block to respect the
agent's `context_budget`.

#### Scenario: Context block is produced and bounded

- **WHEN** the formatter runs for an issue with a configured `context_budget`
- **THEN** it returns a `## Codebase Context` block listing relevant files and symbols, truncated
  so it does not exceed the budget

### Requirement: Layer Violation Detection

The engine SHALL expose `CheckLayerViolations` returning every dependency edge that crosses layer
boundaries in a prohibited direction. Layer order SHALL be derived from the key order of
`knowledge.layer_definitions` (lower-indexed layers are lower in the stack; higher layers may
import lower layers but not vice versa), and custom `harness_rules` SHALL override the default
direction. Nodes in the `unknown` layer SHALL be excluded from violation reporting.

#### Scenario: Upward dependency is reported

- **WHEN** a node in a lower layer depends on a node in a higher layer
- **THEN** `CheckLayerViolations` reports that edge as a violation

#### Scenario: Allowed downward dependency is not reported

- **WHEN** a node in a higher layer depends on a node in a lower layer
- **THEN** no violation is reported for that edge

### Requirement: Knowledge CLI and Error Classification

The engine SHALL provide `conductor knowledge index` and `conductor knowledge search` subcommands,
SHALL emit a `KnowledgeIndexed` audit event on index completion, and SHALL classify failures using
the SPEC §23.5 sentinels `knowledge_index_failed`, `knowledge_search_failed`, and
`embedding_request_failed`.

#### Scenario: Index command reports completion via audit

- **WHEN** `conductor knowledge index` completes
- **THEN** a `KnowledgeIndexed` audit event is written

#### Scenario: Embedding failure is classified

- **WHEN** an embedding request fails during indexing
- **THEN** the error is classified as `embedding_request_failed` and the index operation surfaces
  `knowledge_index_failed`
