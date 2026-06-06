package router

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/config"
	"github.com/conductor-sh/conductor/internal/tracker"
)

func routerWithRouting(routing config.Routing) *Router {
	return New(WithConfig(func() config.Config {
		cfg := config.Defaults()
		cfg.Routing = routing
		return cfg
	}))
}

func TestSelectPipeline_FirstMatchWins(t *testing.T) {
	routing := config.Routing{
		Pipeline: []string{"coder"},
		Rules: []config.RoutingRule{
			{When: config.RoutingMatch{TaskType: "bug"}, Pipeline: []string{"debugger", "coder"}},
			{When: config.RoutingMatch{State: "Todo"}, Pipeline: []string{"planner", "coder"}},
		},
	}
	r := routerWithRouting(routing)

	iss := issue("i1", "A-1", "x")
	iss.TaskType = strp("bug") // matches both rules; first wins.

	require.Equal(t, []string{"debugger", "coder"}, r.SelectPipeline(iss))
}

func TestSelectPipeline_FallbackToDefault(t *testing.T) {
	routing := config.Routing{
		Pipeline: []string{"planner", "coder", "verifier"},
		Rules: []config.RoutingRule{
			{When: config.RoutingMatch{TaskType: "docs"}, Pipeline: []string{"writer"}},
		},
	}
	r := routerWithRouting(routing)

	iss := issue("i1", "A-1", "x")
	iss.TaskType = strp("bug")

	require.Equal(t, []string{"planner", "coder", "verifier"}, r.SelectPipeline(iss))
}

func TestSelectPipeline_MultiConditionConjunction(t *testing.T) {
	routing := config.Routing{
		Pipeline: []string{"coder"},
		Rules: []config.RoutingRule{
			{
				When:     config.RoutingMatch{TaskType: "feature", State: "Todo"},
				Pipeline: []string{"planner", "coder", "verifier"},
			},
		},
	}
	r := routerWithRouting(routing)

	match := issue("i1", "A-1", "x")
	match.TaskType = strp("feature")
	match.State = "Todo"
	require.Equal(t, []string{"planner", "coder", "verifier"}, r.SelectPipeline(match))

	// One condition off → no match → fallback.
	noMatch := issue("i2", "A-2", "x")
	noMatch.TaskType = strp("feature")
	noMatch.State = "In Progress"
	require.Equal(t, []string{"coder"}, r.SelectPipeline(noMatch))
}

func TestSelectPipeline_LabelsVsAnyLabel(t *testing.T) {
	routing := config.Routing{
		Pipeline: []string{"coder"},
		Rules: []config.RoutingRule{
			{When: config.RoutingMatch{Labels: []string{"backend", "urgent"}}, Pipeline: []string{"all-labels"}},
			{When: config.RoutingMatch{AnyLabel: []string{"frontend", "design"}}, Pipeline: []string{"any-label"}},
		},
	}
	r := routerWithRouting(routing)

	// Has both required labels → first rule.
	both := issue("i1", "A-1", "x")
	both.Labels = []string{"backend", "urgent", "extra"}
	require.Equal(t, []string{"all-labels"}, r.SelectPipeline(both))

	// Missing one of the required labels, but has one any_label → second rule.
	partial := issue("i2", "A-2", "x")
	partial.Labels = []string{"backend", "frontend"}
	require.Equal(t, []string{"any-label"}, r.SelectPipeline(partial))

	// Neither → fallback.
	none := issue("i3", "A-3", "x")
	none.Labels = []string{"misc"}
	require.Equal(t, []string{"coder"}, r.SelectPipeline(none))
}

func TestSelectPipeline_TitleRegex(t *testing.T) {
	routing := config.Routing{
		Pipeline: []string{"coder"},
		Rules: []config.RoutingRule{
			{When: config.RoutingMatch{TitleMatches: `(?i)^spike:`}, Pipeline: []string{"investigator"}},
		},
	}
	r := routerWithRouting(routing)

	require.Equal(t, []string{"investigator"}, r.SelectPipeline(issue("i1", "A-1", "Spike: explore caching")))
	require.Equal(t, []string{"coder"}, r.SelectPipeline(issue("i2", "A-2", "Implement caching")))
}

func TestSelectPipeline_Complexity(t *testing.T) {
	routing := config.Routing{
		Pipeline: []string{"coder"},
		Rules: []config.RoutingRule{
			{When: config.RoutingMatch{Complexity: "high"}, Pipeline: []string{"planner", "coder", "verifier", "reviewer"}},
		},
	}
	r := routerWithRouting(routing)

	hi := issue("i1", "A-1", "x")
	hi.EstimatedComplexity = strp("high")
	require.Equal(t, []string{"planner", "coder", "verifier", "reviewer"}, r.SelectPipeline(hi))

	lo := issue("i2", "A-2", "x")
	lo.EstimatedComplexity = strp("low")
	require.Equal(t, []string{"coder"}, r.SelectPipeline(lo))
}

func TestSelectPipeline_EmptyRulePipelineIgnored(t *testing.T) {
	routing := config.Routing{
		Pipeline: []string{"coder"},
		Rules: []config.RoutingRule{
			{When: config.RoutingMatch{State: "Todo"}, Pipeline: nil}, // matches but no pipeline
		},
	}
	r := routerWithRouting(routing)
	require.Equal(t, []string{"coder"}, r.SelectPipeline(issue("i1", "A-1", "x")))
}

var _ = tracker.Issue{}
