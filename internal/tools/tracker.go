package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// Tool names for the tracker tools (SPEC §7.3).
const (
	ToolTrackerQuery  = "conductor_tracker_query"
	ToolTrackerMutate = "conductor_tracker_mutate"
)

// trackerQueryTool implements conductor_tracker_query over a TrackerExecutor.
// Auth is injected by Conductor at the adapter layer; the agent supplies only
// the query and variables (SPEC §21.1).
type trackerQueryTool struct{ exec TrackerExecutor }

// NewTrackerQueryTool constructs the conductor_tracker_query tool.
func NewTrackerQueryTool(exec TrackerExecutor) Tool { return &trackerQueryTool{exec: exec} }

func (t *trackerQueryTool) Name() string { return ToolTrackerQuery }

func (t *trackerQueryTool) Description() string {
	return "Execute a read-only query against the configured issue tracker. " +
		"Auth is injected by Conductor; you never see the token."
}

func (t *trackerQueryTool) ParametersSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "GraphQL document (Linear) or URL path (GitHub)."},
    "variables": {"type": "object", "description": "GraphQL variables; ignored for REST trackers."}
  },
  "required": ["query"]
}`)
}

func (t *trackerQueryTool) Execute(ctx context.Context, params map[string]any, _ ExecutionContext) (ToolResult, error) {
	if t.exec == nil {
		return ToolResult{}, fmt.Errorf("tools: %s: %w", t.Name(), ErrEngineUnavailable)
	}
	query, err := requiredString(params, "query")
	if err != nil {
		return ToolResult{}, err
	}
	out, err := t.exec.ExecuteQuery(ctx, query, mapParam(params, "variables"))
	if err != nil {
		return ToolResult{}, fmt.Errorf("tools: %s: %w", t.Name(), err)
	}
	return ToolResult{Content: map[string]any{"result": out}}, nil
}

// trackerMutateTool implements conductor_tracker_mutate over a TrackerExecutor.
// It is classified destructive for approval gating (SPEC §21.2).
type trackerMutateTool struct{ exec TrackerExecutor }

// NewTrackerMutateTool constructs the conductor_tracker_mutate tool.
func NewTrackerMutateTool(exec TrackerExecutor) Tool { return &trackerMutateTool{exec: exec} }

func (t *trackerMutateTool) Name() string { return ToolTrackerMutate }

func (t *trackerMutateTool) Description() string {
	return "Execute a write mutation against the tracker (update state, add comment, etc.). " +
		"Requires approval under review_destructive / manual policies."
}

func (t *trackerMutateTool) ParametersSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "mutation": {"type": "string", "description": "GraphQL mutation (Linear) or URL path (GitHub)."},
    "variables": {"type": "object", "description": "Mutation variables / request body."}
  },
  "required": ["mutation"]
}`)
}

func (t *trackerMutateTool) Execute(
	ctx context.Context, params map[string]any, _ ExecutionContext,
) (ToolResult, error) {
	if t.exec == nil {
		return ToolResult{}, fmt.Errorf("tools: %s: %w", t.Name(), ErrEngineUnavailable)
	}
	mutation, err := requiredString(params, "mutation")
	if err != nil {
		return ToolResult{}, err
	}
	out, err := t.exec.ExecuteMutation(ctx, mutation, mapParam(params, "variables"))
	if err != nil {
		return ToolResult{}, fmt.Errorf("tools: %s: %w", t.Name(), err)
	}
	return ToolResult{Content: map[string]any{"result": out}}, nil
}
