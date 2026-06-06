package memory

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLayerAndSourceValid(t *testing.T) {
	require.True(t, LayerEpisodic.Valid())
	require.True(t, LayerSemantic.Valid())
	require.True(t, LayerProcedural.Valid())
	require.False(t, Layer("bogus").Valid())

	require.True(t, SourceAgentWritten.Valid())
	require.True(t, SourceAutoExtracted.Valid())
	require.True(t, SourceConsolidated.Valid())
	require.True(t, SourceValidationResult.Valid())
	require.False(t, Source("bogus").Valid())
}

func TestSentinelStrings(t *testing.T) {
	require.Equal(t, "memory_read_failed", ErrReadFailed.Error())
	require.Equal(t, "memory_write_failed", ErrWriteFailed.Error())
}

// TestRelevanceScoreNotPersisted asserts the SPEC §4.1.5 rule that
// relevance_score is computed at retrieval time and never stored.
func TestRelevanceScoreNotPersisted(t *testing.T) {
	store := newFakeStore()
	m := newTestManager(t, store, nil, nil, nil)

	e, err := m.Write(context.Background(), WriteInput{
		Layer:     LayerSemantic,
		ProjectID: "proj",
		Content:   "the project uses the repository pattern",
		Source:    SourceAgentWritten,
	})
	require.NoError(t, err)
	require.NotNil(t, e)

	got, err := store.Get(context.Background(), e.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, 0.0, got.RelevanceScore)
}

// newTestManager builds an enabled Manager wired with the supplied fakes.
func newTestManager(t *testing.T, store MemoryStore, emb Embedder, synth Synthesizer, clk func() time.Time) *Manager {
	t.Helper()
	opts := []Option{WithStore(store), WithProjectID("proj")}
	if emb != nil {
		opts = append(opts, WithEmbedder(emb))
	}
	if synth != nil {
		opts = append(opts, WithSynthesizer(synth))
	}
	if clk != nil {
		opts = append(opts, WithClock(clk))
	}
	m, err := New(context.Background(), enabledConfig(), opts...)
	require.NoError(t, err)
	return m
}
