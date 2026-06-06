package router

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/audit"
	"github.com/conductor-sh/conductor/internal/config"
	"github.com/conductor-sh/conductor/internal/provider"
)

func pipelineConfig(maxTurns int, routing config.Routing, roles map[string]config.ProviderConfig) config.Config {
	cfg := config.Defaults()
	cfg.Agent.MaxTurns = maxTurns
	cfg.Routing = routing
	if roles != nil {
		cfg.Providers.Roles = roles
	}
	cfg.Validation.RunAfterTurn = false // off unless a test wires a validator
	return cfg
}

func newPipelineRouter(t *testing.T, pr Provider, tr Tracker, cfg config.Config, templates map[string]string, opts ...Option) (*Router, *captureSink) {
	t.Helper()
	w, sink := newCaptureWriter()
	base := []Option{
		WithProvider(pr),
		WithTracker(tr),
		WithAudit(w),
		WithConfig(func() config.Config { return cfg }),
		WithTemplates(func() map[string]string { return templates }),
		WithLogger(nopLogger()),
	}
	return New(append(base, opts...)...), sink
}

func runCtx(roles []string, advance func(int)) RunContext {
	return RunContext{
		Issue:         issue("i1", "A-1", "do the thing"),
		AttemptID:     "att-1",
		Attempt:       1,
		WorkspacePath: "/fake/A-1",
		Pipeline:      roles,
		AdvanceIndex:  advance,
	}
}

func TestRunPipeline_TwoRolesOutputHandoff(t *testing.T) {
	pr := &fakeProvider{results: []provider.TurnResult{
		{Text: "PLAN OUTPUT"},
		{Text: "CODE OUTPUT"},
	}}
	tr := &fakeTracker{states: map[string]string{"i1": "Done"}} // not active → no continuation
	cfg := pipelineConfig(1, config.Routing{Pipeline: []string{"planner", "coder"}}, nil)
	templates := map[string]string{
		"planner": "PLANNER: {{ issue.title }}",
		"coder":   "CODER: {{ issue.title }}",
	}

	var indices []int
	r, sink := newPipelineRouter(t, pr, tr, cfg, templates)

	out, err := r.RunPipeline(context.Background(), runCtx([]string{"planner", "coder"}, func(i int) {
		indices = append(indices, i)
	}))
	require.NoError(t, err)
	require.Equal(t, "CODE OUTPUT", out)

	turns := pr.recorded()
	require.Len(t, turns, 2)
	require.Contains(t, turns[0].prompt, "PLANNER:")
	require.Contains(t, turns[1].prompt, "CODER:")
	require.Contains(t, turns[1].prompt, "## Output from Previous Role (planner)")
	require.Contains(t, turns[1].prompt, "PLAN OUTPUT")

	require.Equal(t, []int{0, 1}, indices, "pipeline_index advanced per role")
	require.Equal(t, 2, sink.count(audit.EventPipelineRoleStarted))
	require.Equal(t, 2, sink.count(audit.EventPipelineRoleEnded))
}

func TestRunPipeline_PerRoleProviderResolution(t *testing.T) {
	// We cannot read the cfg off the opaque session, so assert resolution by
	// driving distinct behavior: the "planner" role has its own config and the
	// default applies to "coder". Resolution itself is unit-tested via the
	// resolveProviderConfig helper below; here we just confirm both roles run.
	pr := &fakeProvider{defaultResult: provider.TurnResult{Text: "ok"}}
	tr := &fakeTracker{states: map[string]string{"i1": "Done"}}
	roles := map[string]config.ProviderConfig{
		"planner": {Provider: "openai", Model: "gpt-x"},
	}
	cfg := pipelineConfig(1, config.Routing{Pipeline: []string{"planner", "coder"}}, roles)
	r, _ := newPipelineRouter(t, pr, tr, cfg, map[string]string{"planner": "P", "coder": "C"})

	_, err := r.RunPipeline(context.Background(), runCtx([]string{"planner", "coder"}, nil))
	require.NoError(t, err)
	require.Len(t, pr.recorded(), 2)
}

func TestResolveProviderConfig(t *testing.T) {
	cfg := config.Defaults()
	cfg.Providers.Default = config.ProviderConfig{Provider: "anthropic", Model: "default-model"}
	cfg.Providers.Roles = map[string]config.ProviderConfig{
		"planner": {Provider: "openai", Model: "gpt-x"},
		"empty":   {}, // no provider → fall back to default
	}
	require.Equal(t, "openai", resolveProviderConfig(cfg, "planner").Provider)
	require.Equal(t, "anthropic", resolveProviderConfig(cfg, "coder").Provider)
	require.Equal(t, "anthropic", resolveProviderConfig(cfg, "empty").Provider)
}

func TestRunPipeline_FailedRoleFailsAttempt(t *testing.T) {
	pr := &fakeProvider{results: []provider.TurnResult{
		{Err: errors.New("turn blew up")},
	}}
	tr := &fakeTracker{}
	cfg := pipelineConfig(1, config.Routing{Pipeline: []string{"coder"}}, nil)
	r, sink := newPipelineRouter(t, pr, tr, cfg, map[string]string{"coder": "C"})

	_, err := r.RunPipeline(context.Background(), runCtx([]string{"coder"}, nil))
	require.Error(t, err)
	require.Equal(t, 1, sink.count(audit.EventPipelineRoleStarted))
	require.Equal(t, 1, sink.count(audit.EventPipelineRoleEnded))
}

func TestRunPipeline_ValidationFailureFailsAttempt(t *testing.T) {
	pr := &fakeProvider{defaultResult: provider.TurnResult{Text: "ok"}}
	tr := &fakeTracker{}
	cfg := pipelineConfig(1, config.Routing{Pipeline: []string{"coder"}}, nil)
	cfg.Validation.RunAfterTurn = true
	val := &fakeValidator{err: errors.New("lint failed")}
	r, _ := newPipelineRouter(t, pr, tr, cfg, map[string]string{"coder": "C"}, WithValidator(val))

	_, err := r.RunPipeline(context.Background(), runCtx([]string{"coder"}, nil))
	require.ErrorIs(t, err, ErrValidationPipelineFailed)
	require.Equal(t, 1, val.calls)
}

func TestRunPipeline_ValidationSkippedWhenNoValidator(t *testing.T) {
	pr := &fakeProvider{defaultResult: provider.TurnResult{Text: "ok"}}
	tr := &fakeTracker{states: map[string]string{"i1": "Done"}}
	cfg := pipelineConfig(1, config.Routing{Pipeline: []string{"coder"}}, nil)
	cfg.Validation.RunAfterTurn = true // enabled, but no validator wired
	r, _ := newPipelineRouter(t, pr, tr, cfg, map[string]string{"coder": "C"})

	_, err := r.RunPipeline(context.Background(), runCtx([]string{"coder"}, nil))
	require.NoError(t, err, "validation step is nil-safe when Phase 8 is not wired")
}

func TestRunPipeline_ContinuationWhileActiveUnderCap(t *testing.T) {
	pr := &fakeProvider{defaultResult: provider.TurnResult{Text: "ok"}}
	tr := &fakeTracker{states: map[string]string{"i1": "In Progress"}} // active
	cfg := pipelineConfig(3, config.Routing{Pipeline: []string{"coder"}}, nil)
	r, _ := newPipelineRouter(t, pr, tr, cfg, map[string]string{"coder": "C"})

	_, err := r.RunPipeline(context.Background(), runCtx([]string{"coder"}, nil))
	require.NoError(t, err)

	turns := pr.recorded()
	require.Len(t, turns, 3, "ran up to max_turns iterations while active")
	require.False(t, turns[0].continued, "first turn uses StartTurn")
	require.True(t, turns[1].continued, "later turns use ContinueTurn (continuation)")
	require.True(t, turns[2].continued)
	// Continuation prompt is the built-in, not the original role template.
	require.NotContains(t, turns[1].prompt, "## Output from Previous Role")
	require.Contains(t, turns[1].prompt, "continuation turn")
}

func TestRunPipeline_ExitWhenInactive(t *testing.T) {
	pr := &fakeProvider{defaultResult: provider.TurnResult{Text: "ok"}}
	tr := &fakeTracker{states: map[string]string{"i1": "Done"}} // not active
	cfg := pipelineConfig(5, config.Routing{Pipeline: []string{"coder"}}, nil)
	r, _ := newPipelineRouter(t, pr, tr, cfg, map[string]string{"coder": "C"})

	_, err := r.RunPipeline(context.Background(), runCtx([]string{"coder"}, nil))
	require.NoError(t, err)
	require.Len(t, pr.recorded(), 1, "exited after first iteration; issue left active state")
	require.Equal(t, 1, tr.calls, "re-fetched issue state once")
}

func TestRunPipeline_ExitAtTurnCap(t *testing.T) {
	pr := &fakeProvider{defaultResult: provider.TurnResult{Text: "ok"}}
	tr := &fakeTracker{states: map[string]string{"i1": "In Progress"}} // always active
	cfg := pipelineConfig(2, config.Routing{Pipeline: []string{"coder"}}, nil)
	r, _ := newPipelineRouter(t, pr, tr, cfg, map[string]string{"coder": "C"})

	_, err := r.RunPipeline(context.Background(), runCtx([]string{"coder"}, nil))
	require.NoError(t, err)
	require.Len(t, pr.recorded(), 2, "stopped at max_turns even though still active")
}

func TestRunPipeline_RenderErrorReturnsSentinel(t *testing.T) {
	pr := &fakeProvider{defaultResult: provider.TurnResult{Text: "ok"}}
	tr := &fakeTracker{}
	cfg := pipelineConfig(1, config.Routing{Pipeline: []string{"coder"}}, nil)
	r, _ := newPipelineRouter(t, pr, tr, cfg, map[string]string{"coder": "{{ bogus_var }}"})

	_, err := r.RunPipeline(context.Background(), runCtx([]string{"coder"}, nil))
	require.ErrorIs(t, err, ErrPromptRenderFailed)
	require.Empty(t, pr.recorded(), "no turn runs when the prompt fails to render")
}
