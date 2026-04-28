# Conductor — Service Specification v0.1

**Status:** Draft  
**Language:** English (canonical)  
**Version:** 0.1 — enriched with technology stack decisions

---

## Preface

This document specifies Conductor: an AI-provider-agnostic, harness-first agent orchestration
service. It is written as the authoritative reference for implementors — human and AI alike.

Conductor was designed by identifying the structural gaps in OpenAI's Symphony specification and
Harness Engineering methodology, then solving them at the architecture level rather than leaving
them as operational concerns.

> **Guiding principle**: the harness — not the model — determines whether autonomous agents
> ship reliable software at scale. Conductor's job is to be the best possible harness.

---

## 0. Motivation: Gaps in the Symphony Specification

This section is intentional. Understanding what Conductor improves over Symphony is essential for
understanding why each design decision was made the way it was.

### 0.1 Symphony's Structural Deficits

| Dimension                    | Symphony (Gap)                                                                   | Conductor (Resolution)                                                                                        |
| ---------------------------- | -------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------- |
| **AI provider**              | Hard-wired to Codex App Server via proprietary JSON-RPC protocol                 | Provider Adapter layer: OpenRouter, Anthropic, OpenAI, Ollama, LM Studio, any OpenAI-compatible endpoint      |
| **Codebase intelligence**    | Agent is contextually blind; operates only on workspace files it happens to open | Knowledge Engine: semantic + structural graph, hybrid RAG injected into every turn                            |
| **Memory**                   | No cross-session persistence; each attempt starts from zero                      | Three-layer memory system: episodic, semantic, procedural                                                     |
| **Documentation**            | `docs/` lives inside the project repo; no external management                    | Decoupled Doc Store: git repos, S3, Notion, Confluence, local FS — configurable independently of the codebase |
| **Tracker support**          | Linear only                                                                      | Multi-tracker adapter: Linear, GitHub Issues, Jira, Plane, Shortcut                                           |
| **Architecture enforcement** | Mentioned as a manual practice; not a first-class orchestrator concern           | Harness Enforcer: drift detection before every dispatch, scheduled GC cron, blocking violation gates          |
| **Agent validation**         | Agents self-evaluate without external feedback loops                             | Validation Pipeline: lint, tests, type checks run after each turn and injected as context for the next        |
| **Agent specialization**     | One monolithic agent per issue                                                   | Agent Router: configurable pipelines with specialized roles — Planner, Coder, Verifier, Reviewer, GC Agent    |
| **Context anxiety**          | Unaddressed                                                                      | Per-provider context budget management with configurable compaction strategies (summarize, sliding window)    |
| **Audit trail**              | Basic structured logs                                                            | Provenance graph: every tool call, memory read, validation result, and decision is recorded with causal links |
| **Multi-repo**               | Implicit single-repo per workspace                                               | First-class multi-repo workspace support                                                                      |

### 0.2 Harness Engineering Principles Not Implemented by Symphony

The OpenAI harness engineering post identified four problems and solutions. Symphony implements
the orchestration skeleton but leaves the harness layer itself to the user:

1. **Shared codebase understanding** — Symphony provides `WORKFLOW.md` and a workspace. It does
   not index the codebase, build a dependency graph, or inject structured knowledge. Conductor's
   Knowledge Engine solves this at the infrastructure level.

2. **Automated self-validation** — The OpenAI team plugged observability tools directly into
   Codex. Symphony does not specify how agents validate their own output. Conductor's Validation
   Pipeline runs checks after each agent turn and feeds results back as context — shifting feedback
   left without requiring agent self-assessment.

3. **Architectural drift enforcement** — The OpenAI team encoded layered architecture into custom
   linters with inline fix messages. Symphony mentions this pattern but provides no mechanism.
   Conductor's Harness Enforcer checks layer invariants before every dispatch and optionally
   creates GC issues automatically.

4. **Silent technical debt** — The OpenAI team ran background Codex tasks on a schedule to refactor
   drift. Symphony has no equivalent. Conductor's Enforcement scheduler implements this directly.

---

## 1. Problem Statement

Conductor is a long-running orchestration service that continuously reads work from an issue
tracker, creates isolated per-issue workspaces, and executes specialized AI coding agent pipelines
for each issue — enriched with live codebase intelligence, persistent cross-session memory, and
decoupled documentation management.

The service solves six operational problems:

- **Provider freedom**: run any LLM — through any API endpoint — as the execution engine, without
  changing the orchestration layer.
- **Intelligent context**: give every agent structured knowledge of the codebase before it touches
  a single file, reducing architectural drift and hallucinated APIs.
- **Knowledge continuity**: preserve and retrieve what agents learned across sessions, issues, and
  service restarts.
- **Decoupled documentation**: manage project specs, ADRs, and knowledge bases independently of
  the codebase repository.
- **Harness enforcement**: detect and remediate architectural drift automatically, without human
  intervention.
- **Observable execution**: maintain a complete causal record of every agent decision for
  debugging and governance.

**System boundary:**

- Conductor is a scheduler, runner, knowledge manager, memory store, and harness enforcer.
- Project-specific business logic lives in `HARNESS.md` (the policy contract).
- Issue tracker writes (state transitions, comments, PR links) are performed by agents using
  tools injected by Conductor — not by Conductor's orchestration logic directly.
- A successful run may end at any workflow-defined handoff state, not necessarily a terminal
  tracker state.

---

## 2. Goals and Non-Goals

### 2.1 Goals

- Abstract AI providers behind a stable execution interface.
- Route issues to specialized agent pipelines based on configurable rules.
- Index the codebase semantically and inject structured knowledge into every agent turn.
- Maintain a three-layer persistent memory system across sessions and restarts.
- Manage project documentation in stores that are decoupled from the codebase repository.
- Enforce architectural invariants before dispatch and on a configurable schedule.
- Run a per-turn Validation Pipeline that shifts feedback left into the agent's context.
- Support multiple issue trackers through interchangeable adapters.
- Support hybrid deployment: single binary for local dev, composable for cloud.
- Expose a full-control dashboard: observability, manual dispatch, memory management, harness editing.
- Emit a complete audit trail with provenance graph for every agent decision.
- Recover from failures with exponential backoff and tracker-driven reconciliation.
- Reload configuration dynamically without service restart.

### 2.2 Non-Goals

- Mandating a specific sandbox or approval posture for all deployments (documented by each
  implementation).
- Replacing existing CI/CD systems (Conductor integrates with them).
- Providing built-in tracker write logic (that belongs in the agent workflow).
- General-purpose distributed job scheduler (Conductor is domain-specific to coding agent
  orchestration).

---

## 3. Technology Stack

### 3.1 Design Decisions Summary

These decisions are made for the reference implementation. The specification remains
language-agnostic at the orchestration level, but the reference implementation targets the
stack below.

| Layer                         | Technology                                                                                                                               | Rationale                                                                                                                                                       |
| ----------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Backend runtime**           | Go 1.23+                                                                                                                                 | Single binary deployment, native goroutine concurrency, excellent stdlib, strong ecosystem for CLI and HTTP servers                                             |
| **Package manager (backend)** | Go modules                                                                                                                               | Native; no additional tooling required                                                                                                                          |
| **HTTP server**               | `net/http` + [Chi v5](https://github.com/go-chi/chi)                                                                                     | Lightweight router, zero dependencies beyond stdlib, idiomatic Go                                                                                               |
| **WebSocket**                 | [Gorilla WebSocket](https://github.com/gorilla/websocket)                                                                                | Stable, battle-tested, used for real-time dashboard updates                                                                                                     |
| **CLI**                       | [Cobra](https://github.com/spf13/cobra) + [Viper](https://github.com/spf13/viper)                                                        | De facto standard for Go CLIs; Viper for config file + env var integration                                                                                      |
| **Database (default)**        | SQLite via [modernc/sqlite](https://pkg.go.dev/modernc.org/sqlite)                                                                       | Pure Go, zero CGO, embeds in the binary; no external deps for local deployment                                                                                  |
| **Database (optional)**       | PostgreSQL via [pgx](https://github.com/jackc/pgx)                                                                                       | High-concurrency cloud deployments; team-scale persistent state                                                                                                 |
| **Vector store (default)**    | SQLite + [sqlite-vec](https://github.com/asg017/sqlite-vec)                                                                              | Hybrid vector + relational in the same file; no external service needed                                                                                         |
| **Vector store (optional)**   | [Qdrant](https://qdrant.tech/)                                                                                                           | Large codebases, high-frequency similarity search, cloud deployments                                                                                            |
| **Template engine**           | [Liquid Go](https://github.com/osteele/liquid)                                                                                           | Liquid-compatible semantics matching Symphony spec; strict variable checking                                                                                    |
| **YAML parsing**              | [go-yaml v3](https://github.com/go-yaml/yaml)                                                                                            | Standard for Go YAML; handles HARNESS.md front matter                                                                                                           |
| **Process management**        | `os/exec` + goroutines                                                                                                                   | Agent runners as goroutines with subprocess management; Docker as opt-in                                                                                        |
| **Filesystem watch**          | [fsnotify](https://github.com/fsnotify/fsnotify)                                                                                         | Cross-platform file system events for HARNESS.md hot-reload and knowledge indexing                                                                              |
| **Frontend runtime**          | [Bun](https://bun.sh/)                                                                                                                   | Fast JS runtime + package manager + bundler                                                                                                                     |
| **Frontend framework**        | [SvelteKit](https://kit.svelte.dev/)                                                                                                     | Superior runtime performance over React (no virtual DOM), smaller bundles, built-in SSR for fast initial load, excellent real-time reactivity via Svelte stores |
| **Frontend styling**          | [Tailwind CSS v4](https://tailwindcss.com/)                                                                                              | Utility-first, zero runtime, pairs perfectly with Svelte                                                                                                        |
| **Frontend build**            | Vite (via SvelteKit) + Bun                                                                                                               | Sub-second HMR, optimized production builds                                                                                                                     |
| **Frontend state**            | Svelte stores + WebSocket                                                                                                                | Real-time updates from backend without polling                                                                                                                  |
| **Embedding delivery**        | Go `embed` package                                                                                                                       | Frontend assets embedded in Go binary for single-binary deployment                                                                                              |
| **Codebase parsing**          | [go-tree-sitter](https://github.com/smacker/go-tree-sitter)                                                                              | AST-level dependency graph extraction per language                                                                                                              |
| **Cron scheduling**           | [robfig/cron](https://github.com/robfig/cron)                                                                                            | Structured cron expressions for GC enforcement scheduling                                                                                                       |
| **Logging**                   | [zerolog](https://github.com/rs/zerolog)                                                                                                 | Structured JSON logging, zero-allocation, excellent performance                                                                                                 |
| **Testing**                   | Go `testing` + [testify](https://github.com/stretchr/testify) + [testcontainers-go](https://github.com/testcontainers/testcontainers-go) | Unit + integration testing with real DB containers                                                                                                              |
| **Container (optional)**      | Docker + Docker Compose                                                                                                                  | Cloud and team deployments                                                                                                                                      |

> **On the SvelteKit choice**: The user mentioned React + Tailwind as a reference point. After
> evaluating the requirements — real-time dashboard, complex state (running agents, streaming
> logs, memory graphs), full control panel — SvelteKit is the stronger choice. It compiles to
> vanilla JS with no virtual DOM overhead, resulting in 30–50% smaller bundles and measurably
> faster runtime than equivalent React apps. The component model is simpler for streaming and
> reactive state. The learning curve for anyone familiar with React is 1–2 days. If React is a
> hard requirement, Vite + React 19 + Tanstack Query is the recommended fallback.

### 3.2 Deployment Profiles

#### Profile A: Local (default)

Single Go binary. No external dependencies.

```
conductor          ← single binary
  ├── embedded SQLite (state + memory + vector store)
  ├── embedded SvelteKit assets (dashboard served at :8080)
  └── HARNESS.md (local or remote Doc Store reference)
```

Startup: `conductor start --harness ./HARNESS.md`

#### Profile B: Team / Self-hosted

Docker Compose with optional external services.

```yaml
# docker-compose.yml (generated by `conductor init --profile team`)
services:
  conductor:
    image: conductor:latest
    ports: ["8080:8080"]
    environment:
      - CONDUCTOR_DB=postgres
      - DATABASE_URL=postgres://...
      - CONDUCTOR_VECTOR_STORE=qdrant
      - QDRANT_URL=http://qdrant:6333
    volumes:
      - ./HARNESS.md:/harness/HARNESS.md
      - workspaces:/workspaces

  postgres:
    image: postgres:16-alpine
    ...

  qdrant:
    image: qdrant/qdrant:latest
    ...

volumes:
  workspaces:
```

#### Profile C: Cloud-native

Kubernetes-ready. Each component scales independently. Conductor exposes health and readiness
endpoints. Workspaces use a shared volume or object storage (S3-compatible). State in PostgreSQL.
Vector store in Qdrant.

### 3.3 Binary Layout

```
conductor/
├── cmd/
│   └── conductor/
│       └── main.go              ← CLI entrypoint (Cobra)
├── internal/
│   ├── orchestrator/            ← Core scheduling state machine
│   ├── harness/                 ← Harness loader + validator + enforcer
│   ├── knowledge/               ← Knowledge Engine (indexing, RAG)
│   ├── memory/                  ← Memory Manager (three-layer)
│   ├── docstore/                ← Doc Store Manager + backends
│   ├── tracker/                 ← Tracker adapters (Linear, GitHub, Jira...)
│   ├── provider/                ← LLM Provider adapters
│   ├── router/                  ← Agent Router + pipeline executor
│   ├── workspace/               ← Workspace lifecycle manager
│   ├── validation/              ← Validation Pipeline
│   ├── audit/                   ← Audit Trail writer + query
│   ├── api/                     ← HTTP API handlers (Chi)
│   ├── config/                  ← Typed config layer (Viper)
│   └── db/                      ← Database abstraction (SQLite / Postgres)
├── web/                         ← SvelteKit frontend source
│   ├── src/
│   │   ├── routes/              ← SvelteKit pages
│   │   ├── lib/
│   │   │   ├── components/      ← UI components
│   │   │   ├── stores/          ← Svelte reactive stores
│   │   │   └── api/             ← HTTP + WebSocket client
│   │   └── app.html
│   ├── package.json
│   └── vite.config.ts
├── go.mod
├── go.sum
├── Makefile                     ← build, test, embed-web, docker
└── HARNESS.md                   ← default harness for Conductor's own development
```

---

## 4. Core Domain Model

### 4.1 Entities

#### 4.1.1 Issue

Normalized issue record used across orchestration, prompt rendering, and observability.

Fields:

- `id` (string) — stable tracker-internal ID.
- `identifier` (string) — human-readable ticket key (e.g., `ABC-123`).
- `title` (string)
- `description` (string or null)
- `priority` (integer or null) — lower values are higher priority.
- `state` (string) — current tracker state name.
- `branch_name` (string or null) — tracker-provided branch hint.
- `url` (string or null)
- `labels` (list of strings) — normalized to lowercase.
- `blocked_by` (list of BlockerRef)
  - `id` (string or null)
  - `identifier` (string or null)
  - `state` (string or null)
- `created_at` (timestamp or null)
- `updated_at` (timestamp or null)
- `task_type` (string or null) — classified by the Agent Router on first dispatch:
  `feature`, `bug`, `refactor`, `investigation`, `docs`, `gc_task`, `unknown`.
- `estimated_complexity` (enum or null) — estimated by Planner Agent: `trivial`, `small`,
  `medium`, `large`, `epic`.
- `knowledge_context_ids` (list of strings) — IDs of Knowledge Engine chunks retrieved for this
  issue on last dispatch.
- `memory_session_ids` (list of strings) — IDs of past memory sessions related to this issue.

#### 4.1.2 Harness Definition

Parsed `HARNESS.md` payload:

- `config` (map) — YAML front matter root object.
- `prompt_templates` (map `agent_role -> string`) — one template per agent role, parsed from
  Markdown body sections (`## <role>`).
- `harness_rules` (list of HarnessRule) — rules extracted from front matter.
- `doc_refs` (list of DocRef) — references to Doc Store documents.

#### 4.1.3 Provider Config

Typed LLM provider configuration.

Fields:

- `provider` (string) — one of: `openrouter`, `anthropic`, `openai`, `ollama`, `lm_studio`,
  `custom`.
- `model` (string) — model identifier as understood by the provider.
- `api_key` (string or `$VAR`) — literal token or env var reference.
- `base_url` (string, optional) — overrides provider's default endpoint.
- `max_tokens` (integer) — maximum tokens to generate per turn. Default: `8192`.
- `temperature` (float, optional) — provider default if absent.
- `context_budget` (integer, optional) — soft token budget for context window; triggers compaction
  when breached. Default: `null` (no limit beyond model max).
- `compaction_strategy` (enum) — `none`, `summarize`, `sliding_window`. Default: `summarize`.
- `extra_params` (map, optional) — arbitrary params forwarded verbatim to the provider API.
  Useful for provider-specific features (e.g., `top_p`, `repetition_penalty`).

#### 4.1.4 Agent Role

A named participant in the execution pipeline for an issue.

Built-in roles:

| Role       | Responsibility                                                                                  |
| ---------- | ----------------------------------------------------------------------------------------------- |
| `planner`  | Decomposes the issue into subtasks; estimates complexity; produces a structured execution plan. |
| `coder`    | Implements the work. Primary execution role. Receives the plan from `planner`.                  |
| `verifier` | Validates coder output against harness rules and acceptance criteria.                           |
| `reviewer` | Produces PR description, changelog entry, and review notes. Runs after `verifier`.              |
| `gc_agent` | Specialized agent for garbage collection: refactors drift, reduces technical debt.              |

Each role may be assigned its own `ProviderConfig` (different model, different temperature,
different context budget). If a role has no specific provider config, `providers.default` is used.

Custom roles can be defined by adding a `## <role_name>` section to `HARNESS.md` and listing the
role in `routing.pipeline` or a routing rule.

#### 4.1.5 Memory Entry

An entry in the three-layer memory system.

Fields:

- `id` (string) — UUID.
- `layer` (enum) — `episodic`, `semantic`, `procedural`.
- `project_id` (string) — scoped to the project.
- `issue_id` (string, optional) — scoped to a specific issue (episodic only).
- `task_type` (string, optional) — scoped to a task type (procedural).
- `content` (string) — free-text or structured JSON.
- `embedding` (float vector, optional) — generated for semantic search.
- `tags` (list of strings)
- `source` (enum) — `agent_written`, `auto_extracted`, `consolidated`, `validation_result`.
- `created_at` (timestamp)
- `expires_at` (timestamp or null) — null for semantic and procedural; TTL-bounded for episodic.
- `relevance_score` (float 0.0–1.0) — computed at retrieval time, not stored.

#### 4.1.6 Knowledge Node

A node in the codebase knowledge graph.

Node types:

- `file` — source file.
- `symbol` — exported function, class, type, or variable.
- `module` — logical package or module.
- `layer` — architectural layer as defined in `knowledge.layer_definitions`.
- `doc` — document from a Doc Store.
- `chunk` — generic text chunk for file types not parsed by the AST layer.

Fields:

- `id` (string) — stable hash of `<project_id>/<relative_path>[#<symbol_name>]`.
- `type` (node type enum)
- `project_id` (string)
- `path` (string) — relative path in workspace or doc store.
- `name` (string)
- `summary` (string) — LLM-generated or statically extracted summary.
- `content` (string) — raw content snippet for retrieval injection.
- `embedding` (float vector)
- `outgoing_edges` (list of KnowledgeEdge)
  - `target_id` (string)
  - `edge_type` (enum: `imports`, `calls`, `extends`, `implements`, `references_doc`)
- `layer_id` (string, optional)
- `language` (string, optional) — source language.
- `line_start` / `line_end` (integer, optional) — for symbol nodes.
- `last_indexed_at` (timestamp)
- `checksum` (string) — content hash for incremental indexing.

#### 4.1.7 Knowledge Edge

A directed edge in the codebase knowledge graph.

Fields:

- `from_id` (string)
- `to_id` (string)
- `edge_type` (enum: `imports`, `calls`, `extends`, `implements`, `references_doc`, `depends_on`)
- `weight` (float, optional) — for ranking edges by importance.

#### 4.1.8 Harness Rule

An architectural invariant that the Harness Enforcer checks.

Fields:

- `id` (string) — stable identifier used in logs and audit trail.
- `name` (string) — human-readable name.
- `category` (enum) — `architecture`, `naming`, `file_size`, `dependency`, `test_coverage`,
  `custom`.
- `severity` (enum) — `warning`, `error`, `blocking`.
  - `warning` — injected into agent context; does not block dispatch.
  - `error` — injected as a mandatory fix; does not block dispatch by default.
  - `blocking` — may halt dispatch if `enforcement.blocking_violations_halt_dispatch` is true.
- `check` (string) — shell command executed in the workspace root. Exit code 0 = pass.
- `fix_hint` (string) — remediation message injected inline into agent context on violation.
- `auto_fix` (boolean) — if true, violations trigger automatic GC task creation.

#### 4.1.9 Doc Ref

A reference to a document in a Doc Store backend.

Fields:

- `id` (string)
- `title` (string)
- `store_id` (string) — references `docs.stores[].id`.
- `path_or_id` (string) — path or document ID within the backend.
- `content_hash` (string) — for change detection.
- `tags` (list of strings)
- `embedding` (float vector, optional) — for semantic search.
- `last_synced_at` (timestamp)

#### 4.1.10 Audit Event

A node in the provenance graph.

Fields:

- `id` (string) — UUID.
- `timestamp` (timestamp, UTC)
- `project_id` (string)
- `issue_id` (string, optional)
- `session_id` (string, optional) — `<run_attempt_id>-<turn_index>`.
- `agent_role` (string, optional)
- `event_type` (string) — see Section 17.2 for the full event type registry.
- `payload` (map) — event-specific data.
- `parent_event_id` (string, optional) — causal link for provenance graph traversal.
- `duration_ms` (integer, optional) — for events that span time.

#### 4.1.11 Orchestrator Runtime State

Single authoritative in-memory state owned by the orchestrator. Identical to Symphony Section
4.1.8 with additions:

- `enforcer_status` (enum) — `clear`, `violations_present`, `blocked`.
- `knowledge_index_status` (enum) — `ready`, `indexing`, `stale`, `disabled`.
- `pending_gc_tasks` (integer) — count of unprocessed GC task issues.
- `doc_store_sync_status` (map `store_id -> SyncStatus`)

#### 4.1.12 Run Attempt

One execution attempt for one issue. Fields identical to Symphony Section 4.1.5.

Additional fields:

- `pipeline` (list of strings) — agent roles in execution order for this attempt.
- `pipeline_index` (integer) — current role index in the pipeline.
- `validation_results` (list of ValidationResult) — results from last Validation Pipeline run.

### 4.2 Normalization Rules

- **Issue ID** — stable tracker ID; used as map key.
- **Issue Identifier** — human-readable key; used in logs and workspace naming.
- **Workspace Key** — sanitized identifier: only `[A-Za-z0-9._-]` permitted; other characters
  replaced with `_`.
- **Issue State** — compared after `strings.ToLower()`.
- **Session ID** — `<run_attempt_id>-<turn_index>` (zero-padded turn index, e.g., `abc123-004`).
- **Knowledge Node ID** — `sha256(<project_id> + "/" + <relative_path> + "#" + <symbol_or_empty>)`,
  truncated to 16 hex characters.
- **Memory Scope**:
  - Episodic: `<project_id>/<issue_id>`
  - Semantic: `<project_id>`
  - Procedural: `<project_id>/<task_type>`
- **Task Type** — lowercase, one of the built-in values or a custom string from labels.

---

## 5. Harness Definition (`HARNESS.md`)

### 5.1 Discovery and Path Resolution

Precedence:

1. `--harness <path>` CLI flag.
2. `CONDUCTOR_HARNESS_PATH` environment variable.
3. `HARNESS.md` in the current working directory.
4. Doc Store reference: a `HARNESS.md` stored in any configured Doc Store backend (resolved after
   local options are exhausted).

If no `HARNESS.md` is found, startup fails with a `missing_harness_file` error.

### 5.2 File Format

`HARNESS.md` is a Markdown file with optional YAML front matter.

```
---
<YAML front matter>
---

## planner

<Liquid template for the planner role>

## coder

<Liquid template for the coder role>

## verifier

<Liquid template for the verifier role>

## reviewer

<Liquid template for the reviewer role>
```

Parsing rules:

- If the file starts with `---`, parse lines until the next `---` as YAML front matter.
- Remaining content is the template body.
- The template body is split into sections by top-level `## <role>` headings.
- Each section becomes the prompt template for the named role.
- If no `##` sections are present, the entire body is assigned to the `coder` role.
- Front matter must decode to a YAML map; non-map YAML is a `harness_parse_error`.
- Unknown front matter keys are ignored (forward compatibility).

Returned Harness Definition:

- `config`: front matter root object.
- `prompt_templates`: map from role name to trimmed Markdown section body.

### 5.3 Front Matter Schema

Top-level keys (all optional unless noted):

```
project        ← required
tracker        ← required for dispatch
polling
workspace
hooks
providers      ← required (at least default)
routing
knowledge
memory
docs
harness_rules
enforcement
validation
agent
server
```

Unknown top-level keys are ignored.

#### 5.3.1 `project` (object, required)

- `id` (string, required) — stable project identifier used as memory and knowledge scope key.
- `name` (string, optional) — human-readable display name.
- `description` (string, optional)

#### 5.3.2 `tracker` (object)

- `kind` (string) — required. Supported values: `linear`, `github`, `jira`, `plane`, `shortcut`.
- `endpoint` (string) — API endpoint. Default: tracker-kind-specific.
- `api_key` (string or `$VAR`) — authentication token. Canonical env vars:
  - Linear: `LINEAR_API_KEY`
  - GitHub: `GITHUB_TOKEN`
  - Jira: `JIRA_API_TOKEN`
  - Plane: `PLANE_API_KEY`
  - Shortcut: `SHORTCUT_API_TOKEN`
- `project_slug` (string) — required when kind is `linear` or `plane`.
- `project_id` (string) — required when kind is `github` (repository `owner/repo`), `jira`
  (project key), or `shortcut` (project ID).
- `active_states` (list of strings) — default: `["Todo", "In Progress"]`.
- `terminal_states` (list of strings) — default: `["Done", "Cancelled", "Canceled", "Closed",
"Duplicate"]`.
- `extra` (map, optional) — tracker-specific parameters (e.g., Jira JQL filter clauses).

#### 5.3.3 `polling` (object)

- `interval_ms` (integer) — default `30000`. Applied dynamically; changes take effect on the
  next tick without restart.

#### 5.3.4 `workspace` (object)

- `root` (path string or `$VAR`) — default: `<os.TempDir()>/conductor_workspaces`.
  `~` and `$VAR` are expanded. Strings without path separators are used as relative roots.
- `repos` (list of WorkspaceRepo, optional) — for multi-repo workspaces.

WorkspaceRepo fields:

- `name` (string, required) — subdirectory name within the workspace.
- `url` (string or `$VAR`, required) — repository clone URL.
- `branch_template` (string, optional) — Liquid template for branch name
  (variables: `issue`, `attempt`). Default: `conductor/{{ issue.identifier | downcase }}`.
- `sparse_checkout` (list of glob strings, optional) — paths for sparse checkout.
- `depth` (integer, optional) — clone depth. Default: full clone.

#### 5.3.5 `hooks` (object)

- `after_create` (shell script, optional) — runs once when a workspace directory is newly
  created. Failure aborts workspace creation.
- `before_run` (shell script, optional) — runs before each agent attempt. Failure aborts the
  attempt.
- `after_run` (shell script, optional) — runs after each attempt regardless of outcome. Failure
  is logged and ignored.
- `after_turn` (shell script, optional) — runs after each individual agent turn. Receives env
  vars: `CONDUCTOR_ISSUE_ID`, `CONDUCTOR_ISSUE_IDENTIFIER`, `CONDUCTOR_TURN_INDEX`,
  `CONDUCTOR_AGENT_ROLE`, `CONDUCTOR_TURN_STATUS` (`success`|`failed`|`timeout`).
- `before_remove` (shell script, optional) — runs before workspace deletion. Failure is logged
  and ignored; cleanup proceeds.
- `on_harness_violation` (shell script, optional) — runs when a HarnessRule violation is
  detected. Receives: `CONDUCTOR_RULE_ID`, `CONDUCTOR_RULE_SEVERITY`, `CONDUCTOR_WORKSPACE`.
- `timeout_ms` (integer) — applied to all hooks. Default: `60000`. Non-positive values fall back
  to default.

Hook execution contract:

- All hooks execute with `workspace_path` as `cwd`.
- On POSIX: `bash -lc <script>`.
- On Windows: `cmd /C <script>`.
- Stdout and stderr are captured and appended to the issue's audit log.

#### 5.3.6 `providers` (object)

Map of `agent_role -> ProviderConfig`. The special key `default` is used when a role has no
specific config.

```yaml
providers:
  default:
    provider: openrouter
    model: anthropic/claude-sonnet-4-5
    api_key: $OPENROUTER_API_KEY
    context_budget: 150000
    compaction_strategy: summarize
  planner:
    provider: anthropic
    model: claude-opus-4-5
    api_key: $ANTHROPIC_API_KEY
    context_budget: 100000
  verifier:
    provider: openai
    model: o3-mini
    api_key: $OPENAI_API_KEY
    max_tokens: 4096
  gc_agent:
    provider: ollama
    model: qwen2.5-coder:32b
    base_url: http://localhost:11434
```

#### 5.3.7 `routing` (object)

- `pipeline` (list of strings) — default pipeline applied when no rule matches.
  Default: `[planner, coder, verifier]`.
- `rules` (list of RoutingRule) — ordered list of conditional routing rules.

RoutingRule fields:

- `when` (map of match conditions):
  - `labels` (list of strings) — issue must have ALL listed labels.
  - `any_label` (list of strings) — issue must have AT LEAST ONE listed label.
  - `task_type` (string) — matches classified task type.
  - `state` (string) — matches current tracker state name.
  - `title_matches` (regex string) — matches issue title.
  - `complexity` (enum) — matches estimated complexity.
- `pipeline` (list of strings) — pipeline to use when the rule matches.

Rules are evaluated in order. First match wins. If no rule matches, `routing.pipeline` is used.

Example:

```yaml
routing:
  pipeline: [planner, coder, verifier, reviewer]
  rules:
    - when: { labels: [docs] }
      pipeline: [coder, reviewer]
    - when: { task_type: investigation }
      pipeline: [planner, coder]
    - when: { any_label: [gc_task, refactor] }
      pipeline: [gc_agent]
    - when: { complexity: trivial }
      pipeline: [coder, verifier]
```

#### 5.3.8 `knowledge` (object)

- `enabled` (boolean) — default `true`.
- `index_on_startup` (boolean) — default `true`. Runs a full index on service start.
- `watch_for_changes` (boolean) — default `true`. Uses fsnotify for incremental re-indexing.
- `include_patterns` (list of glob strings) — files to index. Default: language-specific defaults.
- `exclude_patterns` (list of glob strings) — files to exclude.
  Default: `["**/node_modules/**", "**/.git/**", "**/vendor/**", "**/__pycache__/**"]`.
- `embedding_provider` (string) — provider ID from `providers` map used for embeddings.
  Default: `default`.
- `embedding_model` (string) — embedding model identifier. Provider-specific default:
  - `openai`: `text-embedding-3-small`
  - `ollama`: `nomic-embed-text`
  - `openrouter`: delegates to configured model (use a model with embedding support)
- `store_backend` (enum) — `sqlite_vec` (default), `qdrant`.
- `store_path` (path or `$VAR`) — for `sqlite_vec`. Default:
  `<workspace_root>/.conductor/<project_id>/knowledge.db`.
- `qdrant_url` (string) — required if `store_backend == qdrant`.
- `qdrant_collection` (string) — default: `conductor_<project_id>`.
- `top_k` (integer) — chunks returned per RAG query. Default: `10`.
- `use_ast` (boolean) — enable tree-sitter AST parsing for dependency graph extraction.
  Default: `true`. Set to `false` for faster indexing without dependency edges.
- `layer_definitions` (map `layer_name -> list of glob patterns`) — defines architectural layers
  for enforcement. Example:
  ```yaml
  layer_definitions:
    types: ["src/types/**", "pkg/types/**"]
    config: ["src/config/**"]
    repo: ["src/repo/**", "internal/repo/**"]
    service: ["src/services/**", "internal/service/**"]
    runtime: ["src/runtime/**", "cmd/**"]
    ui: ["src/ui/**", "web/**"]
  ```

#### 5.3.9 `memory` (object)

- `enabled` (boolean) — default `true`.
- `store_backend` (enum) — `sqlite` (default), `postgres`.
- `store_path` (path or `$VAR`) — for `sqlite`. Default:
  `<workspace_root>/.conductor/<project_id>/memory.db`.
- `episodic_ttl_days` (integer) — TTL for episodic memories. Default: `90`.
- `max_context_memories` (integer) — max memories injected per turn. Default: `5`.
- `consolidation_enabled` (boolean) — promote episodic → semantic via background LLM synthesis.
  Default: `true`.
- `consolidation_interval_hours` (integer) — consolidation run cadence. Default: `24`.
- `consolidation_provider` (string) — provider used for consolidation synthesis. Default:
  `default`.

#### 5.3.10 `docs` (object)

- `enabled` (boolean) — default `true`.
- `stores` (list of DocStoreConfig)

DocStoreConfig fields:

- `id` (string, required) — unique identifier for this store.
- `backend` (enum, required) — `local_fs`, `git_repo`, `s3`, `notion`, `confluence`, `custom`.
- `path_or_url` (string or `$VAR`, required) — location of the store.
- `auth` (map, optional) — backend-specific authentication:
  - `git_repo`: `{token: $TOKEN}` or `{ssh_key: $KEY_PATH}`.
  - `s3`: `{access_key_id: $ID, secret_access_key: $KEY, region: us-east-1}`.
  - `notion`: `{api_key: $KEY}`.
  - `confluence`: `{email: $EMAIL, token: $TOKEN, base_url: https://org.atlassian.net}`.
- `sync_interval_minutes` (integer) — default `60`.
- `include_patterns` (list of glob strings) — files/pages to index from this store.
- `tags` (list of strings) — applied to all documents from this store for filtering.

#### 5.3.11 `harness_rules` (list of HarnessRule)

See Section 4.1.8 for the HarnessRule schema.

Example:

```yaml
harness_rules:
  - id: no-cross-layer-imports
    name: "No cross-layer imports (higher layers may not import lower layers directly)"
    category: dependency
    severity: blocking
    check: "conductor lint check-layers"
    fix_hint: "Move the shared type to the 'types' layer or introduce an interface. See docs/architecture.md."
    auto_fix: false

  - id: max-file-lines
    name: "Source files must not exceed 500 lines"
    category: file_size
    severity: warning
    check: |
      FILES=$(find . -name '*.go' -not -path './.conductor/*' | xargs wc -l 2>/dev/null | awk '$1 > 500 {print $2}')
      [ -z "$FILES" ] && exit 0 || (echo "$FILES" && exit 1)
    fix_hint: "Split this file into smaller modules. Each file should have a single clear responsibility."
    auto_fix: true

  - id: require-tests
    name: "Every new exported function must have a corresponding test"
    category: test_coverage
    severity: error
    check: "go test ./... -count=1"
    fix_hint: "Write unit tests for all exported functions you added or modified."
    auto_fix: false
```

#### 5.3.12 `enforcement` (object)

- `enabled` (boolean) — default `true`.
- `gc_schedule_cron` (string, optional) — standard cron expression for automated GC task
  creation. Default: null (disabled). Example: `"0 2 * * *"` (2am daily).
- `gc_issue_label` (string) — label applied to auto-created GC issues. Default: `gc_task`.
- `gc_issue_state` (string) — initial state for GC issues. Default: first value of
  `tracker.active_states`.
- `drift_check_on_dispatch` (boolean) — run all harness rules before each dispatch. Default: `true`.
- `blocking_violations_halt_dispatch` (boolean) — if true, any `blocking` severity violation
  prevents new dispatches until resolved. Default: `false`.

#### 5.3.13 `validation` (object)

- `enabled` (boolean) — default `true`.
- `run_after_turn` (boolean) — execute validation pipeline after each agent turn. Default: `true`.
- `fail_on_severity` (enum or null) — if a check at or above this severity fails, the turn is
  failed. Values: `warning`, `error`. Default: `null` (validation never fails the turn).
- `inject_results_into_context` (boolean) — inject validation output into the next turn's prompt.
  Default: `true`.
- `timeout_ms` (integer) — total timeout for all checks per run. Default: `300000` (5m).
- `checks` (list of ValidationCheck)

ValidationCheck fields:

- `id` (string, required)
- `name` (string, required)
- `command` (string, required) — shell command run in workspace root.
- `timeout_ms` (integer) — per-check timeout. Default: `60000`.
- `severity` (enum) — `info`, `warning`, `error`. Default: `warning`.
- `output_max_bytes` (integer) — truncate captured output to this size before injection.
  Default: `4096`.

#### 5.3.14 `agent` (object)

- `max_concurrent_agents` (integer) — global concurrency cap. Default: `10`.
- `max_turns` (integer) — max agent turns per worker session. Default: `20`.
- `max_retry_backoff_ms` (integer) — cap for exponential retry backoff. Default: `300000`.
- `max_concurrent_agents_by_state` (map `state_name -> positive integer`) — per-state concurrency
  cap. State keys are normalized to lowercase. Default: `{}`.

#### 5.3.15 `server` (object)

- `port` (integer) — HTTP server port. Default: `8080`. `0` for ephemeral port.
- `host` (string) — bind address. Default: `127.0.0.1`.
- `cors_origins` (list of strings) — allowed CORS origins. Default: `["http://localhost:5173"]`
  (SvelteKit dev server).
- `enable_api` (boolean) — enable REST API endpoints. Default: `true`.
- `enable_dashboard` (boolean) — serve the embedded SvelteKit dashboard. Default: `true`.

---

## 6. Configuration Layer

### 6.1 Source Precedence

1. CLI flags (highest priority).
2. Environment variables (via Viper).
3. YAML front matter values.
4. `$VAR` indirection within selected YAML values.
5. Built-in defaults (lowest priority).

### 6.2 Value Coercion

- Path fields: `~` expansion, `$VAR` expansion, OS separator normalization.
- Integer fields: string integers are accepted (e.g., `"30000"` for `interval_ms`).
- URI fields: no expansion (passed as-is).
- Shell command fields: no expansion (executed via shell).

### 6.3 Dynamic Reload

- Conductor watches `HARNESS.md` (and its Doc Store equivalent if remote) for changes using
  fsnotify.
- On change: re-read, re-validate, re-apply. Does not restart.
- Changes apply to: future dispatch, retry scheduling, reconciliation, hook execution, agent
  launches, harness rule enforcement, polling cadence, concurrency limits.
- In-flight agent sessions are not interrupted by config changes.
- Invalid reloads: keep the last known good effective config; emit `config_reload_failed` audit
  event and log a structured error.
- Implementations must also re-validate defensively before dispatch in case watch events are missed.

### 6.4 Startup Validation

Required checks before the scheduling loop starts:

- `project.id` is present and non-empty.
- `tracker.kind` is present and in the supported tracker list.
- `tracker.api_key` resolves to a non-empty string after `$VAR` substitution.
- `tracker.project_slug` / `tracker.project_id` is present when required by the tracker kind.
- `providers.default` is present and its `api_key` resolves.
- `providers.default.provider` is a supported provider.
- `knowledge.store_backend` is valid.
- `memory.store_backend` is valid.
- Each `docs.stores[].backend` is in the supported backend list.
- Each role referenced in `routing.pipeline` and `routing.rules[].pipeline` has a template in
  the parsed `prompt_templates` map.

Startup validation failure: log the error, exit non-zero.

Per-tick dispatch validation: re-run a subset of the above (tracker connectivity, provider key
presence, harness parseable). On failure: skip dispatch for that tick; keep reconciliation active.

---

## 7. Provider Adapter Layer

### 7.1 Abstract Interface

The Provider Adapter exposes a stable interface that all LLM provider implementations conform to.
The orchestrator, Agent Router, and Memory Manager interact only with this interface.

```go
// Go pseudocode — normative intent, not exact syntax
type ProviderAdapter interface {
    // Initialize a new agent session for a workspace
    CreateSession(ctx context.Context, cfg ProviderConfig, workspace string) (SessionHandle, error)

    // Start a new agent turn with a prompt and injected tools
    StartTurn(ctx context.Context, s SessionHandle, prompt string, tools []ToolSpec) (TurnStream, error)

    // Continue an existing turn on the same session thread
    ContinueTurn(ctx context.Context, s SessionHandle, prompt string) (TurnStream, error)

    // Terminate the session and release resources
    EndSession(ctx context.Context, s SessionHandle) error

    // Return token usage for the last completed turn
    GetUsage(s SessionHandle) TokenUsage
}

type TurnStream interface {
    // Channel of agent events
    Events() <-chan AgentEvent

    // Blocks until the turn is complete
    Wait() TurnResult
}
```

### 7.2 Supported Adapters

#### 7.2.1 `openrouter`

- Base URL: `https://openrouter.ai/api/v1` (or `base_url` override)
- Auth: `Authorization: Bearer <api_key>`
- Protocol: OpenAI Chat Completions API (fully compatible)
- Streaming: Server-Sent Events
- Tool use: OpenAI function calling format
- Required headers: `HTTP-Referer: <server.host>`, `X-Title: Conductor`
- Model selection: any model available on OpenRouter (format: `provider/model-name`)
- Note: OpenRouter enables mixing providers per role cheaply — e.g., Anthropic for planning,
  Google for coding, OpenAI for verification — all through one API key.

#### 7.2.2 `anthropic`

- Base URL: `https://api.anthropic.com/v1` (or `base_url` override)
- Auth: `x-api-key: <api_key>`
- API version header: `anthropic-version: 2023-06-01`
- Protocol: Anthropic Messages API
- Streaming: Server-Sent Events
- Tool use: Anthropic `tool_use` / `tool_result` content blocks
- Minimum API version: `2023-06-01`

#### 7.2.3 `openai`

- Base URL: `https://api.openai.com/v1` (or `base_url` override)
- Auth: `Authorization: Bearer <api_key>`
- Protocol: OpenAI Chat Completions API
- Streaming: Server-Sent Events
- Tool use: OpenAI function calling format

#### 7.2.4 `ollama`

- Base URL: `http://localhost:11434` (or `$OLLAMA_BASE_URL` or `base_url` override)
- Auth: none (local by default)
- Protocol: Ollama `/api/chat` (OpenAI-compatible mode also supported via `/v1/chat/completions`)
- Streaming: JSON stream (`stream: true`)
- Tool use: Ollama tool calling (OpenAI format)
- Note: Suitable for air-gapped environments or cost-sensitive GC agent roles.

#### 7.2.5 `lm_studio`

- Base URL: `http://localhost:1234/v1` (or `$LM_STUDIO_BASE_URL` or `base_url` override)
- Auth: `lm-studio` (ignored by local server)
- Protocol: OpenAI-compatible Chat Completions
- Tool use: OpenAI function calling format

#### 7.2.6 `custom`

- Requires `base_url` in ProviderConfig.
- Uses OpenAI Chat Completions format.
- Enables integration with vLLM, TGI, Together AI, Groq, Mistral AI, Cohere, or any
  OpenAI-compatible server.

### 7.3 Conductor Tool Injection

On every session creation, the Provider Adapter registers the following built-in tools that the
agent can call. These are injected via the provider's native tool-use mechanism.

| Tool Name                    | Description                                                                                                                                     |
| ---------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------- |
| `conductor_tracker_query`    | Execute a read query against the configured issue tracker. Auth is injected by Conductor; the agent never sees the token.                       |
| `conductor_tracker_mutate`   | Execute a write mutation against the tracker (e.g., update state, add comment). Requires approval if `approval_policy` is `review_destructive`. |
| `conductor_knowledge_search` | Semantic + structural search over the codebase knowledge graph.                                                                                 |
| `conductor_doc_search`       | Semantic search over the Doc Store.                                                                                                             |
| `conductor_memory_read`      | Retrieve relevant memories for a given query.                                                                                                   |
| `conductor_memory_write`     | Persist a new memory entry (episodic or semantic).                                                                                              |
| `conductor_harness_check`    | Explicitly run a named HarnessRule check and return its result.                                                                                 |
| `conductor_validation_run`   | Trigger the Validation Pipeline on demand and return results.                                                                                   |

Additional tools can be registered via plugin (see Section 19.5).

### 7.4 Context Budget and Compaction

If `context_budget` is set:

1. The adapter tracks cumulative prompt + completion tokens for the session.
2. At 80% of `context_budget`: emit `ContextWarning` event to orchestrator.
3. At 95% of `context_budget`:
   - `compaction_strategy: summarize` — invoke the provider with a summarization prompt:
     `"Summarize the key decisions, code changes, and open questions from this conversation in
under 500 words."` Restart context with the summary as a system message. Log a
     `ContextCompacted` audit event.
   - `compaction_strategy: sliding_window` — drop the oldest N message pairs (keep system
     message and last K turns). Log a `ContextSlid` audit event.
   - `compaction_strategy: none` — emit `ContextLimitApproaching` event; continue.

The compaction target (summary or window) is computed to bring usage to approximately 60% of
`context_budget`, leaving headroom for the next turn.

---

## 8. Knowledge Engine

### 8.1 Overview

The Knowledge Engine indexes the project codebase and any configured Doc Stores into a persistent
knowledge graph, and serves hybrid semantic + structural queries for context injection.

It is a first-class background service — not a one-shot tool — that maintains a live model of the
codebase and updates incrementally as files change.

### 8.2 Indexing Pipeline

```
Discovery → Parsing → Summarization → Embedding → Graph Construction → Layer Assignment → Persistence
```

**Stage 1: Discovery**

Recursive filesystem traversal of all workspace repos, filtered by `include_patterns` and
`exclude_patterns`. File checksums are compared against the stored index for incremental updates.

**Stage 2: Parsing**

For each file, select the appropriate parser:

- **AST parser** (when `knowledge.use_ast == true`): extract symbols (functions, types, classes),
  imports, and dependency edges using tree-sitter. Supported languages via go-tree-sitter grammars:
  Go, TypeScript, JavaScript, Python, Rust, C, C++, Java, Ruby, Elixir.
- **Line chunker** (fallback): split into overlapping 200-line chunks with 50-line overlap.
  Used for unsupported languages, config files, and documentation.

**Stage 3: Summarization**

For each parsed unit (symbol or chunk), generate a 1–3 sentence summary using the configured
`embedding_provider`. Summaries are cached; only regenerated when file content changes.

Summarization prompt template (internal, not user-configurable):

```
Summarize the following code in 1-3 sentences. Focus on what it does, not how.
Do not repeat the code. Be specific about inputs, outputs, and side effects.

Path: {{ path }}
Content:
{{ content }}
```

**Stage 4: Embedding**

Generate embeddings for the concatenation of `summary + "\n" + content_snippet` (truncated to
the embedding model's context limit). Embeddings are stored in the vector store backend.

**Stage 5: Graph Construction**

Build directed dependency edges from parsed imports and symbol references. Edge types:
`imports`, `calls`, `extends`, `implements`.

**Stage 6: Layer Assignment**

Match each node's path against `knowledge.layer_definitions` glob patterns. Assign `layer_id`.
Nodes matching no pattern are in the `unknown` layer.

**Stage 7: Persistence**

Upsert nodes and edges in the configured `store_backend`. For `sqlite_vec`: store node metadata
in a relational table and embeddings in a virtual FTS5/vec table. For `qdrant`: store metadata
as payload fields and embeddings as the vector.

### 8.3 Incremental Re-indexing

When `knowledge.watch_for_changes == true`:

- fsnotify watches all workspace repo directories.
- On `Write` or `Create` event: re-parse and re-embed the affected file.
- On `Remove` event: delete the file's nodes and edges from the store.
- Batch events within a 500ms debounce window to avoid thrashing on git checkouts.

### 8.4 Hybrid Search

Query types:

| Query Type     | Implementation                                                |
| -------------- | ------------------------------------------------------------- |
| **Semantic**   | Embedding similarity search against all node embeddings.      |
| **Structural** | SQL/filter query on node type, layer, language, path pattern. |
| **Dependency** | Graph traversal: "what imports X" or "what does X import".    |
| **Hybrid**     | Semantic search results re-ranked by structural relevance.    |

`conductor_knowledge_search` tool schema:

```json
{
  "query": "string — natural language query",
  "types": ["file", "symbol", "chunk", "doc"],
  "layers": ["types", "service"],
  "path_pattern": "glob string, optional",
  "include_dependencies": "boolean — include direct dependency nodes",
  "top_k": "integer — default 10"
}
```

### 8.5 Context Injection

Before each agent turn, the Knowledge Engine runs:

1. Hybrid search with the issue title + description as the seed query.
2. Returns `top_k` most relevant nodes.
3. Formats results as a structured context block:

```markdown
## Codebase Context

### Relevant Files and Symbols

**[service] src/services/auth.go** (AuthService)
Handles JWT token validation and session management. Exposes `ValidateToken(ctx, token)`.
Dependencies: types.User, config.JWTConfig

**[types] src/types/user.go** (User, UserID)
Core user entity. Fields: ID, Email, Roles, CreatedAt.
Imported by: auth.go, repo/user.go, handlers/user.go

...
```

Formatted block is truncated to respect the agent's `context_budget`.

### 8.6 Layer Violation Detection

```go
// Returns all layer invariant violations in the workspace
func (ke *KnowledgeEngine) CheckLayerViolations(workspacePath string) []LayerViolation
```

A LayerViolation is produced when a node in layer `A` has a dependency edge to a node in layer
`B`, and the harness rules prohibit that direction.

Layer constraint direction is derived from the order of keys in `knowledge.layer_definitions`:
lower-indexed layers are "lower" in the stack. By default, higher layers may import lower layers
but not vice versa. Custom rules in `harness_rules` override this default.

---

## 9. Memory Manager

### 9.1 Overview

The Memory Manager maintains a three-layer persistent store that survives service restarts and
allows agents to access relevant knowledge from past sessions.

This directly addresses the primary failure mode of stateless agent orchestration: agents repeat
the same mistakes because they have no institutional memory.

### 9.2 The Three Layers

```
┌─────────────────────────────────────────────────────┐
│                   Memory Manager                      │
│                                                       │
│  ┌─────────────────────────────────────────────────┐  │
│  │ Episodic Memory                                 │  │
│  │ What happened in past sessions for this issue   │  │
│  │ Scope: project_id / issue_id                    │  │
│  │ TTL: configurable (default 90 days)             │  │
│  └─────────────────────────────────────────────────┘  │
│                          ↓ consolidation               │
│  ┌─────────────────────────────────────────────────┐  │
│  │ Semantic Memory                                 │  │
│  │ What the project has learned                    │  │
│  │ Scope: project_id                               │  │
│  │ TTL: none (until explicitly retired)            │  │
│  └─────────────────────────────────────────────────┘  │
│                          ↓ synthesis                   │
│  ┌─────────────────────────────────────────────────┐  │
│  │ Procedural Memory                               │  │
│  │ How to do things well in this project           │  │
│  │ Scope: project_id / task_type                   │  │
│  │ TTL: none (until superseded)                    │  │
│  └─────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────┘
```

**Episodic** — session-scoped facts. Examples:

- _"Attempt 2 for ABC-123: tried using `pkg/auth.ValidateJWT()` but it was removed in PR #89.
  Use `pkg/auth.VerifyToken()` instead."_
- _"PR for ABC-123 was rejected: missing error handling in the `/health` endpoint path."_

**Semantic** — project-wide patterns. Examples:

- _"This project follows the Repository pattern. All database access goes through a `Repo`
  interface in `internal/repo/`. Never query the DB directly from service or handler layers."_
- _"Integration tests use the `testdb.New(t)` helper, not mocks. Unit tests use mocks."_

**Procedural** — task-type recipes. Examples:

- _"To add a new REST endpoint: (1) Define request/response types in `src/types/`, (2) implement
  handler in `src/handlers/`, (3) register route in `src/routes/index.go`, (4) write integration
  test in `tests/handlers/`. Run `make test` to verify."_

### 9.3 Memory Retrieval

Before each agent turn, the Memory Manager:

1. **Episodic query**: retrieve all episodic memories for `project_id/issue_id`, sorted by
   recency. Limit to 3 most recent.
2. **Semantic query**: embedding similarity search over semantic memories for `project_id` using
   the turn's intent as seed. Return top 3.
3. **Procedural query**: retrieve procedural memory for `project_id/task_type`. Return 1.
4. **Rank and deduplicate**: rank by relevance score; apply `max_context_memories` limit.
5. **Format and inject** as `## Relevant Memory` section in the prompt.

Formatted injection:

```markdown
## Relevant Memory

### From previous sessions on this issue

- [2 sessions ago] Tried `pkg/auth.ValidateJWT()` — removed in PR #89. Use `pkg/auth.VerifyToken()`.
- [last session] PR rejected: missing error handling on the `/health` path.

### Project knowledge

- This project uses the Repository pattern. Never access the DB outside `internal/repo/`.
- Integration tests use `testdb.New(t)`, not mocks.

### How to implement a feature (your task type)

1. Define types in `src/types/`
2. Implement handler in `src/handlers/`
3. Register route in `src/routes/index.go`
4. Write integration test, run `make test`
```

### 9.4 Memory Writing

Memory entries are created through three paths:

1. **Agent-initiated** — agent calls `conductor_memory_write` tool directly.
2. **Auto-extraction** — after each turn, the orchestrator parses the agent's output for
   patterns matching known memory templates (errors encountered, APIs used, decisions made).
3. **Post-session consolidation** — after each session ends, a summarization call synthesizes
   the session's episodic entries into a consolidated episodic memory.

Auto-extraction patterns (configurable):

- Lines starting with `MEMORY:` in agent output are automatically saved as semantic memory.
- Structured `<memory type="semantic">...</memory>` XML blocks in agent output are parsed.
- Validation failures are automatically saved as episodic memories with the issue and check ID.

### 9.5 Consolidation

The consolidation process runs every `memory.consolidation_interval_hours` hours:

1. Group episodic memories for `project_id` by semantic similarity (embedding clustering).
2. For each cluster with 3+ members: invoke the consolidation provider with a synthesis prompt.
3. Save the synthesis as a semantic memory entry.
4. Group successful turn sequences by `task_type` and synthesize procedural memories.
5. Delete episodic entries that have exceeded `episodic_ttl_days`.

---

## 10. Doc Store Manager

### 10.1 Overview

The Doc Store Manager allows project documentation to live outside the codebase repository.

**Motivation**: specs, ADRs, design documents, API contracts, and integration guides are
frequently useful across multiple projects, require different access controls than code, or are
managed by tools (Notion, Confluence) that are better suited to human collaboration. Coupling
them to the code repo creates friction and prevents sharing.

Conductor treats Doc Store documents as first-class knowledge sources: they are indexed alongside
the codebase and injected into agent context via the same hybrid RAG mechanism.

### 10.2 Supported Backends

| Backend      | Use Case                                                        | Auth                          |
| ------------ | --------------------------------------------------------------- | ----------------------------- |
| `local_fs`   | Local directory; useful for specs co-located with config        | None                          |
| `git_repo`   | Separate Git repository for specs/docs; versioned independently | Token or SSH key              |
| `s3`         | Object storage; team-scale, cloud-friendly                      | AWS credentials or compatible |
| `notion`     | Notion Database or Page tree; team collaboration                | Notion Integration token      |
| `confluence` | Confluence Space; enterprise documentation                      | API token + base URL          |
| `custom`     | Plugin-defined backend                                          | Plugin-defined                |

### 10.3 Synchronization Contract

Each DocStoreConfig defines a sync interval. The Doc Store Manager:

1. On startup: run an initial sync for all configured stores (unless initial sync was recent).
2. On sync: fetch document list from the backend; compare checksums; download changed documents.
3. Re-index changed documents in the Knowledge Engine.
4. On sync failure: log a structured warning; retain the last successfully synced documents.

### 10.4 `HARNESS.md` from a Doc Store

If `HARNESS.md` is stored in a Doc Store:

- Specify via CLI flag: `conductor start --harness docs://specs/HARNESS.md`
- The Harness Loader resolves the `docs://` URI to the configured store.
- The file is synced and cached locally before parsing.
- Changes are detected on the next sync interval and trigger a config reload.

### 10.5 Agent Integration

Agents search the Doc Store via `conductor_doc_search`:

```json
{
  "query": "how to configure database connection pooling",
  "stores": ["specs", "architecture-decisions"],
  "tags": ["database", "config"],
  "top_k": 5
}
```

Results are injected as `## Relevant Documentation` in the agent prompt.

---

## 11. Harness Enforcer

### 11.1 Overview

The Harness Enforcer is the operational implementation of architectural governance — the part of
Harness Engineering that the OpenAI team described but Symphony did not specify.

It runs harness rules on three triggers:

1. **Pre-dispatch check**: before every dispatch cycle.
2. **Scheduled GC**: on a cron schedule, creating issues for persistent violations.
3. **On-demand**: agents can trigger it via `conductor_harness_check`.

### 11.2 Pre-Dispatch Check

If `enforcement.drift_check_on_dispatch == true`:

1. Run all HarnessRules with `check` commands in the workspace root.
2. Collect results by severity.
3. `warning` violations: inject as `## Known Technical Debt` in the next agent's prompt.
4. `error` violations: inject as `## Architectural Issues You Must Fix` in the prompt.
5. `blocking` violations: if `blocking_violations_halt_dispatch`, set `enforcer_status = blocked`
   and skip dispatch for this tick; log a `HarnessEnforcerBlocked` audit event.

### 11.3 Scheduled GC

When `enforcement.gc_schedule_cron` fires:

1. Run all HarnessRules.
2. For each `auto_fix == true` violation:
   a. Check if an open GC issue already exists for this `rule_id` (to avoid duplicates).
   b. If not: create a tracker issue with:
   - Title: `[GC] <rule.name>: <violation_summary>`
   - Description: violation details + fix_hint + affected files.
   - Labels: `[enforcement.gc_issue_label]`
   - State: `enforcement.gc_issue_state`
     c. Log a `GCTaskCreated` audit event.
3. GC issues are routed to the `gc_agent` pipeline by the routing rules.

### 11.4 Layer Violation Reporting

Layer violations from the Knowledge Engine are reported as violations of the
`no-cross-layer-imports` category (or the matching harness rule by `category: dependency`).
The Knowledge Engine's `CheckLayerViolations` output is translated into HarnessRule violation
records by the Enforcer.

---

## 12. Agent Router

### 12.1 Overview

The Agent Router selects the execution pipeline for each issue and manages the pipeline execution
lifecycle — invoking each role in sequence, passing outputs between roles, and collecting results.

### 12.2 Issue Classification

On first dispatch of an unclassified issue:

1. Invoke the `default` provider with a classification prompt:

   ```
   Classify the following issue into exactly one task type:
   feature, bug, refactor, investigation, docs, gc_task, unknown.

   Issue: {{ issue.title }}
   Description: {{ issue.description }}
   Labels: {{ issue.labels | join: ", " }}

   Respond with only the task type, no explanation.
   ```

2. Update `issue.task_type` in orchestrator state.
3. Record the classification as an audit event.

### 12.3 Pipeline Selection

```
For each issue:
1. Evaluate routing.rules in order
2. First match: use its pipeline
3. No match: use routing.pipeline
```

### 12.4 Pipeline Execution

For each role in the selected pipeline:

1. Retrieve `ProviderConfig` for the role (fall back to `default`).
2. Instantiate the corresponding Provider Adapter.
3. Build the turn prompt (see Section 15).
4. Execute the turn via the adapter.
5. Run the Validation Pipeline (if `validation.run_after_turn`).
6. If the turn succeeds: capture the role's output as `previous_role_output`.
7. If the turn fails: fail the attempt; orchestrator applies retry/backoff.
8. Advance to the next role.

Role outputs are passed forward as `## Output from Previous Role (<role_name>)` sections.

### 12.5 Continuation Handling

After the last role in the pipeline completes:

1. The worker re-fetches the issue state from the tracker.
2. If still in an `active_state` and `turn_count < max_turns`: start another iteration of the
   pipeline using continuation prompt templates.
3. If the issue transitioned to a non-active state or `turn_count >= max_turns`: exit.
4. After normal exit: the orchestrator schedules a 1-second continuation retry (per Symphony).

---

## 13. Orchestrator

### 13.1 State Machine

Internal orchestration states (not tracker states):

1. **Unclaimed** — not in `running` or `retry_attempts`.
2. **Classifying** — Agent Router is inferring `task_type`.
3. **Claimed** — reserved to prevent duplicate dispatch.
4. **Running** — in `running` map; pipeline is executing.
5. **Validating** — post-turn Validation Pipeline is running.
6. **RetryQueued** — retry timer is active.
7. **EnforcerBlocked** — dispatch halted by a blocking harness violation.
8. **Released** — claim removed; issue is terminal or no longer active.

### 13.2 Poll Loop

```
tick:
  1. Run Harness Enforcer pre-dispatch check
  2. Reconcile active runs (stall detection + tracker state refresh)
  3. Run dispatch preflight validation
  4. Sync pending Doc Stores
  5. Fetch candidate issues from tracker
  6. Classify unclassified candidates
  7. Sort by dispatch priority
  8. Dispatch eligible issues until slots exhausted
  9. Notify observability consumers
```

If step 3 (preflight validation) fails: skip steps 5–8; continue with steps 1–2 and 9.

### 13.3 Candidate Selection

An issue is dispatch-eligible if and only if:

- `id`, `identifier`, `title`, and `state` are all present.
- State is in `active_states` and not in `terminal_states`.
- Not in `running` map.
- Not in `claimed` set.
- Global concurrency slots are available: `max_concurrent_agents - len(running) > 0`.
- Per-state concurrency slots are available.
- Blocker rule: if state is `Todo`, all `blocked_by` entries must be in terminal states.
- Enforcer is not in `blocked` status (if `blocking_violations_halt_dispatch`).

Dispatch sort order:

1. `priority` ascending (null sorts last).
2. `created_at` oldest first.
3. `identifier` lexicographic tiebreaker.

### 13.4 Retry and Backoff

Backoff formula:

- Continuation retry (clean worker exit): fixed 1000ms delay.
- Failure-driven retry: `min(10000 * 2^(attempt-1), max_retry_backoff_ms)`.

After each retry that succeeds, the Memory Manager records a `RetrySucceeded` episodic memory
noting the prior error, so future agents are warned about it.

### 13.5 Reconciliation

**Part A: Stall detection** — identical to Symphony Section 8.5 Part A.

**Part B: Tracker state refresh** — identical to Symphony Section 8.5 Part B.

**Part C: Memory post-processing**

- For each run that terminates (success, failure, timeout, stall, cancellation):
  - Extract audit events from the run's audit log.
  - Write a session-end episodic memory: summary of what was done, outcome, notable errors.
  - If `memory.consolidation_enabled`: queue for next consolidation pass.

### 13.6 Startup Terminal Cleanup

On startup:

1. Query tracker for issues in terminal states.
2. For each: remove workspace directory; clear any stale runtime state.
3. On failure: log warning; continue startup.

---

## 14. Workspace Management

### 14.1 Layout

```
<workspace.root>/
  <sanitized_identifier>/
    .conductor/
      meta.json          ← workspace metadata (created_at, issue snapshot)
      session.json       ← current session state
      knowledge.json     ← last knowledge context snapshot
      memory.json        ← last memory context snapshot
      audit.jsonl        ← local audit trail for this workspace
      validation/
        <turn_index>.json  ← validation results per turn
    <repo>/              ← cloned repository (or workspace contents)
    <repo-extra>/        ← additional repos for multi-repo workspaces
```

The `.conductor/` directory is in `.gitignore` by default (added by the `after_create` hook if
not already present).

### 14.2 Safety Invariants

**Invariant 1**: The coding agent process runs only within `workspace_path`.

- Verified before process launch: `abs(cwd) == abs(workspace_path)`.

**Invariant 2**: `workspace_path` must be within `workspace_root`.

- Normalized absolute paths required. Reject any path where `workspace_path` does not have
  `workspace_root` as a directory prefix.

**Invariant 3**: Workspace key is sanitized.

- Only `[A-Za-z0-9._-]` in workspace directory names. All other characters → `_`.

**Invariant 4**: Credentials are never written to the workspace.

- Provider API keys, tracker tokens, and doc store auth are never written to workspace files.
- Agent access to tracker and docs goes exclusively through injected Conductor tools.

### 14.3 Agent Isolation

Default isolation: **goroutine + subprocess** (`os/exec`).

- Each agent run is a goroutine that owns a subprocess (the CLI agent or API call loop).
- Goroutines are bounded by `max_concurrent_agents`.
- No shared mutable state between goroutines other than the Orchestrator's serialized state.

Optional isolation: **Docker container per workspace** (opt-in).

When `workspace.container` is configured:

```yaml
workspace:
  container:
    image: conductor-agent:latest
    network: none # air-gapped by default
    memory_limit: 4g
    cpu_limit: 2.0
    extra_mounts: []
```

- Each agent run launches a Docker container with the workspace mounted.
- `network: none` prevents the agent from making external HTTP calls (all external access goes
  through Conductor tool injection).
- Conductor's tool server runs on the host and is accessible via a Unix socket mounted into the
  container.

---

## 15. Validation Pipeline

### 15.1 Overview

The Validation Pipeline runs configurable shell commands in the workspace after each agent turn
and injects results into the next turn's context. This is the "shift feedback left" mechanism
described in Harness Engineering, implemented as infrastructure rather than a team convention.

### 15.2 Execution

For each check in `validation.checks`, in order:

1. Execute command in workspace root with a timeout of `check.timeout_ms`.
2. Capture stdout, stderr, and exit code.
3. Classify: exit 0 → `passed`; non-zero → `failed`; timeout → `timeout`.
4. Truncate output to `check.output_max_bytes`.
5. Record result in `.conductor/validation/<turn_index>.json`.
6. Record a `ValidationCheckRun` audit event.

### 15.3 Turn Failure on Validation

If `validation.fail_on_severity` is set:

- If any check with severity ≥ threshold fails or times out: mark the turn as failed.
- The orchestrator treats this identically to any other turn failure (retry/backoff applies).
- The failure reason is `validation_pipeline_failed:<check_id>`.

### 15.4 Context Injection

If `validation.inject_results_into_context == true`, format and prepend to the next turn prompt:

```markdown
## Validation Results (Previous Turn)

✅ unit_tests — 142 passed, 0 failed (2.3s)
❌ lint — 3 errors (eslint)
⚠️ type_check — 1 warning (tsc)

### lint failures

src/services/auth.ts:42:5 - error TS2345: Argument of type 'string' is not assignable...
src/handlers/user.ts:17:1 - error no-unused-vars: 'logger' is defined but never used.
[... truncated to 4096 bytes]
```

---

## 16. Prompt Construction

### 16.1 Prompt Assembly Order

Each agent turn prompt is assembled in this order:

```
1. Role template (rendered with Liquid, issue variables, pipeline metadata)
2. ## Validation Results (Previous Turn)      ← from Validation Pipeline
3. ## Output from Previous Role (<name>)      ← from prior pipeline stage
4. ## Codebase Context                        ← from Knowledge Engine
5. ## Relevant Documentation                  ← from Doc Store Manager
6. ## Relevant Memory                         ← from Memory Manager
7. ## Known Technical Debt                    ← warning-severity harness violations
8. ## Architectural Issues You Must Fix       ← error-severity harness violations
```

Sections are omitted if their source is disabled or returns no content.
The assembled prompt is truncated from the bottom (sections 7–8 first) if it exceeds
`context_budget * 0.7` before the model response.

### 16.2 Liquid Template Variables

Standard variables available in all role templates:

```
issue.id, issue.identifier, issue.title, issue.description,
issue.priority, issue.state, issue.url, issue.labels,
issue.blocked_by, issue.created_at, issue.updated_at,
issue.task_type, issue.estimated_complexity

attempt                  ← null on first run; integer on retry/continuation
agent_role               ← current role name
pipeline                 ← list of role names in the pipeline
pipeline_index           ← current role's index in the pipeline (0-based)
pipeline_length          ← total number of roles in the pipeline

memory_summary           ← one-sentence summary of retrieved memories
knowledge_summary        ← one-sentence summary of retrieved codebase context
```

Unknown variables cause a `template_render_error` and fail the run attempt.
Unknown filters cause a `template_render_error` and fail the run attempt.

### 16.3 Continuation Prompts

For turns after the first in a multi-turn session (same thread):

- If `## continuation` section exists in `HARNESS.md`: use it as the template.
- Otherwise: use the built-in continuation prompt:
  ```
  Continue working on {{ issue.identifier }}: {{ issue.title }}.
  This is continuation turn {{ attempt }}.
  Review your previous work and the validation results, then continue.
  ```

The full original task prompt is never re-sent on continuation turns (it is already in thread
history).

---

## 17. Audit Trail

### 17.1 Overview

Every significant action in the system is recorded as an `AuditEvent` with a `parent_event_id`
link. This builds a provenance graph that allows tracing any code change back to the issue,
the agent turn, the tool call, and the model decision that produced it.

### 17.2 Event Type Registry

| Event Type                 | Trigger                                            |
| -------------------------- | -------------------------------------------------- |
| `IssueDispatched`          | Issue moved to `Running` state                     |
| `IssueCancelled`           | Orchestrator cancelled a running issue             |
| `IssueReleased`            | Claim released (terminal or no longer active)      |
| `RunAttemptStarted`        | New run attempt created                            |
| `RunAttemptEnded`          | Run attempt completed (success or failure)         |
| `PipelineRoleStarted`      | A specific agent role began execution              |
| `PipelineRoleEnded`        | A specific agent role completed                    |
| `TurnStarted`              | An agent turn began                                |
| `TurnCompleted`            | An agent turn completed successfully               |
| `TurnFailed`               | An agent turn failed                               |
| `TurnTimedOut`             | An agent turn exceeded `turn_timeout_ms`           |
| `ToolCalled`               | Agent called a Conductor-injected tool             |
| `ToolResult`               | Result returned to agent from a tool call          |
| `MemoryRead`               | `conductor_memory_read` executed                   |
| `MemoryWritten`            | `conductor_memory_write` executed                  |
| `KnowledgeQueried`         | `conductor_knowledge_search` executed              |
| `DocSearched`              | `conductor_doc_search` executed                    |
| `ValidationCheckRun`       | A Validation Pipeline check ran                    |
| `ValidationPipelineFailed` | The Validation Pipeline failed the turn            |
| `ContextCompacted`         | Context compaction triggered                       |
| `HarnessViolationDetected` | A HarnessRule check failed                         |
| `HarnessEnforcerBlocked`   | Dispatch blocked by blocking violation             |
| `GCTaskCreated`            | An automatic GC issue was created in the tracker   |
| `KnowledgeIndexed`         | Knowledge Engine completed a (re-)index cycle      |
| `DocStoreSynced`           | A Doc Store sync completed                         |
| `MemoryConsolidated`       | Memory consolidation run completed                 |
| `ConfigReloaded`           | `HARNESS.md` was successfully hot-reloaded         |
| `ConfigReloadFailed`       | `HARNESS.md` reload failed; using last good config |
| `WorkspaceCreated`         | New workspace directory created                    |
| `WorkspaceRemoved`         | Workspace directory deleted                        |
| `HookExecuted`             | A lifecycle hook was run                           |
| `SessionStalled`           | Stall timeout exceeded; worker being terminated    |
| `RetryScheduled`           | Retry timer queued after failure                   |

### 17.3 Storage

Audit events are written to:

1. **Local audit log**: `.conductor/audit.jsonl` within each workspace (issue-scoped).
2. **Central audit store**: the configured database (`audit_events` table in SQLite or Postgres).
3. **Optional sink**: HTTP webhook or stdout (configurable via `server.audit_webhook_url`).

The central store supports queries via the API (see Section 18.3).

---

## 18. HTTP API and Dashboard

### 18.1 Architecture

The HTTP server is embedded in the `conductor` binary. It serves:

- REST API on `/api/v1/`
- WebSocket endpoint on `/ws`
- Embedded SvelteKit dashboard assets on `/` (served via Go `embed` package)

In local profile: dashboard is available at `http://127.0.0.1:8080`.
In team profile: dashboard is available at the configured host/port with optional auth.

### 18.2 WebSocket Protocol

Real-time updates are pushed to the dashboard over a single WebSocket connection per client.
The server pushes structured JSON events on state changes:

```json
{"type": "OrchestratorState", "payload": { ...runtime_snapshot... }}
{"type": "AuditEvent", "payload": { ...audit_event... }}
{"type": "AgentTurnStream", "payload": {"issue_id": "...", "chunk": "..."}}
{"type": "ValidationResult", "payload": { ...validation_result... }}
```

Clients subscribe to specific issue IDs by sending:

```json
{ "type": "Subscribe", "issue_ids": ["ABC-123", "ABC-456"] }
```

### 18.3 REST API Endpoints

```
GET  /api/v1/status                    ← Full runtime snapshot
GET  /api/v1/issues                    ← All tracked issues with their orchestration state
GET  /api/v1/issues/:id                ← Single issue detail
POST /api/v1/issues/:id/dispatch       ← Force dispatch of an issue
POST /api/v1/issues/:id/cancel         ← Cancel a running issue
GET  /api/v1/audit                     ← Paginated audit events (filter by issue, role, type)
GET  /api/v1/audit/:issue_id           ← Audit trail for a specific issue (provenance graph)
GET  /api/v1/memory/:project_id        ← Semantic and procedural memories for the project
GET  /api/v1/memory/:project_id/:issue_id ← Episodic memories for an issue
DELETE /api/v1/memory/:id              ← Retire a specific memory entry
POST /api/v1/memory/consolidate        ← Trigger manual consolidation
GET  /api/v1/knowledge/search?q=...    ← Semantic search over knowledge graph
GET  /api/v1/knowledge/nodes/:id       ← Single knowledge node + edges
GET  /api/v1/docs/search?q=...         ← Semantic search over doc stores
POST /api/v1/docs/sync                 ← Trigger manual sync of all doc stores
GET  /api/v1/harness                   ← Current effective harness config + templates
PUT  /api/v1/harness                   ← Update HARNESS.md (writes to file; triggers reload)
GET  /api/v1/harness/violations        ← Current harness rule violations
POST /api/v1/harness/check             ← Run harness checks on demand
GET  /api/v1/workspaces                ← List all workspaces with metadata
DELETE /api/v1/workspaces/:key         ← Delete a workspace
GET  /api/v1/providers                 ← Configured providers and their status (key present / not)
```

### 18.4 Dashboard Views

The SvelteKit dashboard provides:

1. **Overview** — live grid of all tracked issues: state, agent role, turn count, tokens used,
   elapsed time, validation status.
2. **Issue Detail** — per-issue: audit timeline, agent output stream, validation results,
   memory entries, knowledge context used.
3. **Memory** — browse and manage semantic, episodic, and procedural memories; manually retire
   or promote entries.
4. **Knowledge Graph** — visual explorer of the codebase graph: nodes, edges, layer assignments,
   violation markers.
5. **Harness Editor** — live editor for `HARNESS.md`; split-pane with preview; save triggers
   hot-reload.
6. **Doc Stores** — status of all configured stores; manual sync trigger; document browser.
7. **Audit Trail** — searchable, filterable event stream with provenance graph visualization.
8. **Settings** — provider configuration, enforcement settings, validation checks.

---

## 19. CLI

### 19.1 Command Structure

```
conductor
  start           ← Start the Conductor service (blocking)
  stop            ← Gracefully stop the service
  status          ← Print current runtime status to stdout
  dispatch <id>   ← Force-dispatch an issue by identifier
  cancel <id>     ← Cancel a running issue by identifier
  workspace
    list          ← List all workspaces
    remove <key>  ← Delete a workspace
    open <key>    ← Open workspace in $EDITOR
  memory
    list          ← List memories for the project
    retire <id>   ← Mark a memory entry as retired
    consolidate   ← Run memory consolidation now
  knowledge
    index         ← Re-index the codebase
    search <q>    ← Search the knowledge graph
  docs
    sync          ← Sync all doc stores
    search <q>    ← Search doc stores
  harness
    check         ← Run all harness checks
    validate      ← Validate HARNESS.md without starting the service
  init            ← Scaffold a new HARNESS.md for the current directory
    --profile local|team|cloud
  version         ← Print version and build info
```

### 19.2 `conductor start` Flags

```
--harness <path>           Path to HARNESS.md (default: ./HARNESS.md)
--port <int>               Override server.port
--log-level <level>        debug, info, warn, error (default: info)
--log-format <format>      json, text (default: json)
--no-dashboard             Disable the web dashboard
--no-api                   Disable the REST API
--dry-run                  Validate config and print what would run; do not start
```

---

## 20. Extensibility

### 20.1 Tracker Adapters

New trackers are registered by implementing:

```go
type TrackerAdapter interface {
    FetchCandidateIssues(ctx context.Context) ([]Issue, error)
    FetchIssuesByStates(ctx context.Context, states []string) ([]Issue, error)
    FetchIssueStatesByIDs(ctx context.Context, ids []string) (map[string]string, error)
    ExecuteQuery(ctx context.Context, query string, variables map[string]any) (map[string]any, error)
    ExecuteMutation(ctx context.Context, mutation string, variables map[string]any) (map[string]any, error)
}
```

### 20.2 Provider Adapters

New LLM providers implement `ProviderAdapter` (see Section 7.1).

### 20.3 Knowledge Store Backends

```go
type KnowledgeStore interface {
    Upsert(ctx context.Context, nodes []KnowledgeNode) error
    Search(ctx context.Context, embedding []float32, filters KnowledgeFilter, topK int) ([]KnowledgeNode, error)
    FindByID(ctx context.Context, id string) (*KnowledgeNode, error)
    Delete(ctx context.Context, ids []string) error
    ListEdges(ctx context.Context, fromID string) ([]KnowledgeEdge, error)
}
```

### 20.4 Doc Store Backends

```go
type DocStoreBackend interface {
    Sync(ctx context.Context) ([]DocRef, error)
    Fetch(ctx context.Context, ref DocRef) (string, error)
    List(ctx context.Context, filters DocFilter) ([]DocRef, error)
}
```

### 20.5 Tool Plugins

```go
type ToolPlugin interface {
    Name() string
    Description() string
    ParametersSchema() JSONSchema
    Execute(ctx context.Context, params map[string]any, exec ExecutionContext) (ToolResult, error)
}
```

Plugins are registered at startup and advertised to all agent sessions.

---

## 21. Security Posture

### 21.1 Credential Isolation

- All secrets (API keys, tokens) are resolved from environment variables at startup.
- Secrets are never written to disk, workspace files, or logs.
- Agent processes never receive raw credentials; they access external services only through
  Conductor-mediated tool calls.
- Audit events for tool calls include parameters but redact known secret fields.

### 21.2 Approval Policies

Configured per-role in `providers.<role>.approval_policy`:

| Policy               | Behavior                                                                                                  |
| -------------------- | --------------------------------------------------------------------------------------------------------- |
| `auto`               | All agent actions are auto-approved. Use in trusted environments only.                                    |
| `review_destructive` | Destructive tracker mutations (state changes, deletions) require operator confirmation via the dashboard. |
| `manual`             | All tool calls require operator approval. Suitable for regulated environments.                            |

Default: `auto`. Each deployment should document its chosen posture.

### 21.3 Container Isolation

When `workspace.container` is configured:

- Network is disabled by default (`network: none`).
- All external access (tracker, LLM, docs) goes through Conductor's tool server via Unix socket.
- The container can only read/write its workspace mount.
- Resource limits (memory, CPU) are enforced by the container runtime.

### 21.4 Authentication for the Dashboard and API

Local profile: no authentication by default (bound to `127.0.0.1`).

Team/cloud profile: configure via:

```yaml
server:
  auth:
    kind: bearer_token # bearer_token | oidc | none
    token: $CONDUCTOR_API_TOKEN
```

OIDC integration is the recommended approach for multi-user team deployments.

---

## 22. Configuration Cheat Sheet

```yaml
project:
  id: my-project
  name: My Project

tracker:
  kind: linear
  api_key: $LINEAR_API_KEY
  project_slug: my-project-slug
  active_states: [Todo, In Progress]
  terminal_states: [Done, Cancelled, Closed, Duplicate]

polling:
  interval_ms: 30000

workspace:
  root: $HOME/.conductor/workspaces/my-project

providers:
  default:
    provider: openrouter
    model: anthropic/claude-sonnet-4-5
    api_key: $OPENROUTER_API_KEY
    context_budget: 150000
    compaction_strategy: summarize
  planner:
    provider: anthropic
    model: claude-opus-4-5
    api_key: $ANTHROPIC_API_KEY
    context_budget: 100000
  verifier:
    provider: openai
    model: o3-mini
    api_key: $OPENAI_API_KEY
  gc_agent:
    provider: ollama
    model: qwen2.5-coder:32b
    base_url: http://localhost:11434

routing:
  pipeline: [planner, coder, verifier, reviewer]
  rules:
    - when: { labels: [docs] }
      pipeline: [coder, reviewer]
    - when: { any_label: [gc_task, refactor] }
      pipeline: [gc_agent]
    - when: { complexity: trivial }
      pipeline: [coder, verifier]

knowledge:
  enabled: true
  store_backend: sqlite_vec
  store_path: $HOME/.conductor/knowledge/my-project.db
  use_ast: true
  top_k: 10
  layer_definitions:
    types: ["src/types/**"]
    config: ["src/config/**"]
    repo: ["internal/repo/**"]
    service: ["internal/service/**"]
    runtime: ["cmd/**"]
    ui: ["web/**"]

memory:
  enabled: true
  store_backend: sqlite
  store_path: $HOME/.conductor/memory/my-project.db
  episodic_ttl_days: 90
  max_context_memories: 5
  consolidation_enabled: true
  consolidation_interval_hours: 24

docs:
  enabled: true
  stores:
    - id: specs
      backend: git_repo
      path_or_url: https://github.com/myorg/project-specs
      auth:
        token: $SPECS_REPO_TOKEN
      sync_interval_minutes: 30
    - id: adrs
      backend: notion
      path_or_url: $NOTION_DATABASE_ID
      auth:
        api_key: $NOTION_API_KEY

harness_rules:
  - id: no-cross-layer-imports
    name: No cross-layer imports
    category: dependency
    severity: blocking
    check: conductor lint check-layers
    fix_hint: "Move the dependency to a shared layer or use an interface."
    auto_fix: false
  - id: max-file-lines
    name: Files must not exceed 500 lines
    category: file_size
    severity: warning
    check: |
      FILES=$(find . -name '*.go' -not -path './.conductor/*' | xargs wc -l 2>/dev/null | awk '$1 > 500 {print $2}')
      [ -z "$FILES" ] && exit 0 || (echo "$FILES" && exit 1)
    fix_hint: Split the file into smaller modules with single responsibilities.
    auto_fix: true

enforcement:
  enabled: true
  gc_schedule_cron: "0 2 * * *"
  gc_issue_label: gc_task
  drift_check_on_dispatch: true
  blocking_violations_halt_dispatch: false

validation:
  enabled: true
  run_after_turn: true
  inject_results_into_context: true
  fail_on_severity: error
  checks:
    - id: unit_tests
      name: Unit tests
      command: go test ./... -count=1 -timeout=120s
      timeout_ms: 130000
      severity: error
    - id: lint
      name: Lint
      command: golangci-lint run --timeout=30s
      timeout_ms: 35000
      severity: warning
    - id: type_check
      name: Type check
      command: go build ./...
      timeout_ms: 60000
      severity: error

hooks:
  after_create: |
    git clone $REPO_URL repo/
    cd repo && go mod download
  before_run: |
    cd repo && git fetch origin && git rebase origin/main || true
  after_turn: |
    echo "Turn $CONDUCTOR_TURN_INDEX completed for $CONDUCTOR_ISSUE_IDENTIFIER (role: $CONDUCTOR_AGENT_ROLE)"
  timeout_ms: 120000

agent:
  max_concurrent_agents: 10
  max_turns: 20
  max_retry_backoff_ms: 300000

server:
  port: 8080
  host: 127.0.0.1
  enable_api: true
  enable_dashboard: true
```

---

## 23. Error Classification

### 23.1 Harness Errors

- `missing_harness_file`
- `harness_parse_error`
- `harness_front_matter_not_a_map`
- `template_parse_error`
- `template_render_error`

### 23.2 Tracker Errors

- `unsupported_tracker_kind`
- `missing_tracker_api_key`
- `missing_tracker_project_id`
- `tracker_request_failed`
- `tracker_response_error`
- `tracker_graphql_errors`
- `tracker_unknown_payload`

### 23.3 Provider Errors

- `unsupported_provider`
- `missing_provider_api_key`
- `provider_request_failed`
- `provider_response_timeout`
- `provider_stream_error`
- `turn_input_required` (agent requested human input — hard failure)
- `context_budget_exceeded` (only if `compaction_strategy: none`)

### 23.4 Run Attempt Errors

- `workspace_creation_failed`
- `hook_failed`
- `prompt_render_failed`
- `turn_timeout`
- `turn_failed`
- `turn_cancelled`
- `stall_timeout`
- `validation_pipeline_failed`
- `cancelled_by_reconciliation`

### 23.5 Knowledge / Memory Errors

- `knowledge_index_failed`
- `knowledge_search_failed`
- `memory_read_failed`
- `memory_write_failed`
- `doc_store_sync_failed`
- `embedding_request_failed`

---

## 24. Normative References

- OpenAI — _Harness Engineering: Leveraging Codex in an Agent-First World_ (Feb 2026)
  https://openai.com/index/harness-engineering/
- OpenAI — _An open-source spec for Codex orchestration: Symphony_ (Apr 2026)
  https://openai.com/index/open-source-codex-orchestration-symphony/
- OpenAI Symphony Specification
  https://github.com/openai/symphony/blob/main/SPEC.md
- GitNexus — Zero-Server Code Intelligence Engine (knowledge graph inspiration)
  https://github.com/abhigyanpatwari/GitNexus
- Cognition — _Devin on Claude Sonnet 4.5: Lessons and Challenges_ (context anxiety research)
  https://cognition.ai/blog/devin-sonnet-4-5-lessons-and-challenges
- Martin Fowler — _Harness Engineering for Coding Agent Users_
  https://martinfowler.com/articles/harness-engineering.html
- Stripe — _Minions: shift-left feedback and blueprints_

---

## Appendix A: Implementation Assumptions

The following decisions were made for the reference implementation based on the requirements
gathered. They are recorded here for transparency.

| Decision                | Choice                                    | Rationale                                                                   |
| ----------------------- | ----------------------------------------- | --------------------------------------------------------------------------- |
| Deployment profile      | Hybrid (local default, cloud opt-in)      | Single binary for local; Docker Compose for teams                           |
| UI scope                | Full dashboard + control                  | Observability, memory management, harness editing, dispatch control         |
| Database strategy       | SQLite default, Postgres optional         | Zero external deps for local; team scale with Postgres                      |
| Agent isolation         | Goroutines + subprocess (Docker opt-in)   | Lightweight default; Docker for air-gapped/high-trust environments          |
| CLI interface           | CLI + HTTP API                            | Cobra CLI for operators; HTTP API for integrations and dashboard            |
| Philosophy              | Full spec first, iterative implementation | Build the right architecture; implement in phases                           |
| AST parsing             | tree-sitter with dependency graph         | Dependency edges are the highest-value output for harness enforcement       |
| UI framework            | SvelteKit + Tailwind + Bun                | Performance over familiarity; smaller bundles, faster reactivity than React |
| Embedding storage       | sqlite-vec (default), Qdrant (opt-in)     | No external service for local; Qdrant for scale                             |
| Real-time transport     | WebSocket (Gorilla)                       | Full-duplex; required for streaming agent output to dashboard               |
| Frontend asset delivery | Go `embed` package                        | Single binary includes the dashboard                                        |

---

_Conductor Specification v0.1 — Draft_  
_Provider-agnostic · Harness-first · Memory-native · Knowledge-driven_
