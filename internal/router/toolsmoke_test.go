package router

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/audit"
	"github.com/conductor-sh/conductor/internal/config"
	"github.com/conductor-sh/conductor/internal/provider"
	"github.com/conductor-sh/conductor/internal/tools"
)

// smokeKnowledge is a fake KnowledgeSearcher returning one hit.
type smokeKnowledge struct{ called bool }

func (s *smokeKnowledge) SearchKnowledge(_ context.Context, _ tools.KnowledgeQuery) ([]tools.KnowledgeResult, error) {
	s.called = true
	return []tools.KnowledgeResult{{Path: "x.go", Summary: "found"}}, nil
}

// smokeTracker is a fake TrackerExecutor whose mutate must never be reached.
type smokeTracker struct{ mutated bool }

func (s *smokeTracker) ExecuteQuery(context.Context, string, map[string]any) (map[string]any, error) {
	return map[string]any{}, nil
}
func (s *smokeTracker) ExecuteMutation(context.Context, string, map[string]any) (map[string]any, error) {
	s.mutated = true
	return map[string]any{}, nil
}

// TestSmoke_DispatchLoopWithRealDispatcher exercises the full Phase 13 path: a
// fake model turn calls conductor_knowledge_search (allowed) then a denied
// conductor_tracker_mutate under review_destructive. The turn runs through the
// real dispatcher + registry, continues with the results, and records
// ToolCalled / ToolResult audit events (SPEC §7.3, §21.2). No live API calls.
func TestSmoke_DispatchLoopWithRealDispatcher(t *testing.T) {
	know := &smokeKnowledge{}
	trk := &smokeTracker{}

	registry := tools.NewRegistry()
	registry.RegisterBuiltins(tools.Engines{Knowledge: know, Tracker: trk})

	sink := &captureSink{}
	writer := audit.NewWriter(nopLogger())
	writer.AddSink(sink)
	dispatcher := tools.NewDispatcher(registry, tools.WithAudit(writer))

	// Turn 1 calls knowledge_search; the continuation turn calls tracker_mutate;
	// the final turn returns text with no tool calls.
	pr := &fakeProvider{results: []provider.TurnResult{
		{ToolCalls: []provider.ToolCall{toolCall("c1", tools.ToolKnowledgeSearch, `{"query":"auth"}`)}},
		{ToolCalls: []provider.ToolCall{toolCall("c2", tools.ToolTrackerMutate, `{"mutation":"m"}`)}},
		{Text: "DONE"},
	}}
	tr := &fakeTracker{states: map[string]string{"i1": "Done"}}

	// review_destructive: knowledge allowed, tracker_mutate denied.
	roles := map[string]config.ProviderConfig{
		"coder": {Provider: "openai", Model: "x", ApprovalPolicy: string(tools.PolicyReviewDestructive)},
	}
	cfg := pipelineConfig(1, config.Routing{Pipeline: []string{"coder"}}, roles)

	w2, _ := newCaptureWriter()
	r := New(
		WithProvider(pr),
		WithTracker(tr),
		WithAudit(w2),
		WithConfig(func() config.Config { return cfg }),
		WithTemplates(func() map[string]string { return map[string]string{"coder": "C"} }),
		WithTools(registry, dispatcher),
		WithLogger(nopLogger()),
	)

	out, err := r.RunPipeline(context.Background(), runCtx([]string{"coder"}, nil))
	require.NoError(t, err)
	require.Equal(t, "DONE", out)

	require.True(t, know.called, "knowledge_search reached the engine")
	require.False(t, trk.mutated, "tracker_mutate was denied and never reached the engine")

	// Two tool calls → two ToolCalled and two ToolResult audit events.
	require.Equal(t, 2, sink.count(audit.EventToolCalled))
	require.Equal(t, 2, sink.count(audit.EventToolResult))

	// The denied mutate produced an approval-required error result fed back.
	cont := pr.recordedToolResults()
	require.Len(t, cont, 2)
	var denied map[string]any
	require.NoError(t, json.Unmarshal(cont[1].results[0].Content, &denied))
	require.Equal(t, true, denied["approval_required"])
	require.True(t, cont[1].results[0].IsError)
}
