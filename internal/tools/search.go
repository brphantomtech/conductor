package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// Tool names for the search tools (SPEC §7.3).
const (
	ToolKnowledgeSearch = "conductor_knowledge_search"
	ToolDocSearch       = "conductor_doc_search"
)

// knowledgeSearchTool implements conductor_knowledge_search over a
// KnowledgeSearcher (Knowledge Engine hybrid search, SPEC §8.4).
type knowledgeSearchTool struct{ eng KnowledgeSearcher }

// NewKnowledgeSearchTool constructs the conductor_knowledge_search tool.
func NewKnowledgeSearchTool(eng KnowledgeSearcher) Tool { return &knowledgeSearchTool{eng: eng} }

func (t *knowledgeSearchTool) Name() string { return ToolKnowledgeSearch }

func (t *knowledgeSearchTool) Description() string {
	return "Semantic + structural search over the codebase knowledge graph."
}

func (t *knowledgeSearchTool) ParametersSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "Natural-language search seed."},
    "types": {"type": "array", "items": {"type": "string"},
      "description": "Restrict to node types (file, symbol, module, doc, ...)."},
    "path_pattern": {"type": "string", "description": "Glob restricting results by path."},
    "top_k": {"type": "integer", "description": "Maximum number of results."}
  },
  "required": ["query"]
}`)
}

func (t *knowledgeSearchTool) Execute(
	ctx context.Context, params map[string]any, _ ExecutionContext,
) (ToolResult, error) {
	if t.eng == nil {
		return ToolResult{}, fmt.Errorf("tools: %s: %w", t.Name(), ErrEngineUnavailable)
	}
	query, err := requiredString(params, "query")
	if err != nil {
		return ToolResult{}, err
	}
	results, err := t.eng.SearchKnowledge(ctx, KnowledgeQuery{
		Query:       query,
		Types:       stringSliceParam(params, "types"),
		PathPattern: func() string { s, _ := stringParam(params, "path_pattern"); return s }(),
		TopK:        intParam(params, "top_k", 0),
	})
	if err != nil {
		return ToolResult{}, fmt.Errorf("tools: %s: %w", t.Name(), err)
	}
	return ToolResult{Content: map[string]any{
		"results": results,
		"count":   len(results),
	}}, nil
}

// docSearchTool implements conductor_doc_search over a DocSearcher (semantic
// search over Doc Store documents indexed as doc nodes, SPEC §10).
type docSearchTool struct{ eng DocSearcher }

// NewDocSearchTool constructs the conductor_doc_search tool.
func NewDocSearchTool(eng DocSearcher) Tool { return &docSearchTool{eng: eng} }

func (t *docSearchTool) Name() string { return ToolDocSearch }

func (t *docSearchTool) Description() string {
	return "Semantic search over the Doc Store (synced documentation)."
}

func (t *docSearchTool) ParametersSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "Natural-language search seed."},
    "top_k": {"type": "integer", "description": "Maximum number of results."}
  },
  "required": ["query"]
}`)
}

func (t *docSearchTool) Execute(ctx context.Context, params map[string]any, _ ExecutionContext) (ToolResult, error) {
	if t.eng == nil {
		return ToolResult{}, fmt.Errorf("tools: %s: %w", t.Name(), ErrEngineUnavailable)
	}
	query, err := requiredString(params, "query")
	if err != nil {
		return ToolResult{}, err
	}
	results, err := t.eng.SearchDocs(ctx, query, intParam(params, "top_k", 0))
	if err != nil {
		return ToolResult{}, fmt.Errorf("tools: %s: %w", t.Name(), err)
	}
	return ToolResult{Content: map[string]any{
		"results": results,
		"count":   len(results),
	}}, nil
}
