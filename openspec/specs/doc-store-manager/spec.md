# doc-store-manager Specification

## Purpose
TBD - created by archiving change phase-11-doc-store-manager. Update Purpose after archive.
## Requirements
### Requirement: Doc Store Backend Interface

The Doc Store Manager SHALL define a `DocStoreBackend` interface (SPEC §20.4) with `Sync`, `Fetch`,
and `List` operations, and SHALL provide `local_fs`, `git_repo`, and `s3` implementations (SPEC
§10.2). Each backend SHALL resolve its credentials from configuration without writing them to disk
or logs.

#### Scenario: local_fs backend lists and fetches documents

- **WHEN** a `local_fs` store is synced against a directory of documents
- **THEN** each document is returned as a `DocRef` and its content is retrievable via `Fetch`

#### Scenario: Remote backends use injected clients

- **WHEN** a `git_repo` or `s3` store is synced in a test
- **THEN** the backend uses its injected runner/client so no live network call is made

### Requirement: DocRef Model

The manager SHALL represent each document as a `DocRef` (SPEC §4.1.9) carrying `id`, `title`,
`store_id`, `path_or_id`, `content_hash`, `tags`, an optional `embedding`, and `last_synced_at`.

#### Scenario: DocRef carries change-detection metadata

- **WHEN** a document is synced
- **THEN** its `DocRef` includes a `content_hash` and `last_synced_at` used for incremental sync

### Requirement: Checksum-Based Synchronization

The manager SHALL run an initial sync on startup (unless a recent sync exists) and resync each store
on its configured interval (SPEC §10.3). Sync SHALL compare document checksums against the stored
set and download only changed documents, re-index changed documents in the Knowledge Engine, and on
failure log a structured warning while retaining the last successfully synced documents. A
`DocStoreSynced` audit event SHALL be emitted per store sync, and the manager SHALL supply the
orchestrator's doc-store-sync poll-loop seam.

#### Scenario: Only changed documents are downloaded

- **WHEN** a store is resynced and some documents are unchanged
- **THEN** unchanged documents are skipped and only changed documents are fetched and re-indexed

#### Scenario: Failed sync retains last-good documents

- **WHEN** a sync fails partway
- **THEN** the previously synced documents and their index entries are retained and the error is
  classified as `doc_store_sync_failed`

### Requirement: Knowledge Engine Integration

The manager SHALL index synced documents as `doc` nodes in the Knowledge Engine so they are
searchable through the existing hybrid query path.

#### Scenario: Synced document becomes a searchable doc node

- **WHEN** a document is synced and indexed
- **THEN** a `doc`-type knowledge node is created for it and is returned by a hybrid search filtered
  to `doc` nodes

### Requirement: docs:// HARNESS Resolution

The manager SHALL resolve a `docs://<store>/<path>` HARNESS reference by fetching the document from
the configured store, caching it locally, and providing the local path to the harness loader before
parsing.

#### Scenario: docs:// reference resolves to a local cached file

- **WHEN** `conductor start --harness docs://specs/HARNESS.md` is used
- **THEN** the referenced document is fetched, cached locally, and parsed as the harness definition

### Requirement: Documentation Context Formatting and CLI

The manager SHALL format relevant documents as a `## Relevant Documentation` block (SPEC §10.5) for
prompt injection, and SHALL provide `conductor docs sync` and `conductor docs search` subcommands.

#### Scenario: docs sync runs all configured stores

- **WHEN** `conductor docs sync` is invoked
- **THEN** each configured store is synced and a `DocStoreSynced` event is emitted per store

#### Scenario: docs search returns matching documents

- **WHEN** `conductor docs search "<query>"` is invoked
- **THEN** matching documents are returned ranked by relevance

