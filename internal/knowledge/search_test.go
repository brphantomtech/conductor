package knowledge

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/config"
)

// seedGraph upserts a small graph directly into an engine's store.
func seedGraph(t *testing.T, eng *Engine) {
	t.Helper()
	ctx := context.Background()
	embed := func(s string) []float32 {
		v, _ := newHashEmbedder(hashEmbedDim).Embed(ctx, s)
		return v
	}
	auth := Node{
		ID: NodeID("proj", "auth.go", ""), Type: NodeFile, ProjectID: "proj",
		Path: "auth.go", Name: "auth.go", LayerID: "service",
		Summary: "authentication token validation login session", Embedding: embed("authentication token validation login session"),
		OutgoingEdges: []Edge{{FromID: NodeID("proj", "auth.go", ""), ToID: NodeID("proj", "user.go", ""), EdgeType: EdgeImports}},
	}
	user := Node{
		ID: NodeID("proj", "user.go", ""), Type: NodeFile, ProjectID: "proj",
		Path: "user.go", Name: "user.go", LayerID: "types",
		Summary: "user entity model fields", Embedding: embed("user entity model fields"),
	}
	render := Node{
		ID: NodeID("proj", "render.go", ""), Type: NodeFile, ProjectID: "proj",
		Path: "render.go", Name: "render.go", LayerID: "ui",
		Summary: "html template rendering view", Embedding: embed("html template rendering view"),
	}
	require.NoError(t, eng.store.Upsert(ctx, []Node{auth, user, render}))
}

func TestSemanticSearchRanking(t *testing.T) {
	eng, _ := testEngine(t, config.Knowledge{Enabled: true})
	seedGraph(t, eng)

	res, err := eng.Search(context.Background(), SearchParams{Query: "token validation login"})
	require.NoError(t, err)
	require.NotEmpty(t, res)
	require.Equal(t, "auth.go", res[0].Node.Path, "best semantic match ranks first")
}

func TestStructuralFilterRestricts(t *testing.T) {
	eng, _ := testEngine(t, config.Knowledge{Enabled: true})
	seedGraph(t, eng)

	res, err := eng.Search(context.Background(), SearchParams{
		Query:  "anything",
		Layers: []string{"types"},
	})
	require.NoError(t, err)
	require.Len(t, res, 1)
	require.Equal(t, "user.go", res[0].Node.Path)
}

func TestDependencyExpansionIncludesNeighbors(t *testing.T) {
	eng, _ := testEngine(t, config.Knowledge{Enabled: true})
	seedGraph(t, eng)

	res, err := eng.Search(context.Background(), SearchParams{
		Query:               "token validation login",
		Layers:              []string{"service"},
		IncludeDependencies: true,
		TopK:                10,
	})
	require.NoError(t, err)
	paths := map[string]bool{}
	for _, r := range res {
		paths[r.Node.Path] = true
	}
	require.True(t, paths["auth.go"], "seed result present")
	require.True(t, paths["user.go"], "dependency neighbor included")
}

func TestTopKBound(t *testing.T) {
	eng, _ := testEngine(t, config.Knowledge{Enabled: true})
	seedGraph(t, eng)

	res, err := eng.Search(context.Background(), SearchParams{Query: "x", TopK: 2})
	require.NoError(t, err)
	require.LessOrEqual(t, len(res), 2)
}

func TestStructuralOnlyQueryWhenNoSeed(t *testing.T) {
	eng, _ := testEngine(t, config.Knowledge{Enabled: true})
	seedGraph(t, eng)

	res, err := eng.Search(context.Background(), SearchParams{Types: []NodeType{NodeFile}})
	require.NoError(t, err)
	require.Len(t, res, 3)
}
