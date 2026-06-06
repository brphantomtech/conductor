package knowledge

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	s, err := NewSQLiteStore(context.Background(), "", zerolog.Nop())
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func sampleNode(id, path, name string, layer string, emb []float32) Node {
	return Node{
		ID:            id,
		Type:          NodeSymbol,
		ProjectID:     "proj",
		Path:          path,
		Name:          name,
		Summary:       "summary of " + name,
		Content:       "content of " + name,
		Embedding:     emb,
		LayerID:       layer,
		Language:      "go",
		LastIndexedAt: time.Unix(1700000000, 0).UTC(),
		Checksum:      "csum-" + id,
	}
}

func TestStoreUpsertIdempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	n := sampleNode("id1", "a.go", "A", "service", []float32{1, 0, 0})

	require.NoError(t, s.Upsert(ctx, []Node{n}))
	require.NoError(t, s.Upsert(ctx, []Node{n}))

	cnt, err := s.Count(ctx, "proj")
	require.NoError(t, err)
	require.Equal(t, 1, cnt, "second upsert must not duplicate")

	// Updated content is reflected.
	n.Summary = "updated"
	require.NoError(t, s.Upsert(ctx, []Node{n}))
	got, err := s.QueryStructural(ctx, Filter{}, 0)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "updated", got[0].Summary)
}

func TestStoreDeleteByPathRemovesNodesAndEdges(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a := sampleNode("a", "a.go", "A", "service", []float32{1, 0})
	a.OutgoingEdges = []Edge{{FromID: "a", ToID: "b", EdgeType: EdgeImports}}
	b := sampleNode("b", "b.go", "B", "types", []float32{0, 1})
	require.NoError(t, s.Upsert(ctx, []Node{a, b}))

	require.NoError(t, s.DeleteByPath(ctx, "proj", "a.go"))

	got, err := s.QueryStructural(ctx, Filter{}, 0)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "b", got[0].ID)

	// Edge from a must be gone; b has no incoming edge left.
	in, err := s.Neighbors(ctx, "b", Incoming)
	require.NoError(t, err)
	require.Empty(t, in)
}

func TestStoreStructuralFilter(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	svc := sampleNode("a", "internal/svc/a.go", "A", "service", []float32{1, 0})
	typ := sampleNode("b", "internal/types/b.go", "B", "types", []float32{0, 1})
	chunk := sampleNode("c", "docs/readme.md", "readme", "unknown", []float32{1, 1})
	chunk.Type = NodeChunk
	chunk.Language = ""
	require.NoError(t, s.Upsert(ctx, []Node{svc, typ, chunk}))

	byLayer, err := s.QueryStructural(ctx, Filter{Layers: []string{"types"}}, 0)
	require.NoError(t, err)
	require.Len(t, byLayer, 1)
	require.Equal(t, "b", byLayer[0].ID)

	byType, err := s.QueryStructural(ctx, Filter{Types: []NodeType{NodeChunk}}, 0)
	require.NoError(t, err)
	require.Len(t, byType, 1)
	require.Equal(t, "c", byType[0].ID)

	byLang, err := s.QueryStructural(ctx, Filter{Languages: []string{"go"}}, 0)
	require.NoError(t, err)
	require.Len(t, byLang, 2)

	byPath, err := s.QueryStructural(ctx, Filter{PathPattern: "internal/svc/**"}, 0)
	require.NoError(t, err)
	require.Len(t, byPath, 1)
	require.Equal(t, "a", byPath[0].ID)
}

func TestStoreSemanticRanking(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	near := sampleNode("near", "near.go", "Near", "service", []float32{1, 0, 0})
	far := sampleNode("far", "far.go", "Far", "service", []float32{0, 1, 0})
	require.NoError(t, s.Upsert(ctx, []Node{near, far}))

	res, err := s.SearchSemantic(ctx, []float32{0.9, 0.1, 0}, Filter{}, 10)
	require.NoError(t, err)
	require.Len(t, res, 2)
	require.Equal(t, "near", res[0].Node.ID, "closest vector ranks first")
	require.GreaterOrEqual(t, res[0].Score, res[1].Score)
}

func TestStoreNeighbors(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a := sampleNode("a", "a.go", "A", "service", []float32{1, 0})
	a.OutgoingEdges = []Edge{{FromID: "a", ToID: "b", EdgeType: EdgeImports}}
	b := sampleNode("b", "b.go", "B", "types", []float32{0, 1})
	require.NoError(t, s.Upsert(ctx, []Node{a, b}))

	out, err := s.Neighbors(ctx, "a", Outgoing)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, "b", out[0].ID)

	in, err := s.Neighbors(ctx, "b", Incoming)
	require.NoError(t, err)
	require.Len(t, in, 1)
	require.Equal(t, "a", in[0].ID)
}
