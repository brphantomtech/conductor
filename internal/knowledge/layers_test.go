package knowledge

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/config"
)

// layerCfg orders layers alphabetically: "service" < "types" < "ui".
// Lower index = lower layer. So service(0) depending on types(1) is "upward"
// (a violation by default); ui(2) depending on service(0) is allowed downward.
func layerCfg() config.Knowledge {
	return config.Knowledge{
		Enabled: true,
		LayerDefinitions: map[string][]string{
			"service": {"service/**"},
			"types":   {"types/**"},
			"ui":      {"ui/**"},
		},
	}
}

func seedLayerGraph(t *testing.T, eng *Engine, fromPath, fromLayer, toPath, toLayer string) {
	t.Helper()
	from := Node{
		ID: NodeID("proj", fromPath, ""), Type: NodeFile, ProjectID: "proj",
		Path: fromPath, LayerID: fromLayer,
		OutgoingEdges: []Edge{{
			FromID: NodeID("proj", fromPath, ""), ToID: NodeID("proj", toPath, ""), EdgeType: EdgeImports,
		}},
	}
	to := Node{
		ID: NodeID("proj", toPath, ""), Type: NodeFile, ProjectID: "proj",
		Path: toPath, LayerID: toLayer,
	}
	require.NoError(t, eng.store.Upsert(context.Background(), []Node{from, to}))
}

func TestUpwardDependencyReported(t *testing.T) {
	eng, _ := testEngine(t, layerCfg())
	// service (lower) depends on types (higher) => violation.
	seedLayerGraph(t, eng, "service/a.go", "service", "types/b.go", "types")

	v, err := eng.CheckLayerViolations(context.Background(), nil)
	require.NoError(t, err)
	require.Len(t, v, 1)
	require.Equal(t, "service", v[0].FromLayer)
	require.Equal(t, "types", v[0].ToLayer)
}

func TestDownwardDependencyNotReported(t *testing.T) {
	eng, _ := testEngine(t, layerCfg())
	// ui (higher) depends on service (lower) => allowed.
	seedLayerGraph(t, eng, "ui/a.go", "ui", "service/b.go", "service")

	v, err := eng.CheckLayerViolations(context.Background(), nil)
	require.NoError(t, err)
	require.Empty(t, v)
}

func TestUnknownLayerExcluded(t *testing.T) {
	eng, _ := testEngine(t, layerCfg())
	seedLayerGraph(t, eng, "service/a.go", "service", "weird/b.go", UnknownLayer)

	v, err := eng.CheckLayerViolations(context.Background(), nil)
	require.NoError(t, err)
	require.Empty(t, v, "unknown-layer targets are excluded")
}

func TestHarnessRuleOverride(t *testing.T) {
	eng, _ := testEngine(t, layerCfg())
	seedLayerGraph(t, eng, "service/a.go", "service", "types/b.go", "types")

	rules := []config.HarnessRule{
		{Category: "dependency", Check: "service -> types"},
	}
	v, err := eng.CheckLayerViolations(context.Background(), rules)
	require.NoError(t, err)
	require.Empty(t, v, "custom rule permits the otherwise-upward dependency")
}

func TestAssignLayer(t *testing.T) {
	eng, _ := testEngine(t, layerCfg())
	require.Equal(t, "service", eng.assignLayer("service/x.go"))
	require.Equal(t, "ui", eng.assignLayer("ui/x.go"))
	require.Equal(t, UnknownLayer, eng.assignLayer("misc/x.go"))
}
