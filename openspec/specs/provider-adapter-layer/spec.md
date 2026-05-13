# provider-adapter-layer Specification

## Purpose
TBD - created by archiving change phase-3-provider-adapter-layer. Update Purpose after archive.
## Requirements
### Requirement: Stable ProviderAdapter interface
The `internal/provider` package SHALL expose an `Adapter` interface that every concrete provider implementation conforms to. The interface SHALL contain exactly five methods matching SPEC §7.1: `CreateSession(ctx, cfg, workspace) (*Session, error)`, `StartTurn(ctx, session, prompt, tools) (TurnStream, error)`, `ContinueTurn(ctx, session, prompt) (TurnStream, error)`, `EndSession(ctx, session) error`, and `GetUsage(session) TokenUsage`. The interface MUST be importable from any Tier 2+ package and is the only surface the orchestrator, router, and memory consolidator are permitted to call.

#### Scenario: Adapter interface satisfied by Anthropic implementation
- **WHEN** code declares `var _ provider.Adapter = (*anthropicAdapter)(nil)`
- **THEN** the build succeeds

#### Scenario: Adapter interface satisfied by OpenAI implementation
- **WHEN** code declares `var _ provider.Adapter = (*openaiAdapter)(nil)`
- **THEN** the build succeeds

#### Scenario: Adapter interface satisfied by OpenRouter implementation
- **WHEN** code declares `var _ provider.Adapter = (*openrouterAdapter)(nil)`
- **THEN** the build succeeds

### Requirement: Adapter construction by provider kind
The `internal/provider` package SHALL expose `New(cfg config.ProviderConfig, opts ...Option) (Adapter, error)` that returns the adapter implementation whose name matches `cfg.Provider`. Supported provider kinds in Phase 3 are `anthropic`, `openai`, and `openrouter`. Any other value SHALL return an error satisfying `errors.Is(err, provider.ErrUnsupportedProvider)`. The factory SHALL accept functional options for the HTTP client, the logger, and the clock function so tests can inject deterministic dependencies.

#### Scenario: Anthropic kind returns Anthropic adapter
- **WHEN** invoking `provider.New(config.ProviderConfig{Provider: "anthropic", Model: "claude-sonnet-4-6", APIKey: "sk-test", MaxTokens: 1024})`
- **THEN** the returned adapter is non-nil and its `CreateSession` calls `https://api.anthropic.com/v1/messages` by default

#### Scenario: OpenAI kind returns OpenAI adapter
- **WHEN** invoking `provider.New(config.ProviderConfig{Provider: "openai", Model: "gpt-4o", APIKey: "sk-test", MaxTokens: 1024})`
- **THEN** the returned adapter is non-nil and its `CreateSession` calls `https://api.openai.com/v1/chat/completions` by default

#### Scenario: OpenRouter kind returns OpenRouter adapter
- **WHEN** invoking `provider.New(config.ProviderConfig{Provider: "openrouter", Model: "anthropic/claude-sonnet-4.6", APIKey: "or-test", MaxTokens: 1024})`
- **THEN** the returned adapter is non-nil and its first request includes both `HTTP-Referer` and `X-Title: Conductor` headers

#### Scenario: Unknown provider kind is rejected
- **WHEN** invoking `provider.New(config.ProviderConfig{Provider: "unknown-provider", Model: "x", APIKey: "y"})`
- **THEN** the returned error satisfies `errors.Is(err, provider.ErrUnsupportedProvider)`

### Requirement: Provider configuration validation
The `internal/provider` package SHALL expose `Validate(p config.Providers) error` that returns `errors.Join` over every problem in the merged providers block. Validation SHALL enforce: (1) `providers.default.provider` is in the supported list; (2) `api_key` is non-empty for every provider except those whose base URL is on the loopback interface (where auth is optional); (3) `max_tokens > 0`; (4) `compaction_strategy` is one of `none`, `summarize`, or `sliding_window` when set; (5) `provider == "custom"` requires a non-empty `base_url`. The `internal/harness` validator SHALL invoke `provider.Validate` so a HARNESS.md with a broken providers block fails startup.

#### Scenario: Valid Anthropic default passes
- **WHEN** validating a `config.Providers` whose `Default = ProviderConfig{Provider: "anthropic", Model: "claude-sonnet-4-6", APIKey: "sk-test", MaxTokens: 8192}`
- **THEN** the returned error is nil

#### Scenario: Missing api_key for remote provider is reported
- **WHEN** validating a `config.Providers` whose `Default.Provider = "anthropic"` and `Default.APIKey = ""`
- **THEN** the returned error satisfies `errors.Is(err, provider.ErrMissingAPIKey)`

#### Scenario: Unsupported provider is reported
- **WHEN** validating a `config.Providers` whose `Default.Provider = "made-up-vendor"`
- **THEN** the returned error satisfies `errors.Is(err, provider.ErrUnsupportedProvider)`

#### Scenario: Custom without base_url is reported
- **WHEN** validating a `config.Providers` whose `Default = ProviderConfig{Provider: "custom", Model: "m", APIKey: "k", MaxTokens: 1024}` with no `BaseURL`
- **THEN** the returned error wraps `provider.ErrUnsupportedProvider` with a message identifying the missing `base_url`

#### Scenario: Per-role override is validated independently
- **WHEN** validating a `config.Providers` whose `Default` is valid but whose `Roles["coder"]` is `ProviderConfig{Provider: "openai", APIKey: ""}`
- **THEN** the returned error satisfies `errors.Is(err, provider.ErrMissingAPIKey)`

### Requirement: SSE streaming for assistant text
Every adapter SHALL stream the assistant's response as a sequence of `AgentEvent` records published on the `TurnStream.Events()` channel. The first event for an assistant response SHALL be `AgentEvent{Type: EventAssistantStart}`. Incremental text SHALL be published as `AgentEvent{Type: EventAssistantDelta, Text: <chunk>}`. The final event before the channel closes SHALL be `AgentEvent{Type: EventTurnEnd}`. `TurnStream.Wait()` SHALL block until the channel closes and return a `TurnResult` containing the concatenated assistant text, accumulated usage delta for the turn, and any terminal error.

#### Scenario: Anthropic streams assistant text
- **WHEN** the Anthropic adapter consumes a recorded SSE fixture containing `message_start` + three `content_block_delta` frames + `message_stop`
- **THEN** the events channel emits one `EventAssistantStart`, three `EventAssistantDelta` with the expected text chunks, and one `EventTurnEnd`, then closes

#### Scenario: OpenAI streams assistant text
- **WHEN** the OpenAI adapter consumes a recorded SSE fixture containing three `data:` lines with `choices[0].delta.content` set and a final `data: [DONE]`
- **THEN** the events channel emits one `EventAssistantStart`, three `EventAssistantDelta`, and one `EventTurnEnd`, then closes

#### Scenario: TurnResult.Text aggregates the deltas
- **WHEN** a stream emits three `EventAssistantDelta` with text values `"Hello"`, `" "`, `"world"`
- **THEN** `TurnStream.Wait().Text == "Hello world"`

### Requirement: SSE streaming for tool calls
Adapters that support tool use SHALL surface tool-call frames as `AgentEvent{Type: EventToolCall, ToolCall: …}` records. The `ToolCall` payload SHALL include the provider's native call ID, the tool name, and the raw arguments as `json.RawMessage`. Tool results submitted on a follow-up `StartTurn` or `ContinueTurn` SHALL be threaded back through the adapter's session state so the provider sees a coherent conversation.

#### Scenario: Anthropic surfaces a tool_use content block
- **WHEN** the Anthropic adapter consumes an SSE fixture whose `content_block_start` declares `type: tool_use, id: toolu_x, name: lookup, input: {...}`
- **THEN** the events channel emits an `AgentEvent{Type: EventToolCall, ToolCall: ToolCall{ID: "toolu_x", Name: "lookup", Arguments: ...}}`

#### Scenario: OpenAI surfaces a tool_calls delta
- **WHEN** the OpenAI adapter consumes an SSE fixture whose `choices[0].delta.tool_calls[0]` declares `id: call_y, function: {name: lookup, arguments: "{}"}`
- **THEN** the events channel emits an `AgentEvent{Type: EventToolCall, ToolCall: ToolCall{ID: "call_y", Name: "lookup", Arguments: json.RawMessage("{}")}}`

### Requirement: Token usage accounting
Each adapter SHALL maintain a cumulative `TokenUsage{Prompt, Completion, Total}` per `Session` and SHALL update it from the provider's reported usage payload at the end of each turn. `Adapter.GetUsage(session) TokenUsage` SHALL return the cumulative total for the session, not the last-turn delta. `TurnResult.Usage` SHALL contain the last-turn delta.

#### Scenario: Cumulative usage tracks across turns
- **WHEN** an Anthropic session has two completed turns reporting `{prompt:10, completion:20}` and `{prompt:15, completion:25}` respectively
- **THEN** `adapter.GetUsage(session) == TokenUsage{Prompt: 25, Completion: 45, Total: 70}`

#### Scenario: TurnResult.Usage is the delta only
- **WHEN** an OpenAI session's second turn reports `usage: {prompt_tokens: 15, completion_tokens: 25, total_tokens: 40}`
- **THEN** the second turn's `TurnResult.Usage == TokenUsage{Prompt: 15, Completion: 25, Total: 40}`

### Requirement: Context budget warning at 80%
When `ProviderConfig.ContextBudget` is greater than zero, the adapter SHALL emit a single `AgentEvent{Type: EventContextWarning}` on the events channel the first time the cumulative session usage crosses 80% of the budget. The warning SHALL fire at most once per session — subsequent turns SHALL NOT re-emit it even if usage remains above the threshold. When `ContextBudget` is zero (the SPEC §4.1.3 default), no warning SHALL ever fire.

#### Scenario: Warning fires when crossing 80% on first turn
- **WHEN** a session is configured with `ContextBudget: 1000` and a single turn reports usage `{total: 850}`
- **THEN** the turn's events channel emits exactly one `EventContextWarning` before `EventTurnEnd`

#### Scenario: Warning is suppressed on subsequent turns
- **WHEN** the same session runs a second turn that pushes total usage from 850 to 920
- **THEN** the second turn's events channel does not include any `EventContextWarning`

#### Scenario: No warning when budget is zero
- **WHEN** a session is configured with `ContextBudget: 0` and any number of turns
- **THEN** the events channel never emits `EventContextWarning`

### Requirement: Error classification
Adapter operations SHALL surface failures using the SPEC §23.3 sentinel set. Transport-level failures (DNS, TCP, TLS, HTTP 4xx/5xx before streaming begins) SHALL wrap `provider.ErrRequestFailed`. Stream-level failures (malformed SSE frames, JSON decode errors, premature stream termination) SHALL wrap `provider.ErrStreamError`. Deadline-exceeded errors SHALL wrap `provider.ErrResponseTimeout`. The "agent requested human input" condition SHALL surface `provider.ErrTurnInputRequired`. Empty `api_key` at construction time SHALL surface `provider.ErrMissingAPIKey`. Unknown provider kinds SHALL surface `provider.ErrUnsupportedProvider`.

#### Scenario: 401 response surfaces ErrRequestFailed
- **WHEN** an adapter calls a server that returns HTTP 401 with body `{"error":{"type":"authentication_error"}}`
- **THEN** the error returned from `StartTurn` satisfies `errors.Is(err, provider.ErrRequestFailed)`

#### Scenario: Malformed SSE frame surfaces ErrStreamError
- **WHEN** an adapter reads a frame whose `data:` payload is not valid JSON
- **THEN** the terminal `AgentEvent` is `EventError` and `TurnResult.Err` satisfies `errors.Is(err, provider.ErrStreamError)`

#### Scenario: Context deadline surfaces ErrResponseTimeout
- **WHEN** a caller passes a `context.Context` that is already cancelled with `context.DeadlineExceeded` when `StartTurn` is invoked
- **THEN** the returned error satisfies `errors.Is(err, provider.ErrResponseTimeout)`

#### Scenario: Empty api_key fails CreateSession for remote provider
- **WHEN** an adapter is constructed with `ProviderConfig{Provider: "anthropic", APIKey: ""}` and `CreateSession` is invoked
- **THEN** the returned error satisfies `errors.Is(err, provider.ErrMissingAPIKey)`

### Requirement: Session lifecycle
`Adapter.CreateSession` SHALL return a `*Session` whose `ID()` is a stable, unique identifier; `Workspace()` is the workspace key passed in; and `Provider()` is the provider kind. `Adapter.EndSession` SHALL release any per-session resources (cached message history, accumulated usage). Subsequent `StartTurn` / `ContinueTurn` calls against a closed session SHALL return an error.

#### Scenario: CreateSession assigns a unique ID
- **WHEN** an adapter is asked to create two sessions for the same workspace
- **THEN** the two sessions have distinct `ID()` values

#### Scenario: EndSession releases state
- **WHEN** a session has completed two turns and `EndSession` is invoked
- **THEN** subsequent calls to `StartTurn` against the same session return a non-nil error

### Requirement: Tests use recorded fixtures only
All adapter unit tests SHALL exercise their HTTP code paths against an `httptest.Server` configured with payloads stored under `internal/provider/testdata/`. The test suite SHALL NOT make outbound network calls and SHALL pass with no network connectivity. Integration tests against live provider APIs MAY exist under the `//go:build integration` tag and SHALL NOT run in `make test`.

#### Scenario: Test run with network disabled passes
- **WHEN** the test runner runs `go test ./internal/provider/...` in an environment with no outbound network access
- **THEN** all tests pass

#### Scenario: Integration tag is opt-in
- **WHEN** the test runner runs `make test`
- **THEN** files tagged `//go:build integration` are not compiled or executed
