package provider

import "encoding/json"

// ToolSpec describes a tool the provider should advertise to the agent on
// every turn. Phase 13 populates the canonical Conductor tools
// (`conductor_tracker_query`, `conductor_knowledge_search`, …); the shape is
// carried by ToolSpec so adapter signatures stay provider-neutral.
//
// Parameters holds the JSON Schema fragment the provider expects — the
// adapter forwards it verbatim through the provider's native tool-use
// payload (Anthropic `input_schema`, OpenAI `function.parameters`).
type ToolSpec struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

// ToolResult is the wire-neutral result of one executed tool call, fed back
// into a session via ContinueWithToolResults (SPEC §7.1, §7.3). The adapter
// renders it in the provider's native shape (Anthropic `tool_result` content
// block; OpenAI-compatible `role: "tool"` message). It is intentionally
// distinct from the internal/tools result type so the provider package stays
// free of any dependency on the tool-injection layer.
//
//revive:disable-next-line:exported // ToolResult mirrors the SPEC §7.3 vocabulary.
type ToolResult struct {
	// CallID is the provider call identifier the result threads back to — the
	// ToolCall.ID the model emitted (Anthropic `toolu_…`, OpenAI `call_…`).
	CallID string
	// Name is the tool name. OpenAI does not require it on the tool message,
	// but it is carried for symmetry and diagnostics.
	Name string
	// Content is the JSON-encoded tool output handed back to the model.
	Content json.RawMessage
	// IsError marks the result as a tool failure so adapters that distinguish
	// error results (Anthropic `is_error`) can flag it.
	IsError bool
}
