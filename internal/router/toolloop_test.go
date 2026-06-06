package router

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/config"
	"github.com/conductor-sh/conductor/internal/provider"
	"github.com/conductor-sh/conductor/internal/tools"
)

// fakeRegistry advertises a fixed spec set.
type fakeRegistry struct{ specs []provider.ToolSpec }

func (f fakeRegistry) Specs() []provider.ToolSpec { return f.specs }

// fakeDispatcher records dispatched calls and returns scripted results.
type fakeDispatcher struct {
	calls  []tools.Call
	result tools.ToolResult
}

func (f *fakeDispatcher) Dispatch(
	_ context.Context, call tools.Call, _ tools.ApprovalPolicy, _ tools.ExecutionContext,
) tools.ToolResult {
	f.calls = append(f.calls, call)
	if f.result.Content == nil {
		return tools.ToolResult{Content: map[string]any{"ok": true}}
	}
	return f.result
}

func toolCall(id, name, args string) provider.ToolCall {
	return provider.ToolCall{ID: id, Name: name, Arguments: json.RawMessage(args)}
}

func TestToolLoop_DispatchesAndContinues(t *testing.T) {
	// First turn emits a tool call; the continuation turn returns final text.
	pr := &fakeProvider{results: []provider.TurnResult{
		{Text: "thinking", ToolCalls: []provider.ToolCall{toolCall("c1", "conductor_knowledge_search", `{"query":"x"}`)}},
		{Text: "FINAL ANSWER"},
	}}
	tr := &fakeTracker{states: map[string]string{"i1": "Done"}}
	disp := &fakeDispatcher{}
	reg := fakeRegistry{specs: []provider.ToolSpec{{Name: "conductor_knowledge_search"}}}

	cfg := pipelineConfig(1, config.Routing{Pipeline: []string{"coder"}}, nil)
	r, _ := newPipelineRouter(t, pr, tr, cfg, map[string]string{"coder": "C"},
		WithTools(reg, disp))

	out, err := r.RunPipeline(context.Background(), runCtx([]string{"coder"}, nil))
	require.NoError(t, err)
	require.Equal(t, "FINAL ANSWER", out)

	require.Len(t, disp.calls, 1)
	require.Equal(t, "conductor_knowledge_search", disp.calls[0].Name)

	cont := pr.recordedToolResults()
	require.Len(t, cont, 1)
	require.Len(t, cont[0].results, 1)
	require.Equal(t, "c1", cont[0].results[0].CallID)
}

func TestToolLoop_AdvertisesSpecsOnStartTurn(t *testing.T) {
	pr := &fakeProvider{defaultResult: provider.TurnResult{Text: "done"}}
	tr := &fakeTracker{states: map[string]string{"i1": "Done"}}
	disp := &fakeDispatcher{}
	reg := fakeRegistry{specs: []provider.ToolSpec{{Name: "conductor_doc_search"}}}

	cfg := pipelineConfig(1, config.Routing{Pipeline: []string{"coder"}}, nil)
	r, _ := newPipelineRouter(t, pr, tr, cfg, map[string]string{"coder": "C"},
		WithTools(reg, disp))

	_, err := r.RunPipeline(context.Background(), runCtx([]string{"coder"}, nil))
	require.NoError(t, err)
	// No tool call emitted → no dispatch, no continuation.
	require.Empty(t, disp.calls)
	require.Empty(t, pr.recordedToolResults())
}

func TestToolLoop_ToolErrorDoesNotAbort(t *testing.T) {
	pr := &fakeProvider{results: []provider.TurnResult{
		{ToolCalls: []provider.ToolCall{toolCall("c1", "conductor_tracker_mutate", `{}`)}},
		{Text: "RECOVERED"},
	}}
	tr := &fakeTracker{states: map[string]string{"i1": "Done"}}
	// Dispatcher returns an error result (e.g. denial / engine fault).
	disp := &fakeDispatcher{result: tools.ToolResult{Content: map[string]any{"error": "boom"}, IsError: true}}
	reg := fakeRegistry{specs: []provider.ToolSpec{{Name: "conductor_tracker_mutate"}}}

	cfg := pipelineConfig(1, config.Routing{Pipeline: []string{"coder"}}, nil)
	r, _ := newPipelineRouter(t, pr, tr, cfg, map[string]string{"coder": "C"}, WithTools(reg, disp))

	out, err := r.RunPipeline(context.Background(), runCtx([]string{"coder"}, nil))
	require.NoError(t, err) // not aborted
	require.Equal(t, "RECOVERED", out)

	cont := pr.recordedToolResults()
	require.Len(t, cont, 1)
	require.True(t, cont[0].results[0].IsError)
}

func TestToolLoop_BoundedByCap(t *testing.T) {
	// The model always calls a tool; the loop must terminate at the cap.
	pr := &fakeProvider{defaultResult: provider.TurnResult{
		Text:      "loop",
		ToolCalls: []provider.ToolCall{toolCall("c1", "conductor_knowledge_search", `{}`)},
	}}
	tr := &fakeTracker{states: map[string]string{"i1": "Done"}}
	disp := &fakeDispatcher{}
	reg := fakeRegistry{specs: []provider.ToolSpec{{Name: "conductor_knowledge_search"}}}

	cfg := pipelineConfig(1, config.Routing{Pipeline: []string{"coder"}}, nil)
	r, _ := newPipelineRouter(t, pr, tr, cfg, map[string]string{"coder": "C"},
		WithTools(reg, disp), WithMaxToolTurns(3))

	out, err := r.RunPipeline(context.Background(), runCtx([]string{"coder"}, nil))
	require.NoError(t, err)
	require.Equal(t, "loop", out)
	// Cap of 3 loop iterations: 3 dispatches and 3 continuation turns, then stop.
	require.Len(t, pr.recordedToolResults(), 3)
	require.Len(t, disp.calls, 3)
}

func TestToolLoop_NoToolsWiredBehavesAsBefore(t *testing.T) {
	pr := &fakeProvider{defaultResult: provider.TurnResult{
		Text:      "ANSWER",
		ToolCalls: []provider.ToolCall{toolCall("c1", "conductor_knowledge_search", `{}`)},
	}}
	tr := &fakeTracker{states: map[string]string{"i1": "Done"}}
	cfg := pipelineConfig(1, config.Routing{Pipeline: []string{"coder"}}, nil)
	// No WithTools → loop is skipped even though the result has a tool call.
	r, _ := newPipelineRouter(t, pr, tr, cfg, map[string]string{"coder": "C"})

	out, err := r.RunPipeline(context.Background(), runCtx([]string{"coder"}, nil))
	require.NoError(t, err)
	require.Equal(t, "ANSWER", out)
	require.Empty(t, pr.recordedToolResults())
}
