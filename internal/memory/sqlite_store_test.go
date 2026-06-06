package memory

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) *sqliteStore {
	t.Helper()
	s, err := newSQLiteStore(context.Background(), "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestSQLiteStorePutQueryScoped(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	exp := now.AddDate(0, 0, 90)
	entries := []MemoryEntry{
		{ID: "e1", Layer: LayerEpisodic, ProjectID: "p", IssueID: "ABC-1", Content: "ep one", Source: SourceAutoExtracted, CreatedAt: now, ExpiresAt: &exp, Embedding: []float32{1, 0, 0}},
		{ID: "s1", Layer: LayerSemantic, ProjectID: "p", Content: "sem one", Source: SourceConsolidated, CreatedAt: now},
		{ID: "pr1", Layer: LayerProcedural, ProjectID: "p", TaskType: "feature", Content: "proc one", Source: SourceConsolidated, CreatedAt: now},
	}
	for _, e := range entries {
		require.NoError(t, s.Put(ctx, e))
	}

	ep, err := s.Query(ctx, QueryFilter{ProjectID: "p", Layer: LayerEpisodic, IssueID: "ABC-1"})
	require.NoError(t, err)
	require.Len(t, ep, 1)
	require.Equal(t, "ep one", ep[0].Content)
	require.NotNil(t, ep[0].ExpiresAt)
	require.Equal(t, []float32{1, 0, 0}, ep[0].Embedding)

	pr, err := s.Query(ctx, QueryFilter{ProjectID: "p", Layer: LayerProcedural, TaskType: "feature"})
	require.NoError(t, err)
	require.Len(t, pr, 1)

	none, err := s.Query(ctx, QueryFilter{ProjectID: "p", Layer: LayerProcedural, TaskType: "bugfix"})
	require.NoError(t, err)
	require.Empty(t, none)
}

func TestSQLiteStoreRetireRemovesFromRetrieval(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	now := time.Now().UTC()
	require.NoError(t, s.Put(ctx, MemoryEntry{ID: "s1", Layer: LayerSemantic, ProjectID: "p", Content: "x", Source: SourceAgentWritten, CreatedAt: now}))

	require.NoError(t, s.Retire(ctx, "s1"))
	out, err := s.Query(ctx, QueryFilter{ProjectID: "p", Layer: LayerSemantic})
	require.NoError(t, err)
	require.Empty(t, out)
}

func TestSQLiteStoreRestartReconstructsEntries(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := dir + "/mem.db"

	s1, err := newSQLiteStore(ctx, path)
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, s1.Put(ctx, MemoryEntry{
		ID: "s1", Layer: LayerSemantic, ProjectID: "p", Content: "durable", Tags: []string{"a", "b"},
		Source: SourceAgentWritten, CreatedAt: now,
	}))
	require.NoError(t, s1.Close())

	s2, err := newSQLiteStore(ctx, path)
	require.NoError(t, err)
	defer func() { _ = s2.Close() }()

	got, err := s2.Get(ctx, "s1")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, LayerSemantic, got.Layer)
	require.Equal(t, "p", got.ProjectID)
	require.Equal(t, []string{"a", "b"}, got.Tags)
}

func TestSQLiteStoreEmbeddingRoundTrip(t *testing.T) {
	vec := []float32{0.1, -0.5, 3.14, 0}
	require.Equal(t, vec, decodeEmbedding(encodeEmbedding(vec)))
	require.Nil(t, encodeEmbedding(nil))
	require.Nil(t, decodeEmbedding([]byte{1, 2, 3}))
}
