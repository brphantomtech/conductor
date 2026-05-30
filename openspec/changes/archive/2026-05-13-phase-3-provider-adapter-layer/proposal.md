## Why

Phase 1 delivered the binary skeleton and Phase 2 wired up HARNESS.md loading + hot reload, but `internal/provider` still contains only error sentinels and a `doc.go`. No code in the repository can actually exchange messages with an LLM, so the orchestrator, router, memory consolidator, and tool-calling layers in Phases 6–13 have nothing to call. Phase 3 closes that gap by shipping a stable `ProviderAdapter` interface plus working adapters for Anthropic, OpenAI, and OpenRouter — enough for every downstream phase to begin development against a real seam.

## What Changes

- New `internal/provider` public surface: `Adapter` interface, `Session`, `TurnStream`, `AgentEvent` family (assistant text, tool call, tool result, usage, error, done), `ToolSpec`, `TurnResult`, `TokenUsage`. The interface in SPEC §7.1 is the contract; existing sentinels in `errors.go` are reused as classifiers.
- Three concrete adapters share a small HTTP-driven core:
  - `anthropicAdapter` — Anthropic Messages API, `x-api-key` auth, `anthropic-version: 2023-06-01`, SSE parser for `message_start`/`content_block_delta`/`message_delta`/`message_stop` events.
  - `openaiAdapter` — OpenAI Chat Completions API, `Authorization: Bearer …`, SSE parser for `data:`-prefixed chunked completions and the `[DONE]` sentinel.
  - `openrouterAdapter` — thin wrapper over the OpenAI adapter that sets `HTTP-Referer` + `X-Title` headers and points at `https://openrouter.ai/api/v1`. Ollama / LM Studio / `custom` reuse the OpenAI code path through a `baseURL` injection point — they will be wired in later phases without changing the interface.
- Provider construction is centralized in `provider.New(cfg)` which maps `ProviderConfig.Provider` → adapter constructor and returns `ErrUnsupportedProvider` for unknown values. `$VAR` substitution on `api_key` is already done upstream by `internal/config`; an empty string surfaces `ErrMissingAPIKey`.
- Streaming: a generic `sseReader` consumes an `io.Reader`, yields normalized `AgentEvent`s on a buffered channel, and surfaces transport / parse failures as `ErrRequestFailed` / `ErrStreamError`. Backpressure is handled by the buffered channel + a `done` signal — readers that stop consuming will not stall the HTTP client.
- Token usage accounting per session: the adapter tracks cumulative `prompt_tokens` and `completion_tokens`, exposes them via `GetUsage`, and emits a `ContextWarning` channel event when the running total crosses 80% of `context_budget`. Actual compaction (`summarize` / `sliding_window`) stays a Phase 9 deliverable per `docs/phases.md`.
- Provider config validation at startup: `provider.Validate(cfg)` enforces supported-provider list, non-empty api_key when required, non-zero `max_tokens`, and that `custom` requires a `base_url`. Called from `harness.Validate` so a bad HARNESS.md fails fast.
- Unit tests use a `httptest.Server` to play back recorded SSE fixtures — no live API calls in CI. Fixtures live under `internal/provider/testdata/` and cover: assistant text streaming, tool-call streaming, usage accounting, malformed-SSE → `ErrStreamError`, 401 → `ErrRequestFailed`, deadline → `ErrResponseTimeout`.

## Capabilities

### New Capabilities

- `provider-adapter-layer`: the stable `ProviderAdapter` surface, the three concrete adapters (Anthropic, OpenAI, OpenRouter), the SSE streaming core, token-usage accounting + 80%-budget warning, startup provider-config validation, and the `provider.New` factory.

### Modified Capabilities

None. The `harness-loader` spec already references `providers.default.provider` validation as part of its startup-validation requirement; Phase 3 satisfies that contract from inside `internal/provider` without changing the `harness-loader` requirement text. No other capability specs exist yet.

## Impact

- **Code (new files):** `internal/provider/{adapter,session,event,tool,usage,errors,sse,httpclient,validate,factory,anthropic,openai,openrouter}.go` plus matching `_test.go` files, plus `internal/provider/testdata/*.sse` fixtures.
- **Code (modified):** `internal/harness/validator.go` to call `provider.Validate` on the merged config; `internal/provider/doc.go` to document the Phase-3 shape.
- **Audit events:** No new event types — Phase 3 emits `ContextWarning` only as an in-process channel event, not an audit event. `EventContextCompacted` is already in the registry from Phase 1 and remains unemitted until Phase 9.
- **Dependencies:** No new external Go modules. Standard-library `net/http` and `encoding/json` carry the load; `bufio` parses SSE. Anthropic / OpenAI client SDKs are intentionally NOT pulled in — keeping the wire format under our control avoids version drift and shrinks the binary.
- **Backward compatibility:** New package surface; nothing in Phases 1–2 imports `internal/provider` yet, so there is nothing to break.
- **Out of scope (deferred):**
  - Compaction (`summarize`, `sliding_window`) → Phase 9.
  - Conductor tool injection (`conductor_tracker_query`, etc.) → Phase 13.
  - Ollama, LM Studio, `custom` adapter wiring beyond reusing the OpenAI code path → Phase 13 / config-only follow-ups.
  - Streaming over WebSocket to the dashboard → Phase 14.
