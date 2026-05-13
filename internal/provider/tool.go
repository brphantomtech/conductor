package provider

import "encoding/json"

// ToolSpec describes a tool the provider should advertise to the agent on
// every turn. Phase 13 will populate the canonical Conductor tools
// (`conductor_tracker_query`, `conductor_knowledge_search`, …); Phase 3
// only carries the shape so adapter signatures are forward-compatible.
//
// Parameters holds the JSON Schema fragment the provider expects — the
// adapter forwards it verbatim through the provider's native tool-use
// payload (Anthropic `input_schema`, OpenAI `function.parameters`).
type ToolSpec struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}
