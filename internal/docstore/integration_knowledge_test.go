package docstore

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/config"
	"github.com/conductor-sh/conductor/internal/knowledge"
)

// hashEmbedder is a small deterministic embedder so the integration test can
// run a semantic-ish search without a live embedding provider.
type hashEmbedder struct{ dim int }

func (h hashEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	v := make([]float32, h.dim)
	for i, r := range text {
		v[i%h.dim] += float32(r%13) / 13.0
	}
	return v, nil
}

func TestSyncedDocBecomesSearchableDocNode(t *testing.T) {
	ctx := context.Background()
	store, err := knowledge.NewSQLiteStore(ctx, "", zerolog.Nop())
	require.NoError(t, err)
	defer func() { _ = store.Close() }()

	emb := hashEmbedder{dim: 16}
	backend := newStubBackend(map[string]string{
		"design.md": "# Architecture Design\nThe orchestrator runs a poll loop.",
	})

	m, err := New(config.Docs{Enabled: true}, "proj",
		WithProjectID("proj"),
		WithKnowledgeStore(store),
		WithEmbedder(emb),
		WithBackend("specs", backend, config.DocStoreConfig{ID: "specs"}),
	)
	require.NoError(t, err)
	require.NoError(t, m.SyncStore(ctx, "specs"))

	// Build an engine over the same store and run a doc-filtered search.
	eng := knowledge.New(config.Knowledge{Enabled: true}, "proj",
		knowledge.WithStore(store),
		knowledge.WithEmbedder(emb),
	)
	results, err := eng.Search(ctx, knowledge.SearchParams{
		Query: "architecture design",
		Types: []knowledge.NodeType{knowledge.NodeDoc},
	})
	require.NoError(t, err)
	require.NotEmpty(t, results)
	for _, r := range results {
		assert.Equal(t, knowledge.NodeDoc, r.Node.Type)
	}
	assert.Equal(t, "docs/stub/design.md", results[0].Node.Path)
}
