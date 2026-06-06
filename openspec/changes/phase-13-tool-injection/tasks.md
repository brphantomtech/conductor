# Phase 13 — Atomic Tasks (Conductor Tool Injection)

Phase 13 goal (per [docs/phases.md](../../../docs/phases.md)): built-in tools available to agents on
every session. SPEC §7.3, §20.5, §21.1, §21.2. Sequential convergence phase — consumes Phases 4, 8,
9, 10, 11, 12. Keep tool logic in `internal/tools`; confine provider/config/router/start edits to
what each task names; touch shared wiring (`start.go`, `config/types.go`) in focused commits. Each
task is sized for one focused session.

## 1. Tool Model & Registry

- [x] 1.1 Define the `Tool` interface (`Name`, `Description`, `ParametersSchema() json.RawMessage`, `Execute(ctx, params map[string]any, ExecutionContext) (ToolResult, error)`), `ToolResult`, and `ExecutionContext` (carries Conductor-injected credentials/handles, never agent-supplied).
- [x] 1.2 Implement `Registry`: register tools, advertise them as `[]provider.ToolSpec` (name/description/parameters schema), look up by name.
- [x] 1.3 Unit tests: registry advertises tool specs; lookup by name; unknown name errors.

## 2. Provider Tool-Result Continuation (SPEC §7.1)

- [x] 2.1 Add `ContinueWithToolResults(ctx, s, []ToolResult) (TurnStream, error)` to the provider `Adapter` interface.
- [x] 2.2 Implement it for Anthropic (`tool_result` content blocks) and the OpenAI-compatible path (`role:"tool"` messages, shared by OpenAI/OpenRouter); error when the session emitted no tool call.
- [x] 2.3 Unit tests (recorded fixtures): tool result sent in native shape returns a TurnStream; OpenAI-compatible share the path; no-prior-tool-call errors.

## 3. Built-in Tools

- [x] 3.1 `conductor_tracker_query` / `conductor_tracker_mutate` over the tracker `ExecuteQuery`/`ExecuteMutation` (consumer-side `trackerExecutor` interface).
- [x] 3.2 `conductor_knowledge_search` (hybrid search) and `conductor_doc_search` (doc search) over consumer-side interfaces.
- [x] 3.3 `conductor_memory_read` / `conductor_memory_write` over the Memory Manager.
- [x] 3.4 `conductor_harness_check` (enforcer rule run) and `conductor_validation_run` (validation pipeline) over consumer-side interfaces.
- [x] 3.5 Unit tests: each tool dispatches to a fake engine and returns a structured result; parameter schema validation.

## 4. Dispatcher & Execution Loop

- [x] 4.1 Implement the `Dispatcher`: route a tool call to the registered tool, marshal params/result, emit `ToolCalled`/`ToolResult` audit events (through the redactor); tool failure → error result, not abort.
- [x] 4.2 Drive the execution loop in the router: on `EventToolCall`(s), dispatch then `ContinueWithToolResults`, repeat until no tool calls or the tool-turn cap; end with a diagnostic at the cap.
- [x] 4.3 Unit tests (fake model + fake tools): tool result continues the turn; tool failure surfaces to the model without aborting; loop bounded by the cap; audit events emitted.

## 5. Approval Policy & Credential Isolation

- [x] 5.1 Add `approval_policy` to `ProviderConfig` (`auto` default, `review_destructive`, `manual`) in `internal/config/types.go`.
- [x] 5.2 Implement the pure `approve(tool, policy)` decision: `auto` allow; `review_destructive` deny destructive tools (mark `conductor_tracker_mutate` destructive); `manual` deny all — denial returns a structured "approval required" result.
- [x] 5.3 Credential isolation: tools read credentials from `ExecutionContext` (Conductor config), never agent params; audit payloads redacted (reuse the Phase 17 redactor).
- [x] 5.4 Unit tests: each policy's allow/deny; tracker_mutate denied under review_destructive; token never appears in tool params/result or audit payload.

## 6. Plugin Interface

- [x] 6.1 Define `ToolPlugin` (SPEC §20.5) and a compile-time registration mechanism that advertises plugins alongside built-ins (dynamic loading deferred to Phase 18).
- [x] 6.2 Unit tests: a sample plugin registers, is advertised, and executes via the dispatcher like a built-in.

## 7. Integration

- [x] 7.1 In `cmd/conductor/cmd/start.go`, construct the registry from the wired engines and inject it into the router; bound the tool-turn cap.
- [x] 7.2 Implement the Phase 12 GC `harness.TrackerIssuer` (`OpenGCRuleIDs`, `CreateGCIssue`) over the tracker adapter and wire it into `wireEnforcer` (closing the deferred Phase 12 follow-up). GitHub (REST) is implemented and wired; Linear (GraphQL) is left behind a clear `// TODO(phase-13)` returning nil (GC stays a no-op there), per the phase guidance to implement one cleanly with the build green.
- [x] 7.3 Update `AGENTS.md` navigation for `internal/tools`.

## 8. Verification

- [x] 8.1 `go build ./...` and `go vet ./...` clean; repo linter passes for `internal/tools` and the provider/router/config edits; existing tests still green.
- [x] 8.2 `go test ./...` passes; `internal/tools` coverage ≥ 70%.
- [x] 8.3 Smoke: a fake-model turn that calls `conductor_knowledge_search` then a denied `conductor_tracker_mutate` (under `review_destructive`) runs through the dispatch loop, continues with results, and records `ToolCalled`/`ToolResult` audit events.
