## Context

The Doc Store Manager depends only on substrate already on `main`: the Knowledge Engine (Phase 10,
with its `doc` node type and hybrid search), `config.Docs`/`config.DocStoreConfig` (already declared
in [internal/config/types.go](../../../internal/config/types.go)), the harness loader (for
`docs://` resolution), `audit.Writer`, and the orchestrator's `DocStoreSync` seam
(`WithDocStoreSync` + the no-op default already present from Phase 6). Nothing in the poll loop
changes; this phase supplies an implementation of an existing seam.

SPEC anchors: §10.2 (backends), §10.3 (sync contract), §10.4 (`docs://`), §10.5 (agent
integration / formatting), §4.1.9 (DocRef), §20.4 (DocStoreBackend interface), §23.5 (error).
Conventions to match (Phases 2–10): constructor + functional `Option`s, injected clock for the
scheduler, injected clients for `git_repo`/`s3` so CI needs no network, sentinel errors equal to
SPEC strings, recorded fixtures, audit via a small helper.

This is a Wave B worktree; all logic stays in `internal/docstore`, the only shared-file edits are
the two append-only integration points in the final commit.

## Goals / Non-Goals

**Goals:**

- A `DocStoreBackend` interface with `local_fs`, `git_repo`, `s3` implementations.
- A checksum-based sync scheduler with initial + interval syncs and last-good retention.
- Indexing changed docs as `doc` nodes in the Knowledge Engine.
- A `docs://` HARNESS resolver and the `## Relevant Documentation` formatter.
- The `DocStoreSync` seam implementation and a `conductor docs` CLI.

**Non-Goals:**

- The `conductor_doc_search` agent tool (Phase 13); Notion/Confluence backends; the `custom` plugin
  (Phase 18); live prompt wiring (Phase 13).

## Decisions

### `DocStoreBackend` interface, one implementation per backend

`Sync`/`Fetch`/`List` per SPEC §20.4. `local_fs` walks a directory; `git_repo` shells out to git
(clone/pull) behind an injected runner; `s3` uses an injected object-store client interface.

- **Why:** SPEC §20.4 fixes the interface; per-backend types keep each backend's auth and transport
  isolated and individually testable. Injected runner/client means `git_repo` and `s3` tests use
  fakes — no live network in CI.

### Checksum-based incremental sync with last-good retention

Each sync lists the backend, compares `content_hash` against the stored `DocRef` set, downloads only
changed docs, re-indexes them, and on failure logs a structured warning while keeping the previously
synced set (SPEC §10.3 step 4).

- **Why:** verbatim SPEC §10.3; checksum diffing bounds both download and re-embedding cost and makes
  the scheduler idempotent. Last-good retention means a transient backend outage never empties the
  index.

### Scheduler driven by an injected clock

Initial sync on startup unless the last sync is within the interval; thereafter a ticker re-reading
`sync_interval_minutes` per store, manually drivable in tests.

- **Why:** time-dependent behavior must be deterministic in tests; matches the Phase 2 watcher /
  Phase 9 consolidation-worker style.

### Knowledge integration via `doc` nodes, not a parallel store

Changed docs are upserted as `doc`-type `KnowledgeNode`s through the existing Knowledge Engine store,
so doc search rides the same hybrid query path.

- **Why:** SPEC §10.1 ("indexed alongside the codebase ... via the same hybrid RAG"); reuses Phase
  10 rather than duplicating a vector store, and makes Phase 13's `conductor_doc_search` a thin
  filter (`types: [doc]`) over existing search.

### `docs://` resolved before parse, cached locally

The resolver maps `docs://<store>/<path>` to a backend `Fetch`, writes the content to a local cache
path, and hands the local path to the existing harness loader.

- **Why:** SPEC §10.4; keeps the harness loader unchanged (it still parses a local file) and lets the
  next sync interval trigger a config reload through the Phase 2 watcher.

## Risks / Trade-offs

- **[git/s3 network in CI]** → Both are behind injected runner/client interfaces; tests use fakes.
  Only `local_fs` touches the real filesystem (a temp dir).
- **[Large doc sets / sync cost]** → Checksum diffing downloads and re-indexes only changed docs;
  the scheduler is interval-bounded and logs what it skipped.
- **[Backend outage]** → Last-good retention (SPEC §10.3 step 4): a failed sync logs a warning and
  leaves the prior `DocRef` set and index intact; `doc_store_sync_failed` classifies the error.
- **[`docs://` + hot reload races]** → The resolver caches to a local file; the Phase 2 watcher owns
  reload. A mid-sync change is picked up on the next interval, not mid-parse.
- **[Secret handling for git/s3 auth]** → Credentials come from config `$VAR` expansion (Phase 1) and
  are never written to disk or logs (SPEC §21.1); only the injected client receives them.

## Migration Plan

Additive only. New `internal/docstore` package and one new CLI file. The two integration-point edits
(`root.go` AddCommand, `start.go` `WithDocStoreSync`) land in the final commit per the Wave B
integration-commit rule. Doc nodes live in the existing Knowledge store (no new schema). Rollback is
reverting the package + the two wiring lines. `docs.enabled = false` leaves behavior unchanged.

## Open Questions

- **`s3` client dependency** — use the AWS SDK vs. a minimal signed-HTTP client; start behind an
  interface so the concrete dependency can be decided during implementation without affecting tests.
- **`docs://` cache location** — under the workspace `.conductor/` vs. a dedicated cache dir; default
  to `.conductor/doc-cache/` and revisit if multi-store collisions appear.
- **Where doc context is injected** — formatter ships now; the live insertion at SPEC §16.1 step 5 is
  owned by Phase 13 and deferred.
