package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTrackerQueryTool_Dispatches(t *testing.T) {
	fk := &fakeTracker{queryOut: map[string]any{"data": "x"}}
	tool := NewTrackerQueryTool(fk)
	require.Equal(t, ToolTrackerQuery, tool.Name())
	require.NotEmpty(t, tool.Description())

	res, err := tool.Execute(context.Background(),
		map[string]any{"query": "{ viewer { id } }", "variables": map[string]any{"k": "v"}},
		ExecutionContext{})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Equal(t, "{ viewer { id } }", fk.queryArgs)
	require.Equal(t, map[string]any{"k": "v"}, fk.queryVars)
	require.Equal(t, map[string]any{"data": "x"}, res.Content["result"])
}

func TestTrackerQueryTool_MissingQueryErrors(t *testing.T) {
	tool := NewTrackerQueryTool(&fakeTracker{})
	_, err := tool.Execute(context.Background(), map[string]any{}, ExecutionContext{})
	require.ErrorIs(t, err, ErrInvalidParams)
}

func TestTrackerQueryTool_NilEngine(t *testing.T) {
	tool := NewTrackerQueryTool(nil)
	_, err := tool.Execute(context.Background(), map[string]any{"query": "q"}, ExecutionContext{})
	require.ErrorIs(t, err, ErrEngineUnavailable)
}

func TestTrackerMutateTool_Dispatches(t *testing.T) {
	fk := &fakeTracker{}
	tool := NewTrackerMutateTool(fk)
	res, err := tool.Execute(context.Background(),
		map[string]any{"mutation": "mutation { close }"}, ExecutionContext{})
	require.NoError(t, err)
	require.Equal(t, "mutation { close }", fk.mutationArgs)
	require.Equal(t, true, res.Content["result"].(map[string]any)["updated"])
	require.True(t, IsDestructive(ToolTrackerMutate))
	require.False(t, IsDestructive(ToolTrackerQuery))
}

func TestTrackerTool_EngineErrorPropagates(t *testing.T) {
	tool := NewTrackerQueryTool(&fakeTracker{err: errEngine})
	_, err := tool.Execute(context.Background(), map[string]any{"query": "q"}, ExecutionContext{})
	require.ErrorIs(t, err, errEngine)
}

func TestKnowledgeSearchTool_Dispatches(t *testing.T) {
	fk := &fakeKnowledge{results: []KnowledgeResult{{Path: "a.go", Name: "A", Score: 0.9}}}
	tool := NewKnowledgeSearchTool(fk)
	res, err := tool.Execute(context.Background(),
		map[string]any{"query": "auth", "types": []any{"symbol"}, "top_k": float64(5)},
		ExecutionContext{})
	require.NoError(t, err)
	require.Equal(t, "auth", fk.gotQuery.Query)
	require.Equal(t, []string{"symbol"}, fk.gotQuery.Types)
	require.Equal(t, 5, fk.gotQuery.TopK)
	require.Equal(t, 1, res.Content["count"])
}

func TestDocSearchTool_Dispatches(t *testing.T) {
	fk := &fakeDocs{results: []DocResult{{Path: "docs/x", Title: "X"}}}
	tool := NewDocSearchTool(fk)
	res, err := tool.Execute(context.Background(),
		map[string]any{"query": "deploy", "top_k": float64(3)}, ExecutionContext{})
	require.NoError(t, err)
	require.Equal(t, "deploy", fk.gotQuery)
	require.Equal(t, 3, fk.gotTopK)
	require.Equal(t, 1, res.Content["count"])
}

func TestMemoryReadTool_UsesExecutionContextScope(t *testing.T) {
	fk := &fakeMemory{readOut: []MemoryResult{{ID: "m1", Content: "remember"}}}
	tool := NewMemoryReadTool(fk)
	res, err := tool.Execute(context.Background(),
		map[string]any{"query": "intent", "task_type": "bug"},
		ExecutionContext{ProjectID: "proj", IssueID: "iss"})
	require.NoError(t, err)
	require.Equal(t, "proj", fk.gotRead.ProjectID)
	require.Equal(t, "iss", fk.gotRead.IssueID)
	require.Equal(t, "bug", fk.gotRead.TaskType)
	require.Equal(t, "intent", fk.gotRead.Intent)
	require.Equal(t, 1, res.Content["count"])
}

func TestMemoryWriteTool_Persists(t *testing.T) {
	fk := &fakeMemory{writeID: "mem-42"}
	tool := NewMemoryWriteTool(fk)
	res, err := tool.Execute(context.Background(),
		map[string]any{"layer": "semantic", "content": "fact", "tags": []any{"t1"}},
		ExecutionContext{ProjectID: "proj"})
	require.NoError(t, err)
	require.Equal(t, "semantic", fk.gotWrite.Layer)
	require.Equal(t, "proj", fk.gotWrite.ProjectID)
	require.Equal(t, []string{"t1"}, fk.gotWrite.Tags)
	require.Equal(t, "mem-42", res.Content["memory_id"])
	require.Equal(t, true, res.Content["written"])
}

func TestMemoryWriteTool_RequiresLayerAndContent(t *testing.T) {
	tool := NewMemoryWriteTool(&fakeMemory{})
	_, err := tool.Execute(context.Background(), map[string]any{"content": "x"}, ExecutionContext{})
	require.ErrorIs(t, err, ErrInvalidParams)
	_, err = tool.Execute(context.Background(), map[string]any{"layer": "semantic"}, ExecutionContext{})
	require.ErrorIs(t, err, ErrInvalidParams)
}

func TestHarnessCheckTool_Dispatches(t *testing.T) {
	fk := &fakeHarness{violations: []HarnessViolation{{RuleID: "r1", Severity: "error"}}}
	tool := NewHarnessCheckTool(fk)
	res, err := tool.Execute(context.Background(),
		map[string]any{"rule_id": "r1"}, ExecutionContext{})
	require.NoError(t, err)
	require.Equal(t, "r1", fk.gotRuleID)
	require.Equal(t, 1, res.Content["count"])
	require.Equal(t, false, res.Content["clear"])
}

func TestValidationRunTool_UsesWorkspaceFromContext(t *testing.T) {
	fk := &fakeValidation{outcome: ValidationOutcome{Passed: true, Checks: []ValidationCheckResult{{CheckID: "c1", Status: "pass"}}}}
	tool := NewValidationRunTool(fk)
	res, err := tool.Execute(context.Background(), map[string]any{},
		ExecutionContext{WorkspacePath: "/ws/iss-1"})
	require.NoError(t, err)
	require.Equal(t, "/ws/iss-1", fk.gotWorkspace)
	require.Equal(t, true, res.Content["passed"])
}

func TestAllBuiltinSchemasValidJSON(t *testing.T) {
	r := NewRegistry()
	r.RegisterBuiltins(Engines{})
	require.Equal(t, 8, r.Len())
	for _, name := range r.Names() {
		tool, _ := r.Lookup(name)
		var schema map[string]any
		require.NoErrorf(t, json.Unmarshal(tool.ParametersSchema(), &schema),
			"tool %s has invalid schema", name)
		require.Equal(t, "object", schema["type"], "tool %s schema type", name)
	}
}

func TestBuiltinsRegistration_CompleteSet(t *testing.T) {
	r := NewRegistry()
	r.RegisterBuiltins(Engines{})
	want := []string{
		ToolDocSearch, ToolHarnessCheck, ToolKnowledgeSearch,
		ToolMemoryRead, ToolMemoryWrite, ToolTrackerMutate,
		ToolTrackerQuery, ToolValidationRun,
	}
	require.ElementsMatch(t, want, r.Names())
}
