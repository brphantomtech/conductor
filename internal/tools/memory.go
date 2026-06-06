package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// Tool names for the memory tools (SPEC §7.3).
const (
	ToolMemoryRead  = "conductor_memory_read"
	ToolMemoryWrite = "conductor_memory_write"
)

// memoryReadTool implements conductor_memory_read over a MemoryStore. Project
// and issue scoping come from the ExecutionContext (Conductor-injected), not
// from agent params.
type memoryReadTool struct{ store MemoryStore }

// NewMemoryReadTool constructs the conductor_memory_read tool.
func NewMemoryReadTool(store MemoryStore) Tool { return &memoryReadTool{store: store} }

func (t *memoryReadTool) Name() string { return ToolMemoryRead }

func (t *memoryReadTool) Description() string {
	return "Retrieve relevant memories (episodic, semantic, procedural) for a query."
}

func (t *memoryReadTool) ParametersSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "Intent / seed text to retrieve memories for."},
    "task_type": {"type": "string", "description": "Optional task type scoping procedural memories."}
  },
  "required": ["query"]
}`)
}

func (t *memoryReadTool) Execute(ctx context.Context, params map[string]any, ec ExecutionContext) (ToolResult, error) {
	if t.store == nil {
		return ToolResult{}, fmt.Errorf("tools: %s: %w", t.Name(), ErrEngineUnavailable)
	}
	query, err := requiredString(params, "query")
	if err != nil {
		return ToolResult{}, err
	}
	taskType, _ := stringParam(params, "task_type")
	results, err := t.store.ReadMemory(ctx, MemoryReadQuery{
		ProjectID: ec.ProjectID,
		IssueID:   ec.IssueID,
		TaskType:  taskType,
		Intent:    query,
	})
	if err != nil {
		return ToolResult{}, fmt.Errorf("tools: %s: %w", t.Name(), err)
	}
	return ToolResult{Content: map[string]any{
		"memories": results,
		"count":    len(results),
	}}, nil
}

// memoryWriteTool implements conductor_memory_write over a MemoryStore. The
// layer and content are agent-supplied; project/issue scoping is injected.
type memoryWriteTool struct{ store MemoryStore }

// NewMemoryWriteTool constructs the conductor_memory_write tool.
func NewMemoryWriteTool(store MemoryStore) Tool { return &memoryWriteTool{store: store} }

func (t *memoryWriteTool) Name() string { return ToolMemoryWrite }

func (t *memoryWriteTool) Description() string {
	return "Persist a new memory entry (episodic or semantic) for future retrieval."
}

func (t *memoryWriteTool) ParametersSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "layer": {"type": "string", "enum": ["episodic", "semantic", "procedural"], "description": "Memory layer."},
    "content": {"type": "string", "description": "The memory content to persist."},
    "task_type": {"type": "string", "description": "Task type for procedural memories."},
    "tags": {"type": "array", "items": {"type": "string"}, "description": "Optional labels."}
  },
  "required": ["layer", "content"]
}`)
}

func (t *memoryWriteTool) Execute(ctx context.Context, params map[string]any, ec ExecutionContext) (ToolResult, error) {
	if t.store == nil {
		return ToolResult{}, fmt.Errorf("tools: %s: %w", t.Name(), ErrEngineUnavailable)
	}
	layer, err := requiredString(params, "layer")
	if err != nil {
		return ToolResult{}, err
	}
	content, err := requiredString(params, "content")
	if err != nil {
		return ToolResult{}, err
	}
	taskType, _ := stringParam(params, "task_type")
	id, err := t.store.WriteMemory(ctx, MemoryWriteEntry{
		Layer:     layer,
		ProjectID: ec.ProjectID,
		IssueID:   ec.IssueID,
		TaskType:  taskType,
		Content:   content,
		Tags:      stringSliceParam(params, "tags"),
	})
	if err != nil {
		return ToolResult{}, fmt.Errorf("tools: %s: %w", t.Name(), err)
	}
	return ToolResult{Content: map[string]any{
		"memory_id": id,
		"written":   true,
	}}, nil
}
