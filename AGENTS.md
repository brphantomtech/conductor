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
- Phase 1 atomic task list → [TASKS.md](TASKS.md)

This file is a map. It contains no rules. Rules and rationale live in `docs/`.
