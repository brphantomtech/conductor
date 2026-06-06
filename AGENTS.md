# AGENTS.md — Navigation Map

Conductor is an AI-provider-agnostic, harness-first agent orchestration service that reads work
from issue trackers, creates isolated per-issue workspaces, and executes specialized AI coding
agent pipelines enriched with codebase intelligence, persistent memory, and decoupled docs. It is
the long-running scheduler/runner/knowledge-manager/memory-store/harness-enforcer; project-specific
business logic lives in `HARNESS.md` (the policy contract). Reference implementation: Go 1.23+
backend with embedded SvelteKit dashboard, SQLite-default storage, single-binary deploy.

## Layers (from SPEC §3.3)

Backend (Go):

- [cmd/conductor/](cmd/conductor/) — CLI entrypoint (Cobra + Viper)
- [internal/orchestrator/](internal/orchestrator/) — core scheduling state machine
- [internal/harness/](internal/harness/) — `HARNESS.md` loader, validator, enforcer
- [internal/knowledge/](internal/knowledge/) — Knowledge Engine: indexing, RAG, AST graph
- [internal/memory/](internal/memory/) — three-layer Memory Manager (episodic/semantic/procedural)
- [internal/docstore/](internal/docstore/) — Doc Store Manager + backends (git, S3, Notion, etc.)
- [internal/tracker/](internal/tracker/) — issue tracker adapters (Linear, GitHub, Jira, Plane, Shortcut)
- [internal/provider/](internal/provider/) — LLM provider adapters (OpenRouter, Anthropic, OpenAI, Ollama, LM Studio, custom)
- [internal/router/](internal/router/) — Agent Router + pipeline executor
- [internal/workspace/](internal/workspace/) — workspace lifecycle manager
- [internal/validation/](internal/validation/) — Validation Pipeline (per-turn shell checks)
- [internal/audit/](internal/audit/) — audit trail writer + provenance graph queries
- [internal/api/](internal/api/) — HTTP API handlers (Chi) + WebSocket (Gorilla)
- [internal/config/](internal/config/) — typed config layer (Viper, dynamic reload)
- [internal/db/](internal/db/) — database abstraction (SQLite default, Postgres opt-in)

Frontend (SvelteKit, embedded into Go binary via `embed`):

- [web/](web/) — SvelteKit source (routes, components, stores, API client)

## Where to find the rest

- Architecture, layer rules, and dependency direction → [docs/architecture.md](docs/architecture.md)
- Tech stack rationale (one ADR per decision from SPEC §3.1) → [docs/decisions.md](docs/decisions.md)
- Implementation phases and ordering → [docs/phases.md](docs/phases.md)
- Go conventions (naming, errors, interfaces) → [docs/conventions.md](docs/conventions.md)
- Authoritative reference → [SPEC.md](SPEC.md)
- Phase 1 + Phase 2 atomic task list → [TASKS.md](TASKS.md)
- Phase 2 HARNESS.md loader (parser, validator, renderer, watcher) → [internal/harness/](internal/harness/)
- Phase 6 orchestrator core (poll loop, runtime state, run-attempt lifecycle, candidate selection, retry/backoff, reconciliation, startup cleanup, single coder dispatch) → [internal/orchestrator/](internal/orchestrator/)
- Phase 7 Agent Router (issue classification, routing-rule pipeline selection, role-by-role pipeline execution with output hand-off, SPEC §16 prompt construction + budget truncation, continuation handling) → [internal/router/](internal/router/)
- Phase 8 validation pipeline (per-turn shell-check runner with timeout/classification, atomic per-turn JSON persistence, fail_on_severity decision, context formatter, `conductor validation run`) → [internal/validation/](internal/validation/)
- Phase 10 Knowledge Engine (seven-stage indexing pipeline, KnowledgeNode/KnowledgeEdge model, sqlite_vec store with qdrant behind a build tag, incremental fsnotify re-index, hybrid search, `## Codebase Context` formatter, layer-violation detection, `conductor knowledge` CLI) → [internal/knowledge/](internal/knowledge/)
- Phase 11 Doc Store Manager (DocRef model, Backend extension point with local_fs/git_repo/s3 backends behind injected clients, checksum-based sync scheduler with last-good retention, doc-node indexing into the Knowledge Engine, `docs://` HARNESS resolver, `## Relevant Documentation` formatter, DocStoreSync seam, `conductor docs` CLI) → [internal/docstore/](internal/docstore/)
- Phase 12 Harness Enforcer (HarnessRule runner with workspace-root command factory + timeout/severity classification, pre-dispatch drift check on the orchestrator's EnforcerCheck seam with blocking-halt, scheduled GC via robfig/cron creating deduplicated tracker issues for auto_fix violations, Knowledge-Engine layer-violation translation, `## Known Technical Debt` / `## Architectural Issues You Must Fix` formatters, `conductor harness check` CLI) → [internal/harness/](internal/harness/) (enforcer.go, runner.go, gc.go, layers.go, formatters.go)
- Phase 16 CLI surface completion (`conductor workspace list/remove/open`, `conductor init --profile local|team|cloud` scaffolding, static `conductor status` snapshot, and offline-capable `conductor dispatch`/`cancel` that report when the running service is required — live control channel is Phase 14) → [cmd/conductor/cmd/](cmd/conductor/cmd/) (`cmd_workspace.go`, `cmd_init.go`, `cmd_status.go`, `cmd_dispatch.go`, `cmd_cancel.go`)

This file is a map. It contains no rules. Rules and rationale live in `docs/`.
