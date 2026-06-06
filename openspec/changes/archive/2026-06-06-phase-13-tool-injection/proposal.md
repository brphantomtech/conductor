# Phase 13 — Conductor Tool Injection

## Why

Every engine Conductor has built — tracker, knowledge, docs, memory, harness enforcer, validation —
is reachable today only by Conductor itself, never by the agent doing the work. An agent that needs
to look up a symbol, recall a past mistake, check an architectural rule, or update an issue has no
way to ask. Phase 13 closes that gap: it injects a fixed set of built-in tools into every agent
session over the provider's native tool-use mechanism, dispatches the agent's tool calls to the
real engines with Conductor-mediated credentials (the agent never sees a token), enforces
per-role approval policies, and exposes a plugin interface for third-party tools. This is the
convergence point that makes the agents genuinely capable — and it gives the Harness Enforcer the
tracker-write surface its scheduled GC needs.

This is a sequential phase (SPEC convergence point): it consumes Phases 4, 8, 9, 10, 11, 12 and is
built on its own branch, then merged.

Authoritative scope: [docs/phases.md](../../../docs/phases.md) → "Phase 13 — Conductor Tool
Injection"; SPEC §7.3 (tool injection), §20.5 (ToolPlugin), §21.1 (credential isolation), §21.2
(approval policies).

## What Changes

- **New `internal/tools` package** — a `Tool` interface (`Name`, `Description`, `ParametersSchema`,
  `Execute(ctx, params, ExecutionContext)`), a `Registry` that advertises the available tools as
  `provider.ToolSpec`s, and a `Dispatcher` that routes an agent's tool call to the matching tool,
  marshals the result, and emits `ToolCalled`/`ToolResult` audit events (already declared).
- **The eight built-in tools** (SPEC §7.3), each backed by an existing engine via a narrow
  consumer-side interface:
  - `conductor_tracker_query` / `conductor_tracker_mutate` → tracker `ExecuteQuery`/`ExecuteMutation`.
  - `conductor_knowledge_search` → Knowledge Engine hybrid search.
  - `conductor_doc_search` → Doc Store search.
  - `conductor_memory_read` / `conductor_memory_write` → Memory Manager retrieve/write.
  - `conductor_harness_check` → Harness Enforcer rule run.
  - `conductor_validation_run` → Validation Pipeline run.
- **Tool-call execution loop**: when a turn emits a tool call, the dispatcher executes it and feeds
  the result back into the same session to continue the turn. This requires a **tool-result
  continuation** on the provider adapter (the current `ContinueTurn` takes a prompt, not a tool
  result) — added for the OpenAI-compatible and Anthropic adapters.
- **Approval policy enforcement** (SPEC §21.2): a new `approval_policy` field on `ProviderConfig`
  (`auto` default, `review_destructive`, `manual`). Destructive tools (`conductor_tracker_mutate`)
  are gated under `review_destructive`; all tools are gated under `manual`. With no operator
  approval channel yet (the dashboard is Phase 15), a gated call is denied with a structured
  "approval required" result rather than executed — honest, not silently auto-approved.
- **Credential isolation** (SPEC §21.1): tools resolve credentials from Conductor's config at
  dispatch time; tool parameters and results are passed to the agent without secrets, and audit
  payloads redact secret fields (reusing the Phase 17 redactor).
- **`ToolPlugin` interface** (SPEC §20.5) and a registration mechanism so plugins are advertised to
  all sessions alongside the built-ins.
- **Router/orchestrator integration**: the router wires the registry into each session's `StartTurn`
  tools and drives the dispatch loop.
- **GC TrackerIssuer wired** — with the tracker-write tool surface in place, the Phase 12 enforcer's
  `TrackerIssuer` (GC issue create + dedup) is implemented over the tracker adapter and wired in
  `start.go`, closing that deferred follow-up.
- **Unit tests** with fakes: registry advertising, dispatch routing + error mapping, each tool
  against a fake engine, approval gating per policy, credential redaction, the tool-result
  continuation, and the plugin registration path. No live API calls in CI.

## Capabilities

### New Capabilities

- `tool-injection`: the `Tool`/`Registry`/`Dispatcher` model, the eight built-in tools, the
  tool-call execution loop, approval-policy enforcement, credential isolation for tool dispatch,
  and the `ToolPlugin` interface + registration.

### Modified Capabilities

- `provider-adapter-layer`: a tool-result continuation path so a session can be continued with the
  output of a tool call (not just a text prompt). (Delta in
  `specs/provider-adapter-layer/spec.md`.)

## Impact

- **Affected specs**: new capability `tool-injection`; modified `provider-adapter-layer`
  (tool-result continuation).
- **Affected code**: `internal/tools/` (new). `internal/provider/` (tool-result continuation on the
  adapter interface + Anthropic/OpenAI/OpenRouter impls). `internal/config/types.go` (add
  `approval_policy` to `ProviderConfig`). `internal/router/` (drive the dispatch loop, inject the
  registry's tools into `StartTurn`). `cmd/conductor/cmd/start.go` (construct the registry from the
  wired engines; wire the GC `TrackerIssuer`).
- **Consumes (unchanged)**: `internal/tracker`, `internal/knowledge`, `internal/docstore`,
  `internal/memory`, `internal/harness` (enforcer), `internal/validation`, `internal/audit`.

### Non-goals

- The operator approval **channel** (interactive approve/deny) — requires the dashboard/API (Phases
  14/15); until then gated calls are denied with an "approval required" result.
- New engine behavior — tools are thin adapters over existing engines; no engine gains features here.
- Streaming tool output to the dashboard (Phase 14/15).
