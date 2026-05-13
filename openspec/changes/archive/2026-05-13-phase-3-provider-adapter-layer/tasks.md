## 1. Public Types and Surface (`internal/provider`)

- [x] 1.1 Add `event.go`: `AgentEvent`, `EventType` enum (`EventAssistantStart`, `EventAssistantDelta`, `EventToolCall`, `EventUsage`, `EventContextWarning`, `EventError`, `EventTurnEnd`), `ToolCall` struct (ID, Name, Arguments json.RawMessage)
- [x] 1.2 Add `tool.go`: `ToolSpec` struct (Name, Description, Parameters json.RawMessage) for SPEC §7.3 forward-compat — Phase 13 will populate it
- [x] 1.3 Add `usage.go`: `TokenUsage{Prompt, Completion, Total int}` with an `Add` helper
- [x] 1.4 Add `session.go`: `Session` struct exposing `ID() string`, `Provider() string`, `Workspace() string`, an unexported `closed bool`, and an unexported `payload any` for per-adapter state
- [x] 1.5 Add `stream.go`: `TurnStream` interface plus a concrete `bufferedStream` with `Events() <-chan AgentEvent`, `Wait() TurnResult`, and `TurnResult{Text string, ToolCalls []ToolCall, Usage TokenUsage, Err error}`
- [x] 1.6 Add `adapter.go`: `Adapter` interface (`CreateSession`, `StartTurn`, `ContinueTurn`, `EndSession`, `GetUsage`) plus `Option` functional-options type and `WithHTTPClient`, `WithLogger`, `WithClock` constructors
- [x] 1.7 Add `adapter_test.go` covering: `var _ Adapter = ...` compile-time assertions, `bufferedStream` close semantics, `TurnResult.Text` aggregation from deltas, `TokenUsage.Add`

## 2. Error Wrapping Helpers

- [x] 2.1 Add `errors.go` helpers (next to the existing sentinels): `wrapRequest(provider, op, err) error` returning `fmt.Errorf("provider: %s: %s: %w", provider, op, errors.Join(err, ErrRequestFailed))`, plus `wrapStream`, `wrapTimeout`. Keep the existing sentinels unchanged.
- [x] 2.2 Confirm `internal/harness/errors_test.go` still passes — the sentinel registry test must remain green
- [x] 2.3 Add `errors_test.go` covering: a wrapped request error satisfies both `errors.Is(err, ErrRequestFailed)` and contains the operation name in its message

## 3. SSE Reader (`internal/provider/sse.go`)

- [x] 3.1 Implement `sseFrame{Event, Data string}` and `sseReader{br *bufio.Reader, max int}` with `Next(ctx) (sseFrame, error)` reading until `\n\n`
- [x] 3.2 Use a 1 MB buffer to accommodate long Anthropic content blocks; over-long lines return `ErrStreamError`
- [x] 3.3 Distinguish `io.EOF` (clean stream end) from `io.ErrUnexpectedEOF` and other read errors (wrap `ErrRequestFailed`)
- [x] 3.4 Honour `ctx.Done()` between frames so callers can cancel cleanly
- [x] 3.5 Add `sse_test.go` covering: single-line frame, multi-line `data:` continuation, `event:` + `data:` pairing, malformed framing → `ErrStreamError`, ctx cancellation mid-stream

## 4. HTTP Client Plumbing (`internal/provider/httpclient.go`)

- [x] 4.1 Define a package-internal `httpDoer` interface compatible with `*http.Client` so tests can inject a fake
- [x] 4.2 Default HTTP client: `&http.Client{Transport: http.DefaultTransport.Clone(), Timeout: 0}` (no overall timeout once streaming starts; per-request ctx deadline handles connect + first byte)
- [x] 4.3 Helper `readErrorBody(resp) error` that reads up to 4 KB of the response body, includes status code + truncated body in the error, and wraps `ErrRequestFailed`
- [x] 4.4 Helper `setStandardHeaders(req, cfg)` for `Content-Type: application/json` + `Accept: text/event-stream`
- [x] 4.5 Add `httpclient_test.go` covering: 401 + JSON error body returns wrapped ErrRequestFailed with code, 5xx + plain-text body is captured, empty body still produces a usable error

## 5. OpenAI Adapter (`internal/provider/openai.go`)

- [x] 5.1 Define `openaiAdapter` struct (cfg, http, log, clock, baseURL, authHeader, extraHeaders) and `openaiSession` payload (messages []map[string]any, usage TokenUsage, warned bool)
- [x] 5.2 `CreateSession`: build a session with a UUID, store `system_prompt` placeholder slot, return `*Session{payload: &openaiSession{...}}`
- [x] 5.3 `StartTurn`/`ContinueTurn`: build a chat-completions request body — `model`, `messages` (appending the prompt as `{role: "user", content: prompt}`), `tools` (mapped from `[]ToolSpec`), `stream: true`, `stream_options: {include_usage: true}`, `max_tokens` from cfg, `temperature` if set
- [x] 5.4 Issue `POST {baseURL}/chat/completions`, validate 200 OK, hand the response body to the SSE reader, spawn a goroutine that translates frames into `AgentEvent`s on the stream's channel
- [x] 5.5 Frame translation: `[DONE]` → `EventTurnEnd`; `delta.content` → `EventAssistantDelta`; `delta.tool_calls[*]` → `EventToolCall` (accumulate fragmented argument strings until the call is complete); final `usage` frame → `EventUsage` + cumulative-budget check
- [x] 5.6 Append the assistant's final text + tool calls back into `openaiSession.messages` so the next turn carries them
- [x] 5.7 `EndSession`: clear the payload, set `closed = true`
- [x] 5.8 `GetUsage`: return cumulative usage from the session payload
- [x] 5.9 Add `openai_test.go` driven by `httptest.Server` + fixtures: assistant-text streaming, tool-call streaming, usage accounting across two turns, 401 error path, malformed SSE path, ctx cancellation

## 6. Anthropic Adapter (`internal/provider/anthropic.go`)

- [x] 6.1 Define `anthropicAdapter` and `anthropicSession` (messages []anthropicMessage, usage, warned)
- [x] 6.2 `StartTurn`/`ContinueTurn` body: `model`, `system` (if set in extra), `messages`, `max_tokens`, `tools` mapped from `[]ToolSpec`, `stream: true`. Headers: `x-api-key`, `anthropic-version: 2023-06-01`
- [x] 6.3 Frame translation per Anthropic event names:
    - `message_start` → capture initial usage; `EventAssistantStart`
    - `content_block_start` (`text` or `tool_use`) → start text/tool-call accumulation
    - `content_block_delta` (`text_delta` / `input_json_delta`) → `EventAssistantDelta` or extend the in-flight tool call's arguments
    - `content_block_stop` → emit any complete `EventToolCall`
    - `message_delta` → update usage delta; trigger 80% warning check
    - `message_stop` → `EventTurnEnd`, close channel
- [x] 6.4 Append the model's final content blocks into `anthropicSession.messages` as a single `assistant` message
- [x] 6.5 Add `anthropic_test.go` mirroring the OpenAI coverage matrix: text, tool calls, usage, error paths, cancellation

## 7. OpenRouter Adapter (`internal/provider/openrouter.go`)

- [x] 7.1 Define `openrouterAdapter` as a thin wrapper that embeds an `openaiAdapter` with `baseURL = "https://openrouter.ai/api/v1"` and extra headers `HTTP-Referer: <server.host>` (from cfg or "https://conductor.dev"), `X-Title: Conductor`
- [x] 7.2 Forward `CreateSession` / `StartTurn` / `ContinueTurn` / `EndSession` / `GetUsage` to the embedded `openaiAdapter` — no behavior changes
- [x] 7.3 Add `openrouter_test.go` asserting that the embedded adapter sets the two required headers on every request

## 8. Factory and Validation

- [x] 8.1 Add `factory.go`: `provider.New(cfg, opts...)` switch over `cfg.Provider` returning the matching adapter or `ErrUnsupportedProvider`
- [x] 8.2 Add `validate.go`: `Validate(p config.Providers) error` enforcing the rules in §spec.md "Provider configuration validation"
- [x] 8.3 Add `factory_test.go` covering each supported kind + the unknown-kind error path
- [x] 8.4 Add `validate_test.go` covering: clean case, missing api_key (default), missing api_key (role override), unsupported provider, custom without base_url, invalid compaction_strategy, zero max_tokens (rejected), localhost loopback allows empty api_key

## 9. Harness Validator Integration

- [x] 9.1 In `internal/harness/validator.go`, call `provider.Validate(cfg.Providers)` and append its return to the joined error
- [x] 9.2 Update `internal/harness/validator_test.go` to add a test scenario that supplies a bad `providers.default` and asserts the joined error surfaces `provider.ErrUnsupportedProvider`
- [x] 9.3 Confirm `internal/harness/errors_test.go` (the SPEC §23 registry test) still passes with no edits — provider sentinels are unchanged

## 10. Fixtures (`internal/provider/testdata/`)

- [x] 10.1 `anthropic_text.sse` — three text-delta frames + `message_stop` for the assistant-text scenario
- [x] 10.2 `anthropic_tool_use.sse` — a `tool_use` content_block_start + `input_json_delta` frames + `content_block_stop`
- [x] 10.3 `anthropic_usage.sse` — frames whose `message_delta.usage` totals match the per-session expectations
- [x] 10.4 `openai_text.sse` — three `data:` frames with content deltas + `data: [DONE]`
- [x] 10.5 `openai_tool_use.sse` — frames demonstrating fragmented `tool_calls[0].function.arguments` reassembly
- [x] 10.6 `openai_usage.sse` — frames whose final `usage` chunk drives the accounting tests
- [x] 10.7 `openai_error_401.json` — body served alongside an HTTP 401 to test the error path
- [x] 10.8 Each fixture has a short header comment naming the provider API version it was recorded against

## 11. Doc, Lint, and Coverage

- [x] 11.1 Expand `internal/provider/doc.go` to enumerate the Phase-3 files (mirroring the Phase 2 doc.go expansion in `internal/harness`)
- [x] 11.2 Run `go build ./...` and confirm the binary compiles
- [x] 11.3 Run `go vet ./...` and confirm no findings
- [x] 11.4 Run `golangci-lint run ./...` if the tool is available locally; if not, document the skipped check
- [x] 11.5 Run `go test ./internal/provider/...` and confirm all unit tests pass on Windows
- [x] 11.6 Run `go test -cover ./internal/provider/...` and confirm coverage ≥ 70% for the new package
- [x] 11.7 Run `make test` to ensure the full suite still passes (harness + provider together)

## 12. Phase Notes

- [x] 12.1 Update `docs/phases.md` if any deliverable text needs sharpening based on the implementation (no behavior change — only doc clarity)
- [x] 12.2 No CLI changes required for Phase 3 — `conductor harness validate` already surfaces the new joined provider errors transparently
