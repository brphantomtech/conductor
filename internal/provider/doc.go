// Package provider hosts the LLM provider adapters and the abstract
// Adapter interface (SPEC §7) that every adapter implements. Phase 3
// ships:
//
//   - adapter.go    — public Adapter interface plus the Option
//     functional-options surface (HTTP client, logger, clock injection).
//   - event.go      — AgentEvent / EventType plus the ToolCall struct
//     that normalizes provider-specific tool-use payloads.
//   - tool.go       — ToolSpec for the Phase 13 Conductor tool injection
//     surface (carried in the signature now for forward compatibility).
//   - usage.go      — TokenUsage and per-session accumulation.
//   - session.go    — opaque Session with id / provider / workspace.
//   - stream.go     — TurnStream + bufferedStream + TurnResult.
//   - sse.go        — shared SSE frame reader (transport only).
//   - httpclient.go — HTTP error-body extraction + header preamble.
//   - openai.go     — OpenAI Chat Completions adapter; parameterized so
//     openrouter / ollama / lm_studio / custom can reuse it.
//   - anthropic.go  — Anthropic Messages API adapter (named SSE events).
//   - openrouter.go — thin openaiAdapter wrapper with SPEC §7.2.1
//     HTTP-Referer + X-Title headers.
//   - factory.go    — provider.New(cfg, opts...) dispatch by kind.
//   - validate.go   — provider.Validate(config.Providers) (SPEC §6.4),
//     called from internal/harness/validator.go.
//   - errors.go     — SPEC §23.3 sentinels and the wrap helpers
//     (wrapRequest / wrapStream / wrapTimeout).
//
// Compaction (`summarize`, `sliding_window`) and Conductor tool injection
// land in Phases 9 and 13 respectively; the surface in Phase 3 is shaped
// so neither needs a breaking change to plug in.
//
// Tier 1 (external adapters). Imports config, db, audit. Does not import
// any other Tier 1 sibling.
package provider
