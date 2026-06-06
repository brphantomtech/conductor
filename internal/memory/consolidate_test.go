package memory

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/audit"
)

func TestConsolidateClusterSynthesizedToSemantic(t *testing.T) {
	ctx := context.Background()
	clk := newStepClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	store := newFakeStore()
	rec := &recordingAudit{}
	emb := newFakeEmbedder()
	// Put the three "auth" memories on the same cluster axis.
	emb.clusterOf = func(text string) int {
		if strings.Contains(text, "auth") {
			return 1
		}
		return 5
	}
	synth := &fakeSynth{result: "consolidated auth lesson"}

	cfg := enabledConfig()
	cfg.ConsolidationEnabled = true
	m, err := New(ctx, cfg, WithStore(store), WithProjectID("proj"),
		WithEmbedder(emb), WithSynthesizer(synth), WithClock(clk.Now), withAuditWriter(rec))
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		clk.Advance(time.Minute)
		_, err := m.Write(ctx, WriteInput{Layer: LayerEpisodic, ProjectID: "proj", IssueID: "ABC-1", Content: "auth issue " + string(rune('a'+i)), Source: SourceAutoExtracted})
		require.NoError(t, err)
	}
	// One unrelated episodic that should not form a 3-cluster.
	_, err = m.Write(ctx, WriteInput{Layer: LayerEpisodic, ProjectID: "proj", IssueID: "ABC-2", Content: "unrelated note", Source: SourceAutoExtracted})
	require.NoError(t, err)

	require.NoError(t, m.Consolidate(ctx, "proj"))

	require.GreaterOrEqual(t, synth.callCount(), 1)
	sem, err := store.Query(ctx, QueryFilter{ProjectID: "proj", Layer: LayerSemantic})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(sem), 1)
	require.Equal(t, "consolidated auth lesson", sem[0].Content)
	require.Equal(t, SourceConsolidated, sem[0].Source)
	require.Equal(t, 1, rec.typesOf(audit.EventMemoryConsolidated))
}

func TestConsolidateSynthesizesProceduralByTaskType(t *testing.T) {
	ctx := context.Background()
	clk := newStepClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	store := newFakeStore()
	emb := newFakeEmbedder()
	emb.clusterOf = func(string) int { return 0 } // all in one cluster
	synth := &fakeSynth{result: "procedural recipe"}

	cfg := enabledConfig()
	cfg.ConsolidationEnabled = true
	m, err := New(ctx, cfg, WithStore(store), WithProjectID("proj"),
		WithEmbedder(emb), WithSynthesizer(synth), WithClock(clk.Now))
	require.NoError(t, err)

	// Three procedural-tagged episodic memories sharing a task type.
	for i := 0; i < 3; i++ {
		clk.Advance(time.Minute)
		require.NoError(t, store.Put(ctx, MemoryEntry{
			ID: "e" + string(rune('a'+i)), Layer: LayerEpisodic, ProjectID: "proj",
			TaskType: "feature", Content: "feature step " + string(rune('a'+i)),
			Source: SourceAutoExtracted, CreatedAt: clk.Now(),
			Embedding: []float32{1, 0, 0, 0, 0, 0, 0, 0},
		}))
	}

	require.NoError(t, m.Consolidate(ctx, "proj"))

	proc, err := store.Query(ctx, QueryFilter{ProjectID: "proj", Layer: LayerProcedural, TaskType: "feature"})
	require.NoError(t, err)
	require.Len(t, proc, 1)
	require.Equal(t, "procedural recipe", proc[0].Content)
}

func TestConsolidateDeletesExpired(t *testing.T) {
	ctx := context.Background()
	clk := newStepClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	store := newFakeStore()
	cfg := enabledConfig()
	cfg.ConsolidationEnabled = true
	m, err := New(ctx, cfg, WithStore(store), WithProjectID("proj"), WithClock(clk.Now))
	require.NoError(t, err)

	_, err = m.Write(ctx, WriteInput{Layer: LayerEpisodic, ProjectID: "proj", IssueID: "ABC-1", Content: "will expire", Source: SourceAutoExtracted})
	require.NoError(t, err)

	clk.Advance(91 * 24 * time.Hour)
	require.NoError(t, m.Consolidate(ctx, "proj"))

	left, err := store.Query(ctx, QueryFilter{ProjectID: "proj", Layer: LayerEpisodic})
	require.NoError(t, err)
	require.Empty(t, left)
}
