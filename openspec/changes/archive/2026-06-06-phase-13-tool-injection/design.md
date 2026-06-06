## Context

Phase 13 is the convergence point: it makes every engine reachable by the agent. All the engines
exist on `main` (tracker, knowledge, docstore, memory, harness enforcer, validation), and the
provider layer already injects `[]ToolSpec` into `StartTurn` and emits `EventToolCall` when the
model calls a tool — but nothing executes those calls or feeds results back. This phase supplies the
registry, the dispatcher, the eight built-in tools, the execution loop, approval gating, credential
isolation, and the plugin interface.

Two pieces of substrate are missing and must be added: a tool-result continuation on the provider
adapter (today `ContinueTurn` takes a prompt string, SPEC §7.1) and an `approval_policy` field on
`ProviderConfig` (SPEC §21.2). Both are small, additive, and isolated.

Conventions to match (Phases 2–12): consumer-side interfaces so `internal/tools` does not import
concrete engines where avoidable, constructor + functional `Option`s, fake engines in tests,
sentinel errors, audit via the existing writer with the Phase 17 redactor, no live API calls in CI.

## Goals / Non-Goals

**Goals:**

- A `Tool`/`Registry`/`Dispatcher` model advertising `provider.ToolSpec`s and routing calls.
- The eight SPEC §7.3 tools, each a thin adapter over an existing engine.
- A tool-call execution loop that continues the turn with the tool result.
- Approval-policy enforcement (`auto`/`review_destructive`/`manual`) with honest denial when no
  approval channel exists yet.
- Credential isolation: agents never see tokens; audit redacts secrets.
- A `ToolPlugin` interface advertised alongside built-ins.
- Wire the Phase 12 GC `TrackerIssuer` now that a tracker-write surface is in place.

**Non-Goals:**

- Interactive operator approval (Phases 14/15); new engine features; dashboard streaming.

## Decisions

### `Tool` interface + `Registry` + `Dispatcher`, consumer-side engine interfaces

`Tool` is `Name() / Description() / ParametersSchema() json.RawMessage / Execute(ctx, params,
ExecutionContext) (ToolResult, error)`. Each built-in tool depends on a narrow interface (e.g.
`knowledgeSearcher`, `memoryStore`, `trackerExecutor`) satisfied by the real engine at wiring time.

- **Why:** mirrors the SPEC §20.5 `ToolPlugin` shape so built-ins and plugins are the same type,
  and keeps `internal/tools` decoupled from concrete engines (testable with fakes). The `Registry`
  produces `[]provider.ToolSpec` so the router injects them into `StartTurn` unchanged.

### Tool-result continuation as a new provider method, not a `ContinueTurn` overload

Add `ContinueWithToolResults(ctx, s, []ToolResult) (TurnStream, error)` to the adapter (Anthropic
tool_result blocks; OpenAI/OpenRouter `role:"tool"` messages).

- **Why:** SPEC §7.1's tool-use contract needs the model to receive the tool output in the
  provider's native shape. A dedicated method keeps `ContinueTurn` (text continuation, Phase 7) and
  tool continuation distinct and each provider's wire format isolated. Added as an ADDED requirement
  on `provider-adapter-layer` (no existing behavior changes).

### Dispatch loop owned by the router, bounded by max tool turns

When a turn stream yields `EventToolCall`(s), the router asks the dispatcher to execute each, then
calls `ContinueWithToolResults`, repeating until the model stops calling tools or a tool-turn cap is
hit. Tool errors become `ToolResult`s with an error payload (the model sees the failure and can
react) rather than aborting the attempt.

- **Why:** the router already owns the turn loop (Phase 7); the dispatch loop is a natural extension
  there. A cap prevents infinite tool loops. Returning errors as results (not aborts) matches how
  agent harnesses surface tool failures.

### Approval gating is a pure decision; denial is explicit

`approve(tool, policy) Decision`: `auto` → allow; `review_destructive` → deny destructive tools
(tracker_mutate) with an "approval required" result; `manual` → deny all with that result. Denial is
a structured `ToolResult`, never a silent skip.

- **Why:** SPEC §21.2. With no approval channel yet (Phase 14/15), the only honest options are deny
  or auto; denying gated calls is safe and visible to the agent and the audit log. When the channel
  lands, `Decision` gains an `await-approval` path without changing tool code.

### Credential isolation at the dispatch boundary

Tools receive credentials from the `ExecutionContext` (sourced from Conductor config), never from
agent-supplied params; audit payloads for `ToolCalled`/`ToolResult` run through the Phase 17 redactor.

- **Why:** SPEC §21.1 — "agent processes never receive raw credentials" and "audit events redact
  known secret fields." The agent passes query/intent params only; Conductor injects auth.

### GC TrackerIssuer over the tracker adapter

Implement the Phase 12 `harness.TrackerIssuer` (`OpenGCRuleIDs`, `CreateGCIssue`) on top of the
tracker adapter's `ExecuteQuery`/`ExecuteMutation`, wired in `start.go`.

- **Why:** the tracker-write surface this phase needs for `conductor_tracker_mutate` is the same one
  GC needs; implementing it here closes the deferred Phase 12 follow-up in its natural home.

## Risks / Trade-offs

- **[Tool loop runs forever]** → A per-attempt tool-turn cap bounds it; exceeding the cap ends the
  turn with a diagnostic. Covered by a test with a fake model that always calls a tool.
- **[Credential leakage via params or audit]** → Auth is injected from `ExecutionContext`, never
  read from agent params; audit runs through the redactor. A test asserts a token never appears in a
  `ToolCalled`/`ToolResult` payload.
- **[Destructive mutation without oversight]** → Default `auto` is documented as trusted-env only;
  `review_destructive`/`manual` deny until the approval channel exists. `conductor_tracker_mutate`
  is classified destructive.
- **[Provider tool-result wire-format drift]** → Each adapter's continuation is isolated and tested
  against recorded fixtures; the OpenAI-compatible three share one code path.
- **[Tool errors aborting otherwise-good turns]** → Tool failures are returned to the model as error
  `ToolResult`s, not fatal errors, so the agent can recover; only dispatcher-internal faults error.

## Migration Plan

Additive plus two small isolated changes: a new provider method (`ContinueWithToolResults`,
implemented for all adapters) and an `approval_policy` field on `ProviderConfig` (default `auto` →
unchanged behavior). New `internal/tools` package; router drives the dispatch loop; `start.go`
constructs the registry and wires the GC issuer. Rollback is reverting the package + the router/
start wiring + the two provider/config additions. With no tools registered the router behaves exactly
as Phase 7. New spec deltas: `tool-injection` (new) and `provider-adapter-layer` (added requirement).

## Open Questions

- **Tool-turn cap value** — reuse `agent.max_turns` vs. a dedicated cap; start with a dedicated
  constant (e.g. 10 tool turns per role turn) and revisit if real runs need tuning.
- **Plugin loading mechanism** — compile-time registration vs. Go plugins (`plugin` pkg) vs. external
  process; start with compile-time registration of `ToolPlugin`s (the interface is the contract),
  leaving dynamic loading to Phase 18 extensibility.
- **Where approval state will live** — when Phase 14 adds the channel, decide whether pending
  approvals sit in the runtime state or a table; out of scope here (denial path only).
