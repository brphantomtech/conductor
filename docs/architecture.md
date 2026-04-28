# Architecture

This document describes Conductor's internal layering, the dependency direction between layers,
and the layer-definitions contract that the Knowledge Engine and Harness Enforcer use to police
the codebase.

## 1. Layer Stack

Conductor's Go backend is organized into seven dependency tiers. Higher tiers may import lower
tiers; lower tiers MUST NOT import higher tiers. This is enforced by `.golangci.yml` (depguard).

```
┌──────────────────────────────────────────────────────────┐
│ Tier 6: cmd/                                             │ ← CLI entrypoint (Cobra)
├──────────────────────────────────────────────────────────┤
│ Tier 5: internal/api                                     │ ← HTTP / WebSocket surface
├──────────────────────────────────────────────────────────┤
│ Tier 4: internal/orchestrator                            │ ← Top-level coordinator
├──────────────────────────────────────────────────────────┤
│ Tier 3: internal/router                                  │ ← Pipeline execution
├──────────────────────────────────────────────────────────┤
│ Tier 2: internal/{harness, knowledge, memory,            │ ← Domain engines
│          validation, docstore}                           │
├──────────────────────────────────────────────────────────┤
│ Tier 1: internal/{provider, tracker, workspace, audit}   │ ← External adapters & infra
├──────────────────────────────────────────────────────────┤
│ Tier 0: internal/{config, db}                            │ ← Foundation
└──────────────────────────────────────────────────────────┘
```

### Tier Responsibilities

**Tier 0 — Foundation.** `config` provides the typed configuration loader (Viper-backed). `db`
provides the database abstraction (SQLite default, Postgres optional). These are pure data
layers with no Conductor-specific business logic.

**Tier 1 — External adapters & infra.** Each package owns one external concern:

- `provider` — LLM provider adapters behind a stable `ProviderAdapter` interface (SPEC §7).
- `tracker` — issue tracker adapters behind a stable `TrackerAdapter` interface (SPEC §20.1).
- `workspace` — workspace lifecycle: create, hook execution, removal, sandboxing (SPEC §14).
- `audit` — provenance graph writer; structured event log (SPEC §17).

**Tier 2 — Domain engines.** Each is a long-running service:

- `harness` — `HARNESS.md` loader, parser, validator, hot-reload watcher (SPEC §5–6).
- `knowledge` — codebase indexer, AST graph, hybrid RAG (SPEC §8).
- `memory` — three-layer Memory Manager: episodic, semantic, procedural (SPEC §9).
- `validation` — Validation Pipeline runner (SPEC §15).
- `docstore` — Doc Store Manager + backends (SPEC §10).

**Tier 3 — Pipeline execution.** `router` owns issue classification, pipeline selection, and
turn-by-turn execution (SPEC §12). It composes Tier 1 + Tier 2 to run a pipeline for an issue.

**Tier 4 — Orchestrator.** The poll loop, dispatch logic, retry/backoff, reconciliation, and
runtime state machine (SPEC §13). Owns the singleton `OrchestratorRuntimeState`.

**Tier 5 — API surface.** REST + WebSocket handlers (SPEC §18). Reads orchestrator state and
exposes commands. Embeds the SvelteKit dashboard via Go `embed`.

**Tier 6 — CLI.** Cobra commands that bind to APIs in Tier 5 (or directly to lower tiers for
offline subcommands like `harness validate`, `knowledge index`). The only `main` package.

## 2. Dependency Direction Rules

These are the hard rules `.golangci.yml` enforces:

1. **No upward imports.** `internal/db` MUST NOT import `internal/orchestrator`. `internal/provider`
   MUST NOT import `internal/router`. Any upward edge is a build failure.
2. **No sibling cross-imports inside Tier 1 and Tier 2.** Adapters and engines communicate
   through Tier 3+ coordinators, not through each other directly. Exception: `audit` may be
   imported by any tier — it is the cross-cutting log.
3. **`web/` is not a Go package.** It is built by Vite (via Bun) and consumed only by
   `internal/api` through the `embed` package.
4. **Tests follow the same rules** as production code, plus they may import `testify`.

## 3. Project Layer Definitions

Conductor's own `HARNESS.md` will define `knowledge.layer_definitions` to mirror the tier stack
above. This lets the Harness Enforcer use the Knowledge Engine's `CheckLayerViolations` (SPEC
§8.6) to detect cross-layer imports automatically. See SPEC §5.3.8.

```yaml
knowledge:
  layer_definitions:
    foundation: ["internal/config/**", "internal/db/**"]
    adapters: ["internal/provider/**", "internal/tracker/**", "internal/workspace/**", "internal/audit/**"]
    engines: ["internal/harness/**", "internal/knowledge/**", "internal/memory/**", "internal/validation/**", "internal/docstore/**"]
    router: ["internal/router/**"]
    orchestrator: ["internal/orchestrator/**"]
    api: ["internal/api/**"]
    cmd: ["cmd/**"]
```

The order in this map is significant: lower-indexed layers are "lower" in the stack. By default
higher layers may import lower layers; lower may not import higher (SPEC §8.6). This is the
authoritative definition — `.golangci.yml` mirrors it for build-time enforcement, the Knowledge
Engine mirrors it at runtime.

## 4. Cross-Cutting Concerns

- **Configuration**: every Tier 1+ package receives its config slice from Tier 0 by value at
  construction time. No package reads `os.Getenv` directly except the config loader.
- **Logging**: every package uses `zerolog`. Loggers are passed in at construction, not pulled
  from a global. The audit log is a separate concern from the structured log.
- **Context propagation**: every public method on Tier 1+ takes `context.Context` as the first
  argument. Cancellation is the only way to stop in-flight work.
- **Errors**: see [conventions.md](conventions.md) for the wrapping policy.

## 5. Testing Boundaries

- Unit tests live next to the code (`foo.go` → `foo_test.go`).
- Integration tests live in `internal/<pkg>/integration_test.go` with build tag `//go:build
  integration` so `go test ./...` runs unit tests only by default.
- Cross-tier "system" tests live under `tests/` (a separate top-level directory, not Tier-bound).
