# memory-manager Specification

## Purpose
TBD - created by archiving change phase-9-memory-manager. Update Purpose after archive.
## Requirements
### Requirement: Three-Layer Memory Store

The Memory Manager SHALL maintain three persistent layers (SPEC §9.2): episodic (scoped to
`project_id/issue_id`, TTL-bounded by `memory.episodic_ttl_days`, default 90), semantic (scoped to
`project_id`, no TTL until retired), and procedural (scoped to `project_id/task_type`, no TTL until
superseded). Each entry SHALL be a MemoryEntry (SPEC §4.1.5) carrying `id`, `layer`, scope fields,
`content`, optional `embedding`, `tags`, `source`, `created_at`, and `expires_at`. Storage SHALL
survive service restarts.

#### Scenario: Episodic entry is TTL-bounded

- **WHEN** an episodic entry is written
- **THEN** its `expires_at` equals `created_at` plus `episodic_ttl_days`

#### Scenario: Semantic and procedural entries have no TTL

- **WHEN** a semantic or procedural entry is written
- **THEN** its `expires_at` is null

#### Scenario: Entries survive restart

- **WHEN** an entry is written and the manager is reconstructed against the same store
- **THEN** the entry is retrievable with its layer and scope intact

### Requirement: Memory Retrieval and Formatting

Before a turn, the manager SHALL retrieve the SPEC §9.3 mix — the 3 most recent episodic entries
for the issue, the top 3 semantic entries by embedding similarity to the turn intent, and 1
procedural entry for the task type — then rank by relevance, deduplicate, apply the
`memory.max_context_memories` cap, and format the result as the `## Relevant Memory` block. The
`relevance_score` SHALL be computed at retrieval time and never persisted.

#### Scenario: Retrieval respects per-layer quotas and the global cap

- **WHEN** retrieval runs for an issue with more than the per-layer limits available
- **THEN** at most 3 episodic, 3 semantic, and 1 procedural entry are selected, then the merged set
  is truncated to `max_context_memories`

#### Scenario: Formatted block matches the section layout

- **WHEN** retrieval returns entries across layers
- **THEN** the formatter emits a `## Relevant Memory` block with the episodic, project-knowledge,
  and procedural sections

### Requirement: Memory Writing and Auto-Extraction

The manager SHALL support the three write paths of SPEC §9.4: agent-initiated writes, auto-extraction
from agent output, and consolidation writes. Auto-extraction SHALL save lines beginning with
`MEMORY:` as semantic memory, parse `<memory type="...">...</memory>` blocks into the named layer,
and save validation failures as episodic memory. Every write SHALL set the entry's `source` and emit
a `MemoryWritten` audit event.

#### Scenario: MEMORY-prefixed line becomes semantic memory

- **WHEN** agent output contains a line beginning with `MEMORY:`
- **THEN** a semantic entry is written with `source = auto_extracted`

#### Scenario: Validation failure becomes episodic memory

- **WHEN** a validation failure is recorded for an issue
- **THEN** an episodic entry scoped to that issue is written with `source = validation_result`

### Requirement: TTL Enforcement and Consolidation

The manager SHALL run a consolidation worker every `memory.consolidation_interval_hours` (SPEC
§9.5): cluster episodic memories for the project by embedding similarity, synthesize each cluster of
3+ members into a semantic entry via the `consolidation_provider`, synthesize procedural memories
from successful turn sequences by task type, and delete episodic entries past `episodic_ttl_days`. A
`MemoryConsolidated` audit event SHALL be emitted per consolidation run.

#### Scenario: A cluster of three is synthesized into semantic memory

- **WHEN** consolidation finds an episodic cluster with at least three members
- **THEN** a semantic entry is written from the synthesis and a `MemoryConsolidated` event is emitted

#### Scenario: Expired episodic entries are deleted

- **WHEN** consolidation runs and episodic entries have passed their `expires_at`
- **THEN** those entries are deleted while non-expired entries are retained

### Requirement: Session-End Post-Processing

The manager SHALL implement the orchestrator's memory post-processor seam (reconciliation Part C,
SPEC §13.5), writing a session-end episodic memory for each terminal run attempt.

#### Scenario: Terminal run writes a session-end memory

- **WHEN** a run attempt reaches a terminal outcome and the post-processor is wired
- **THEN** an episodic memory scoped to the issue is written summarizing the attempt outcome

### Requirement: Memory CLI and Error Classification

The manager SHALL provide `conductor memory list`, `conductor memory retire`, and
`conductor memory consolidate` subcommands, and SHALL classify failures using the SPEC §23.5
sentinels `memory_read_failed` and `memory_write_failed`.

#### Scenario: Retire removes a semantic entry

- **WHEN** `conductor memory retire <id>` targets a semantic entry
- **THEN** the entry is no longer returned by retrieval

#### Scenario: Write failure is classified

- **WHEN** a memory write fails at the store boundary
- **THEN** the error is classified as `memory_write_failed`

