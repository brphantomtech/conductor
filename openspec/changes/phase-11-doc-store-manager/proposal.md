# Phase 11 — Doc Store Manager

## Why

Project documentation — specs, ADRs, design docs, API contracts — is often more useful, more
shareable, and better access-controlled when it lives outside the code repository. Coupling it to
the repo creates friction. The Doc Store Manager lets docs live in a local directory, a separate
Git repo, or S3, syncs them on a schedule, and feeds them into the same hybrid RAG mechanism the
Knowledge Engine (Phase 10) already provides — so agents get `## Relevant Documentation` in their
prompts. It also resolves `docs://` HARNESS.md references.

This is a **Wave B** change (see [docs/parallel-execution-plan.md](../../../docs/parallel-execution-plan.md)):
it depends only on the Knowledge Engine (now on `main`) and lands an independent `internal/docstore`
package, buildable in its own worktree concurrently with Phases 12, 16, and 17.

Authoritative scope: [docs/phases.md](../../../docs/phases.md) → "Phase 11 — Doc Store Manager";
SPEC §10 (Doc Store Manager), §4.1.9 (DocRef), §5.3.10 (docs config), §20.4 (DocStoreBackend),
§23.5 (`doc_store_sync_failed`).

## What Changes

- **New `internal/docstore` package** with the `DocStoreBackend` interface (SPEC §20.4):
  `Sync(ctx) ([]DocRef, error)`, `Fetch(ctx, ref) (string, error)`, `List(ctx, filter) ([]DocRef, error)`.
- **Three backends** (SPEC §10.2): `local_fs` (directory), `git_repo` (clone/pull, token or SSH),
  `s3` (object storage). Notion / Confluence are deferred (noted as non-goals); `custom` is a plugin
  seam left for Phase 18.
- **`DocRef` entity** (SPEC §4.1.9): id, title, store_id, path_or_id, content_hash, tags, optional
  embedding, last_synced_at.
- **Sync scheduler** (SPEC §10.3): initial sync on startup (unless recent), interval-driven
  resync, checksum comparison to download only changed docs, last-good retention on failure,
  `DocStoreSynced` audit event per store.
- **Knowledge Engine integration**: changed docs are indexed as `doc` nodes alongside file/symbol
  nodes (the `doc` node type already exists in the Phase 10 model), so `conductor_doc_search`
  (Phase 13) and context injection reuse the existing hybrid search.
- **`docs://` URI handler** for HARNESS.md resolution: `--harness docs://specs/HARNESS.md` resolves
  to the configured store, syncs + caches locally before parsing.
- **`## Relevant Documentation` context formatter** (SPEC §10.5) for prompt injection.
- **`DocStoreSync` seam implementation** — supplies the orchestrator's existing `DocStoreSync`
  poll-loop seam (`WithDocStoreSync`) so step 4 of the tick syncs pending stores.
- **`conductor docs sync` / `conductor docs search` subcommands** in their own `cmd_docs.go`.
- **Error classification** — SPEC §23.5 `doc_store_sync_failed`.
- **Unit tests** with fixtures/fakes: `local_fs` sync + checksum change detection, `git_repo` and
  `s3` behind injected clients (no live network in CI), the `docs://` resolver, the scheduler, and
  the formatter.

## Capabilities

### New Capabilities

- `doc-store-manager`: the `DocStoreBackend` interface, the `local_fs`/`git_repo`/`s3` backends, the
  `DocRef` model, the checksum-based sync scheduler, Knowledge Engine `doc`-node integration, the
  `docs://` HARNESS resolver, the `## Relevant Documentation` formatter, the `DocStoreSync` seam
  implementation, the `docs` subcommands, and SPEC §23.5 doc-store error classification.

### Modified Capabilities

None. The Knowledge Engine, orchestrator (`DocStoreSync` seam), config, and harness loader are
consumed unchanged.

## Impact

- **Affected specs**: new capability `doc-store-manager`.
- **Affected code**: `internal/docstore/` (new). New `cmd/conductor/cmd/cmd_docs.go`.
- **Integration points (last commit only, append-only)** per the Wave B integration-commit rule:
  - `cmd/conductor/cmd/root.go` — `root.AddCommand(newDocsCommand())`.
  - `cmd/conductor/cmd/start.go` `runOrchestrator` — construct the manager and wire
    `orchestrator.WithDocStoreSync(...)`.
- **Consumes (unchanged)**: `internal/knowledge` (index `doc` nodes), `internal/config` (`Docs`,
  `DocStoreConfig`), `internal/harness` (the loader, for `docs://` resolution), `internal/audit`,
  `internal/orchestrator` (the `DocStoreSync` seam).

### Non-goals

- The `conductor_doc_search` agent tool (Phase 13) — this phase ships the manager + CLI, not
  in-session tool injection.
- Notion and Confluence backends — deferred to a follow-up phase; only `local_fs`, `git_repo`, `s3`.
- The `custom` plugin backend (Phase 18 extensibility surface).
- Wiring `## Relevant Documentation` into the live turn prompt assembly (Phase 13) — the formatter
  ships and is unit-tested; the prompt-assembly insertion is a later seam.
