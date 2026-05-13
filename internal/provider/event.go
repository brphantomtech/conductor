package provider

import "encoding/json"

// EventType discriminates the kinds of agent events an adapter may publish
// on a TurnStream's events channel. The values are stable across providers
// — each concrete adapter normalizes its provider-specific framing into
// this set so the orchestrator, router, and dashboard see a uniform stream.
type EventType string

// Agent event types. The string values are not persisted anywhere yet
// (Phase 14 will route some to the WebSocket), so they are descriptive
// rather than canonical SPEC identifiers.
const (
	// EventAssistantStart fires once per turn when the provider begins
	// emitting assistant content.
	EventAssistantStart EventType = "assistant_start"

	// EventAssistantDelta carries an incremental chunk of assistant text.
	EventAssistantDelta EventType = "assistant_delta"

	// EventToolCall fires when the assistant has emitted a complete tool
	// invocation. Fragmented arguments (OpenAI) are accumulated by the
	// adapter; consumers see a single event per call.
	EventToolCall EventType = "tool_call"

	// EventUsage carries the final usage payload for the turn.
	EventUsage EventType = "usage"

	// EventContextWarning fires the first time cumulative session usage
	// crosses 80% of ProviderConfig.ContextBudget. SPEC §7.4.
	EventContextWarning EventType = "context_warning"

	// EventError carries a terminal stream error. The events channel is
	// closed immediately after this event is emitted.
	EventError EventType = "error"

	// EventTurnEnd is the last event on a successful stream. The events
	// channel is closed immediately after.
	EventTurnEnd EventType = "turn_end"
)

// AgentEvent is one normalized event from an adapter's streaming response.
// Only the fields relevant to the event's Type are populated; consumers
// should branch on Type before reading other fields.
type AgentEvent struct {
	// Type discriminates the event variant.
	Type EventType

	// Text carries assistant text for EventAssistantDelta.
	Text string

	// ToolCall carries the parsed tool invocation for EventToolCall.
	ToolCall ToolCall

	// Usage carries the cumulative session usage for EventUsage and the
	// delta-at-warning for EventContextWarning.
	Usage TokenUsage

	// Err carries the terminal error for EventError.
	Err error
}

// ToolCall is a normalized representation of a tool invocation the
// assistant emitted. The Arguments field holds the provider's native
// argument JSON verbatim so a Phase 13 tool dispatcher can round-trip it
// back to the model without re-encoding.
type ToolCall struct {
	// ID is the provider's call identifier — Anthropic's `toolu_…` or
	// OpenAI's `call_…` — needed to thread the tool_result back to the
	// same call.
	ID string

	// Name is the tool name from ToolSpec.Name.
	Name string

	// Arguments is the raw JSON arguments object as the provider emitted
	// it. May be `null` when the provider has yet to emit a value.
	Arguments json.RawMessage
}
