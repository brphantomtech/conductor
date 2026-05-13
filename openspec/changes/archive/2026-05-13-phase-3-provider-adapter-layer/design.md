## Context

`internal/provider` currently contains only error sentinels (Phase 1) and a package doc. SPEC §7 specifies an abstract `ProviderAdapter` interface, six concrete adapter kinds, SSE-based streaming, a Conductor tool injection layer, and a context-budget compaction policy. Phase 3's scope (per `docs/phases.md`) is everything in §7 EXCEPT compaction (Phase 9) and the Conductor built-in tool implementations (Phase 13). The three OpenAI-compatible adapters (`openai`, `openrouter`, plus the future `ollama` / `lm_studio` / `custom`) share a wire format; the `anthropic` adapter has its own. Both speak SSE.

Downstream consumers in Phases 6–13 will hold a `provider.Adapter` constructed by a `provider.Factory` keyed off `ProviderConfig.Provider`. The orchestrator (Phase 6) and router (Phase 7) will own session lifecycles and call `StartTurn` / `ContinueTurn`; Phase 9's memory consolidator and Phase 13's Conductor tools wrap the same surface. Keeping the interface stable in Phase 3 lets all later phases build against a real seam without churn.

The constraint set is unusual for an HTTP client package: provider APIs differ in tool-use payload shape (Anthropic uses `tool_use` content blocks, OpenAI uses a `tool_calls` array), in auth headers (`x-api-key` vs `Authorization: Bearer`), and in SSE framing (Anthropic uses named events like `message_start`, OpenAI emits unnamed `data:` lines with a `[DONE]` sentinel). The adapter layer must normalize all of these into a single `AgentEvent` stream while preserving enough provider-specific metadata for downstream phases (especially Phase 13's tool dispatcher) to round-trip tool results back to the model.

## Goals / Non-Goals

**Goals:**
- Implement SPEC §7.1 verbatim as the `Adapter` interface in `internal/provider`. Five methods: `CreateSession`, `StartTurn`, `ContinueTurn`, `EndSession`, `GetUsage`.
- Implement three concrete adapters: `anthropic`, `openai`, `openrouter`. The OpenAI adapter must be parameterizable on base URL, default headers, and auth scheme so that `openrouter`, `ollama`, `lm_studio`, and `custom` reuse it through composition (only `openrouter` is wired in this phase per `docs/phases.md`; the other three are config-time additions in later phases).
- Stream agent output as `AgentEvent` records over a buffered Go channel. The stream interface exposes `Events() <-chan AgentEvent` plus a `Wait() TurnResult` that blocks until terminal.
- Track per-session token usage cumulatively and emit a `ContextWarning` event when usage crosses 80% of `ProviderConfig.ContextBudget`. Stop at the warning — actual compaction is Phase 9.
- Add `provider.Validate(cfg config.Providers) error` that the harness validator calls. Surface missing api_key, unsupported provider, custom-without-base_url, and zero/negative `max_tokens`.
- Drive unit tests entirely from `httptest.Server` instances that replay recorded SSE fixtures from `testdata/`. CI never makes a live API call.

**Non-Goals:**
- Compaction (`summarize`, `sliding_window`) — Phase 9 finishes that, including `ContextCompacted` audit event emission.
- The built-in Conductor tools (`conductor_tracker_query`, `conductor_knowledge_search`, etc.) — Phase 13. The `Adapter.StartTurn` signature takes `[]ToolSpec` so Phase 13 can pass them through without breaking the surface.
- Real Ollama, LM Studio, and `custom` adapter constructors — they reuse the OpenAI implementation but their dedicated factory entries land with Phase 13.
- Plugin loading for third-party adapters — Phase 18.
- Audit event emission from inside the adapter. Phase 3's only emitted signal is the in-process `ContextWarning` event on the `AgentEvent` channel. Phases 6 and 9 will translate these into audit events at the orchestrator boundary.

## Decisions

### D1. Speak the wire format directly; no provider SDKs

**Choice.** Implement Anthropic / OpenAI / OpenRouter as plain `net/http` clients with hand-rolled JSON request bodies and an SSE reader. No `github.com/anthropic-sdk/...` or `github.com/sashabaranov/go-openai` imports.

**Why.** Provider SDKs lag the wire format by weeks, balloon the dependency graph, and surface their own retry / streaming quirks. The wire format is small (10–20 fields per request) and stable enough that maintaining our own request structs is cheaper than tracking three SDK release trains. It also keeps the cgo-free single-binary contract intact (SPEC §3).

**Alternatives considered.** (a) Pull in upstream SDKs — adds ~5 MB to the binary, introduces transitive deps, and forces us to translate SDK errors into our sentinel set anyway. (b) Use the OpenRouter "universal" gateway for everything — couples the project to a third-party paid service for local dev and CI, and forfeits the Anthropic prompt-cache headers we will want in Phase 9.

### D2. Adapter interface lives in `internal/provider`, not at a consumer

**Choice.** The public `Adapter` interface is exported from `internal/provider` directly, even though `docs/conventions.md §3.1` prefers consumer-defined interfaces.

**Why.** SPEC §20.2 lists `ProviderAdapter` as one of the five public extension points third parties can implement, alongside `TrackerAdapter`, `KnowledgeStore`, `DocStoreBackend`, and `ToolPlugin`. The conventions doc explicitly carves out an exception for these: "cross-cutting public extension points are defined in the implementer's home package because they are the contract third parties implement." Phase 3 follows that exception.

### D3. SSE reader is shared; framing is per-adapter

**Choice.** A package-internal `sseReader` consumes an `io.Reader`, splits on `\n\n` blank-line frames, and yields raw `sseFrame{event, data}` records. Each adapter then maps frames into `AgentEvent`s through a private `mapFrame(...)` method that owns its provider's framing dialect.

**Why.** Anthropic and OpenAI agree on the transport (SSE per RFC) and disagree on the semantics (event-name routing vs unnamed `data:` lines + `[DONE]`). Splitting the layer along that line — transport shared, semantics private — lets us add Ollama (which uses NDJSON) by writing a new reader without touching the adapters that exist. The buffered channel in `TurnStream` has capacity 16; experiments with Symphony showed that 16 is enough for normal traffic and small enough that a misbehaving consumer surfaces backpressure quickly.

**Alternatives considered.** A monolithic `sseToEvents` function that takes a "provider kind" enum and switches on it — the resulting `switch` would dwarf each adapter and live nowhere natural for tests.

### D4. Session is a value with a `Provider` discriminator, not an interface

**Choice.** `Session` is a concrete struct returned by `Adapter.CreateSession`. It exposes `ID() string`, `Provider() string`, and `Workspace() string`. Each adapter stores its own per-session state (Anthropic `messages` array, OpenAI `messages` array, accumulated `TokenUsage`) inside the struct, hidden behind the value.

**Why.** Sessions move across goroutines (the orchestrator spawns per-issue workers in Phase 6). Returning a `*Session` lets us pass it by value without copy semantics on its internal slices, and a concrete type avoids interface-level type assertions in every adapter method. Adapter implementations type-assert the session's internal payload to their own `*anthropicSession` / `*openaiSession` when called, and panic if the assertion fails — a wrong-session-for-adapter call is a programmer error, not a runtime condition.

### D5. Per-turn `TurnStream` uses an `Events()` channel + a `Wait()` method

**Choice.** `TurnStream` exposes `Events() <-chan AgentEvent` and `Wait() TurnResult`. The channel closes when the turn ends. `Wait` blocks until the channel closes, then returns the accumulated `TurnResult` (final text, tool calls, usage delta, error). The stream is one-shot.

**Why.** Callers fall into two camps: orchestrator/router code that wants to consume events as they arrive (for live streaming to the dashboard in Phase 14), and bulk consumers like the Phase 9 memory consolidator that only need the final text. The two-method shape serves both without forcing one to wrap the other. Closing the channel is the only termination signal — no separate `Done()` chan or `error` field on the stream itself.

**Alternatives considered.** A single `Wait() (TurnResult, error)` method that returns everything at the end — denies live streaming. A pure-channel API where consumers reconstruct the result from events — duplicated bookkeeping in every caller.

### D6. Token usage warning threshold is hard-coded at 80% per SPEC §7.4

**Choice.** When the cumulative usage for the session crosses 80% of `ProviderConfig.ContextBudget`, the adapter emits a single `AgentEvent{Type: EventContextWarning}` on the stream's channel and sets a per-session flag so the warning fires once per session, not once per turn.

**Why.** SPEC §7.4 specifies "At 80% of context_budget: emit ContextWarning event to orchestrator." Phase 3 produces the event; Phase 6 routes it to retry logic; Phase 9 acts on it via compaction. Keeping the threshold a constant (not config-driven) is consistent with SPEC §7.4's normative text.

### D7. Adapter factory is a single `provider.New` function with a switch

**Choice.** `func New(cfg config.ProviderConfig, opts ...Option) (Adapter, error)` dispatches on `cfg.Provider`. Options carry the `*http.Client`, logger, and clock. Unknown providers return `ErrUnsupportedProvider`.

**Why.** Five concrete kinds is small enough that a registry / plugin layer is overkill. Phase 18 will introduce a plugin loader for third-party adapters; until then, the explicit switch is the most discoverable and is what the SPEC §23.3 error already expects ("`unsupported_provider` — providers.<role>.provider value not in the supported list").

### D8. HTTP client and clock are injected via functional options

**Choice.** `provider.New(cfg, WithHTTPClient(c), WithClock(now), WithLogger(log))`. Defaults: `http.DefaultClient` with a 30 s timeout override, `time.Now`, and a `zerolog.Nop()` logger.

**Why.** Tests inject `httptest.Server.Client()` to point the adapter at the local fixture server, a deterministic clock, and a log-capture buffer for assertions. Production callers (the orchestrator in Phase 6) inject a tuned `http.Client` with connection pooling and circuit breaker. The functional-options pattern is the project's established convention (Phase 1's audit Writer uses it).

### D9. SSE parsing classifies failures by source

**Choice.** Network read errors (`io.ErrUnexpectedEOF`, `net.OpError`) wrap to `ErrRequestFailed`. JSON-decode errors on a frame's `data:` payload wrap to `ErrStreamError`. A 4xx/5xx status before streaming begins reads the body and wraps to `ErrRequestFailed` with the provider's error code in the wrapped message. A context-deadline exceeded wraps to `ErrResponseTimeout`.

**Why.** SPEC §23.3 distinguishes these three error classes precisely because callers need to react differently: transport failures retry with backoff, stream failures often indicate a malformed message and should not retry, timeouts feed the orchestrator's stall-detection. The wrapping prefix follows `docs/conventions.md §2.1`: `provider: <action>: <provider> <ids>`.

### D10. Provider config validation is in `internal/provider`, called from `internal/harness`

**Choice.** `provider.Validate(p config.Providers) error` lives in the provider package and returns an `errors.Join` of every problem found. `internal/harness/validator.go` adds a one-line call so a bad providers block in HARNESS.md fails startup. The harness validator does not duplicate the provider-specific rules.

**Why.** Putting validation in the package that owns the supported-provider list keeps the rules close to the code that enforces them. The harness validator already aggregates errors via `errors.Join`, so adding one more sub-error is mechanical.

## Risks / Trade-offs

- **Risk: SSE framing edge cases on partial buffer reads.** A buffer that splits a frame mid-`event:` line will silently drop the event in a naive scanner. → Mitigation: use `bufio.Scanner` with a custom `SplitFunc` that only emits frames terminated by `\n\n`, and grow the buffer up to 1 MB to accommodate long Anthropic content blocks.
- **Risk: Anthropic vs OpenAI tool-use payload divergence leaks into the AgentEvent type.** → Mitigation: the `AgentEvent.ToolCall` field stores a `json.RawMessage` of the provider's native arguments, plus normalized `Name` and `ID` fields. Phase 13's tool dispatcher consumes the raw form, so we do not lose fidelity.
- **Risk: 30 s default HTTP timeout is too tight for streaming, too loose for first-byte latency.** → Mitigation: split the timeout — `http.Client.Timeout = 0` (no overall deadline once streaming starts) and instead use `context.WithTimeout` on `(*http.Request).Context()` for the connect + first-byte phase with a 30 s default, then rely on `context.Context` cancellation from the caller for in-flight streams.
- **Risk: `ContextWarning` emission depends on accurate provider-reported usage; some providers under-report streaming-mode prompt tokens until message_stop.** → Mitigation: accumulate usage on `message_stop` (Anthropic) and final `usage` chunk (OpenAI). For OpenAI we explicitly request `stream_options: {"include_usage": true}`; for OpenRouter we set the same field (it forwards to the upstream model). For providers that still under-report, the warning fires on the next turn — acceptable for an 80% threshold.
- **Risk: fixture-based tests drift from real provider responses.** → Mitigation: each fixture has a comment naming the provider's API version it was recorded against (Anthropic `2023-06-01`, OpenAI `chat.completions` as of 2025-11), and a Phase-9 / Phase-13 follow-up task will rotate them with the matching live-call integration test (build tag `integration`, not run in CI).
- **Risk: `wrapcheck` will flag every `fmt.Errorf("provider: …")` inside helper functions.** → Mitigation: helpers either return raw sentinels (allowed) or use `fmt.Errorf` (in `wrapcheck` allowlist). No new lint exceptions needed.
- **Risk: depguard prevents `internal/provider` from importing other Tier-1 siblings.** → Mitigation: by design we don't need them. Provider talks to `internal/config` (T0) and `internal/audit` (T1, cross-cutting allowed). The OpenAI adapter does not call into the workspace, tracker, or harness packages — anything that needs cross-package coordination happens above us in Phase 6+.

## Migration Plan

- No data migration. New package surface.
- Branch: `phase-3-provider-adapter-layer` cut from `main` after Phase 2 merge.
- Land order inside the phase: errors-and-types → SSE reader → OpenAI adapter (most reusable) → Anthropic adapter → OpenRouter adapter → factory → validator → harness wiring → fixture tests → docs.
- Rollback: revert the merge commit. Nothing outside the package depends on it yet (Phases 6+ have not landed).

## Open Questions

1. Should we ship a `provider.Mock` exported helper for downstream packages to use in their own tests? Tentative answer: no — each consumer is going to need different behavior, and a one-size-fits-all mock encourages over-coupling. Tests in Phase 6+ will hand-roll their own fakes per `docs/conventions.md §6`.
2. How aggressive should HTTP retries be? Tentative answer: no automatic retry at all in Phase 3. The orchestrator (Phase 6) owns retry/backoff per SPEC §13.4; doubling that here would conflict with the run-attempt retry policy.
3. Should `ContextWarning` be configurable (e.g., warn at 70% instead of 80%)? Tentative answer: no, SPEC §7.4 is normative. If a real need surfaces we add it as a non-required `harness.context_warning_pct` field in a later spec amendment.
