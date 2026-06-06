package orchestrator

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/audit"
	"github.com/conductor-sh/conductor/internal/config"
	"github.com/conductor-sh/conductor/internal/provider"
	"github.com/conductor-sh/conductor/internal/router"
)

// routerFakeProvider is a Provider that also satisfies router.Provider
// (ContinueTurn), recording every prompt so output hand-off can be asserted.
type routerFakeProvider struct {
	mu      sync.Mutex
	results []provider.TurnResult
	deflt   provider.TurnResult
	prompts []string
}

func (f *routerFakeProvider) next() provider.TurnResult {
	if len(f.results) == 0 {
		return f.deflt
	}
	r := f.results[0]
	f.results = f.results[1:]
	return r
}

func (f *routerFakeProvider) CreateSession(context.Context, config.ProviderConfig, string) (*provider.Session, error) {
	return &provider.Session{}, nil
}

func (f *routerFakeProvider) StartTurn(_ context.Context, _ *provider.Session, prompt string, _ []provider.ToolSpec) (provider.TurnStream, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prompts = append(f.prompts, prompt)
	return &routerFakeStream{result: f.next()}, nil
}

func (f *routerFakeProvider) ContinueTurn(_ context.Context, _ *provider.Session, prompt string) (provider.TurnStream, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prompts = append(f.prompts, prompt)
	return &routerFakeStream{result: f.next()}, nil
}

func (f *routerFakeProvider) EndSession(context.Context, *provider.Session) error { return nil }

func (f *routerFakeProvider) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.prompts...)
}

type routerFakeStream struct{ result provider.TurnResult }

func (s *routerFakeStream) Events() <-chan provider.AgentEvent {
	ch := make(chan provider.AgentEvent)
	close(ch)
	return ch
}
func (s *routerFakeStream) Wait() provider.TurnResult { return s.result }

// smokeConfig is a single-turn config (max_turns=1) with the given pipeline so
// the continuation loop does not iterate.
func smokeConfig(pipeline []string) config.Config {
	cfg := config.Defaults()
	cfg.Agent.MaxTurns = 1
	cfg.Routing = config.Routing{Pipeline: pipeline}
	cfg.Validation.RunAfterTurn = false
	return cfg
}

func TestSmoke_MultiRolePipelineThroughRealRouter(t *testing.T) {
	tr := &fakeTracker{statesByID: map[string]string{"i1": "Done"}}
	ws := &fakeWorkspaces{}
	pr := &routerFakeProvider{results: []provider.TurnResult{
		{Text: "PLAN"},      // planner
		{Text: "CODE DONE"}, // coder
	}}
	w, sink := newCaptureWriter()
	cfg := smokeConfig([]string{"planner", "coder"})

	agentRouter := router.New(
		router.WithProvider(pr),
		router.WithTracker(tr),
		router.WithConfig(func() config.Config { return cfg }),
		router.WithTemplates(func() map[string]string {
			return map[string]string{"planner": "PLANNER {{ issue.title }}", "coder": "CODER {{ issue.title }}"}
		}),
		router.WithAudit(w),
		router.WithLogger(nopLogger()),
	)

	clk := newFakeClock(time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC))
	o := New(
		WithTracker(tr),
		WithWorkspaces(ws),
		WithProvider(pr, config.ProviderConfig{Provider: "anthropic"}),
		WithAudit(w),
		WithConfig(func() config.Config { return cfg }),
		WithTemplates(func() map[string]string { return map[string]string{} }),
		WithClassifier(agentRouter),
		WithPipelineRouter(agentRouter),
		WithClock(clk.Now),
		WithLogger(nopLogger()),
	)

	iss := issue("i1", "A-1", "Todo")
	iss.Title = "build feature"
	require.True(t, o.dispatch(iss))
	o.Wait()

	prompts := pr.recorded()
	require.Len(t, prompts, 2, "two roles executed")
	require.Contains(t, prompts[0], "PLANNER build feature")
	require.Contains(t, prompts[1], "CODER build feature")
	require.Contains(t, prompts[1], "## Output from Previous Role (planner)")
	require.Contains(t, prompts[1], "PLAN")

	require.Equal(t, 2, sink.count(audit.EventPipelineRoleStarted))
	require.Equal(t, 2, sink.count(audit.EventPipelineRoleEnded))
	require.Equal(t, 1, sink.count(audit.EventIssueReleased))
	require.Empty(t, o.RuntimeState().RetryQueued)
}

func TestSmoke_SingleRoleReproducesPhase6Path(t *testing.T) {
	tr := &fakeTracker{statesByID: map[string]string{"i1": "Done"}}
	ws := &fakeWorkspaces{}
	pr := &routerFakeProvider{deflt: provider.TurnResult{Text: "done"}}
	w, sink := newCaptureWriter()
	cfg := smokeConfig([]string{"coder"})

	agentRouter := router.New(
		router.WithProvider(pr),
		router.WithTracker(tr),
		router.WithConfig(func() config.Config { return cfg }),
		router.WithTemplates(func() map[string]string { return map[string]string{"coder": "CODER"} }),
		router.WithAudit(w),
		router.WithLogger(nopLogger()),
	)

	clk := newFakeClock(time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC))
	o := New(
		WithTracker(tr),
		WithWorkspaces(ws),
		WithProvider(pr, config.ProviderConfig{Provider: "anthropic"}),
		WithAudit(w),
		WithConfig(func() config.Config { return cfg }),
		WithTemplates(func() map[string]string { return map[string]string{} }),
		WithClassifier(agentRouter),
		WithPipelineRouter(agentRouter),
		WithClock(clk.Now),
		WithLogger(nopLogger()),
	)

	require.True(t, o.dispatch(issue("i1", "A-1", "Todo")))
	o.Wait()

	require.Len(t, pr.recorded(), 1, "exactly one coder turn")
	require.Equal(t, 1, sink.count(audit.EventIssueReleased))
	require.Empty(t, o.RuntimeState().RetryQueued)
}
