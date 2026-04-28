# Implementation Phases

Implementation order, derived from the dependency graph of the spec. Each phase has a clear
goal, a concrete deliverables checklist, and the SPEC sections it implements. Phases are
ordered so each one can be built and shipped on its own — the orchestrator does not have to
wait for the dashboard to exist, the dashboard does not have to wait for the Knowledge Engine,
and so on.

The implementation does not aim to land all of Conductor at once. Phase 1 produces a runnable
binary that loads HARNESS.md, validates config, and exits cleanly. Each subsequent phase adds
one engine or surface and is independently testable.

---

## Phase 1 — Foundation & Skeleton

**Goal.** Producing a buildable binary that boots, loads config, and prints status. No
agent execution yet.

**Deliverables.**

- Go module + directory scaffolding under [internal/](../internal/) and [cmd/](../cmd/).
- `internal/config` — typed config struct, Viper loader, env-var precedence chain.
- `internal/db` — database abstraction with SQLite default; migrations runner; connection pool.
- `internal/audit` — append-only event writer; SQLite schema; `AuditEvent` type.
- `cmd/conductor` — `conductor version`, `conductor start --dry-run`, `conductor harness validate`.
- Logging via zerolog, configured by `--log-level` / `--log-format`.
- Unit tests for config precedence and audit writes.

**SPEC sections covered.** §3.3 (binary layout), §4.1.10 (AuditEvent schema), §6 (Configuration
Layer), §17 (Audit Trail storage), §19.1 + §19.2 (CLI command shape and `start` flags), §23
(error classification — error types defined as Go sentinel errors).

---

## Phase 2 — Harness Loader

**Goal.** Parse, validate, and hot-reload `HARNESS.md`.

**Deliverables.**

- `internal/harness` — front-matter parser, prompt-template extractor, schema validator.
- Liquid renderer wired up; unknown-variable / unknown-filter errors classified correctly.
- fsnotify-based watcher with debounced reload; last-known-good fallback on parse error.
- `conductor harness validate <path>` subcommand; emits structured errors.
- Audit events: `ConfigReloaded`, `ConfigReloadFailed`.

**SPEC sections covered.** §4.1.2 (HarnessDefinition), §5 (HARNESS.md format), §6.3 (dynamic
reload), §6.4 (startup validation), §16.2 (Liquid variables), §23.1 (harness errors).

---

## Phase 3 — Provider Adapter Layer

**Goal.** Exchange messages with at least one LLM provider behind the stable abstract interface.

**Deliverables.**

- `internal/provider` — `ProviderAdapter`, `SessionHandle`, `TurnStream`, `AgentEvent`,
  `ToolSpec`, `TurnResult` types.
- Adapter implementations for Anthropic, OpenAI, OpenRouter (the OpenAI-compatible three first;
  Ollama / LM Studio / custom share the OpenAI-compatible code path).
- Streaming response handling (SSE parser with backpressure).
- Token usage accounting + `context_budget` warnings (no compaction yet — Phase 9 finishes that).
- Provider-config validation at startup.
- Unit tests with recorded HTTP fixtures (no live API calls in CI).

**SPEC sections covered.** §4.1.3 (ProviderConfig), §7 (Provider Adapter Layer), §23.3
(provider errors).

---

## Phase 4 — Tracker Adapters

**Goal.** Read issues from at least one tracker.

**Deliverables.**

- `internal/tracker` — `TrackerAdapter` interface, normalization rules from §4.2.
- Linear adapter (GraphQL) — primary, since Symphony parity.
- GitHub Issues adapter (REST) — secondary.
- `Issue` entity, `BlockerRef`, label normalization.
- Recorded-fixture tests.

**SPEC sections covered.** §4.1.1 (Issue), §5.3.2 (tracker config), §20.1 (TrackerAdapter
interface), §23.2 (tracker errors).

---

## Phase 5 — Workspace Management

**Goal.** Create, manage, and destroy isolated per-issue workspaces.

**Deliverables.**

- `internal/workspace` — workspace key sanitization, layout creation, `.conductor/` skeleton.
- Hook execution: `after_create`, `before_run`, `after_run`, `after_turn`, `before_remove`,
  `on_harness_violation`, with timeout enforcement and audit logging.
- Multi-repo support (`workspace.repos`).
- Safety invariants enforced (SPEC §14.2 four invariants).
- Subprocess-based agent isolation (Docker stays a Phase-15 opt-in).

**SPEC sections covered.** §5.3.4 (workspace), §5.3.5 (hooks), §14 (workspace management),
§23.4 (run-attempt errors related to workspace creation/hooks).

---

## Phase 6 — Orchestrator Core

**Goal.** End-to-end: poll the tracker, claim an issue, dispatch an agent run, retry on
failure, release on terminal state. No router yet — single hardcoded "coder" pipeline.

**Deliverables.**

- `internal/orchestrator` — runtime state machine; `OrchestratorRuntimeState`; poll loop.
- Candidate selection, dispatch sort, blocker rules.
- Retry/backoff (continuation 1s; failure exponential).
- Reconciliation Parts A+B (stall detection + tracker state refresh).
- Startup terminal cleanup.
- Run attempt entity + lifecycle.

**SPEC sections covered.** §4.1.11 (orchestrator runtime state), §4.1.12 (RunAttempt),
§13 (Orchestrator), §23.4 (run-attempt errors).

---

## Phase 7 — Agent Router & Pipelines

**Goal.** Multi-role pipelines (planner → coder → verifier → reviewer) with rule-based routing.

**Deliverables.**

- `internal/router` — pipeline executor, role-by-role turn execution.
- Issue classification call on first dispatch.
- Routing rule evaluation (labels, task_type, state, title regex, complexity).
- Per-role provider config resolution.
- Continuation prompts on multi-turn sessions.

**SPEC sections covered.** §4.1.4 (AgentRole), §5.3.7 (routing), §12 (Agent Router), §16
(Prompt Construction).

---

## Phase 8 — Validation Pipeline

**Goal.** Run shell checks after each turn; inject results into the next turn's prompt.

**Deliverables.**

- `internal/validation` — pipeline runner, per-check timeout, severity-based turn failure.
- ValidationResult persistence per turn (`.conductor/validation/<turn_index>.json`).
- Context-injection formatter.
- `conductor validation run` subcommand for ad-hoc execution.

**SPEC sections covered.** §5.3.13 (validation config), §15 (Validation Pipeline).

---

## Phase 9 — Memory Manager

**Goal.** Three-layer persistent memory with cross-session retrieval.

**Deliverables.**

- `internal/memory` — Episodic, Semantic, Procedural stores.
- TTL enforcement on episodic; consolidation worker (LLM-driven cluster→synthesize→promote).
- Memory retrieval and formatter for prompt injection.
- Context-budget compaction (`summarize` and `sliding_window`) — completes the work started in
  Phase 3.
- `conductor memory list / retire / consolidate` subcommands.

**SPEC sections covered.** §4.1.5 (MemoryEntry), §5.3.9 (memory config), §7.4 (compaction),
§9 (Memory Manager).

---

## Phase 10 — Knowledge Engine

**Goal.** Index the codebase semantically + structurally; serve hybrid RAG.

**Deliverables.**

- `internal/knowledge` — discovery, parsing (tree-sitter for AST languages, line chunker
  fallback), summarization, embedding, graph construction, layer assignment, persistence.
- sqlite-vec backend; Qdrant backend behind a build tag.
- Incremental re-indexing via fsnotify (500ms debounce).
- Hybrid search (semantic + structural + dependency).
- `CheckLayerViolations` for the Harness Enforcer.
- Context-injection formatter.
- `conductor knowledge index / search` subcommands.

**SPEC sections covered.** §4.1.6 (KnowledgeNode), §4.1.7 (KnowledgeEdge), §5.3.8 (knowledge
config), §8 (Knowledge Engine).

---

## Phase 11 — Doc Store Manager

**Goal.** Index external documentation alongside the codebase.

**Deliverables.**

- `internal/docstore` — `DocStoreBackend` interface; `local_fs`, `git_repo`, `s3` backends.
- Sync scheduler with checksum-based change detection.
- Knowledge Engine integration (Doc nodes alongside file/symbol nodes).
- `docs://` URI handler for `HARNESS.md` resolution from a Doc Store.
- Notion / Confluence backends deferred to a follow-up phase if not needed for v0.1.

**SPEC sections covered.** §4.1.9 (DocRef), §5.3.10 (docs config), §10 (Doc Store Manager),
§20.4 (DocStoreBackend interface).

---

## Phase 12 — Harness Enforcer

**Goal.** Enforce architectural invariants pre-dispatch and on a schedule.

**Deliverables.**

- `internal/harness` (extended) — `HarnessRule` runner, severity classification, violation
  reporting.
- Pre-dispatch check wired into the orchestrator poll loop.
- Scheduled GC via robfig/cron; auto-creation of GC tracker issues with deduplication.
- Layer violation translation from Knowledge Engine.

**SPEC sections covered.** §4.1.8 (HarnessRule), §5.3.11–§5.3.12 (rules + enforcement),
§11 (Harness Enforcer), §8.6 (layer violations).

---

## Phase 13 — Conductor Tool Injection

**Goal.** Built-in tools available to agents on every session.

**Deliverables.**

- Tool dispatcher module shared by all provider adapters.
- All tools listed in SPEC §7.3 implemented:
  `conductor_tracker_query`, `conductor_tracker_mutate`, `conductor_knowledge_search`,
  `conductor_doc_search`, `conductor_memory_read`, `conductor_memory_write`,
  `conductor_harness_check`, `conductor_validation_run`.
- Approval policy enforcement (`auto`, `review_destructive`, `manual`).
- Plugin loading mechanism (`ToolPlugin` interface).

**SPEC sections covered.** §7.3 (tool injection), §20.5 (ToolPlugin), §21.1 (credential
isolation), §21.2 (approval policies).

---

## Phase 14 — HTTP API & WebSocket

**Goal.** Full REST + WebSocket surface backing the dashboard and external integrations.

**Deliverables.**

- `internal/api` — Chi-based REST handlers for every endpoint in SPEC §18.3.
- WebSocket hub (Gorilla); per-client subscription model.
- Streaming agent output over WS; provenance graph queries.
- Optional auth: bearer token + OIDC.

**SPEC sections covered.** §18 (HTTP API and Dashboard), §21.4 (auth).

---

## Phase 15 — SvelteKit Dashboard

**Goal.** All eight dashboard views in SPEC §18.4 are functional.

**Deliverables.**

- Routes for Overview, Issue Detail, Memory, Knowledge Graph, Harness Editor, Doc Stores,
  Audit Trail, Settings.
- WebSocket-backed Svelte stores for real-time issue/audit/turn streams.
- Knowledge Graph visualization (force-directed layout).
- Harness Editor with split-pane preview and save-to-reload flow.
- Embedding pipeline (`bun run build` → Go `embed` → single binary).

**SPEC sections covered.** §18.4 (Dashboard Views), §3.3 (web layout).

---

## Phase 16 — CLI Surface Completion

**Goal.** Every `conductor <subcommand>` in SPEC §19.1 works.

**Deliverables.**

- `workspace list / remove / open`.
- `dispatch / cancel`.
- `init --profile local|team|cloud` (scaffolds a starter HARNESS.md + docker-compose.yml).
- `status` with dashboard-equivalent runtime snapshot.

**SPEC sections covered.** §19 (CLI).

---

## Phase 17 — Container Isolation & Hardening

**Goal.** Air-gapped agent execution profile + security hardening.

**Deliverables.**

- Docker container per workspace (`workspace.container`).
- Conductor tool server reachable from container via Unix socket.
- Cloud profile (Profile C) Dockerfile + sample Helm chart.
- Secret redaction in audit events.

**SPEC sections covered.** §14.3 (agent isolation), §21.3 (container isolation), §3.2 Profile C.

---

## Phase 18 — Extensibility Surface

**Goal.** Third-party tracker adapters, provider adapters, knowledge stores, doc-store backends,
and tool plugins can be added without modifying core code.

**Deliverables.**

- Public `pkg/` API surfaces for the four extension points.
- Sample plugin in `examples/`.
- Plugin discovery + loading at startup.

**SPEC sections covered.** §20 (Extensibility).
