package memory

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/config"
	"github.com/conductor-sh/conductor/internal/orchestrator"
)

func TestPostProcessWritesEpisodicForTerminalAttempt(t *testing.T) {
	ctx := context.Background()
	store := newFakeStore()
	m := newTestManager(t, store, nil, nil, nil)
	pp := NewPostProcessor(m)

	attempt := &orchestrator.RunAttempt{
		ID:         "att-1",
		IssueID:    "ABC-1",
		Identifier: "ABC-1",
		Attempt:    2,
		Outcome:    orchestrator.OutcomeFailed,
	}
	require.NoError(t, pp.PostProcess(ctx, attempt))

	ep, err := store.Query(ctx, QueryFilter{ProjectID: "proj", Layer: LayerEpisodic, IssueID: "ABC-1"})
	require.NoError(t, err)
	require.Len(t, ep, 1)
	require.Contains(t, ep[0].Content, "failed")
}

func TestPostProcessDisabledIsNoop(t *testing.T) {
	ctx := context.Background()
	store := newFakeStore()
	m, err := New(ctx, config.Memory{}, WithStore(store), WithProjectID("proj"))
	require.NoError(t, err)
	pp := NewPostProcessor(m)

	require.NoError(t, pp.PostProcess(ctx, &orchestrator.RunAttempt{IssueID: "ABC-1", Outcome: orchestrator.OutcomeSucceeded}))
	require.Equal(t, 0, store.count())
}

func TestCountsPerLayer(t *testing.T) {
	ctx := context.Background()
	store := newFakeStore()
	m := newTestManager(t, store, nil, nil, nil)

	_, _ = m.Write(ctx, WriteInput{Layer: LayerEpisodic, ProjectID: "proj", IssueID: "i", Content: "a", Source: SourceAutoExtracted})
	_, _ = m.Write(ctx, WriteInput{Layer: LayerSemantic, ProjectID: "proj", Content: "b", Source: SourceConsolidated})
	_, _ = m.Write(ctx, WriteInput{Layer: LayerSemantic, ProjectID: "proj", Content: "c", Source: SourceConsolidated})

	counts, err := m.Counts(ctx, "proj")
	require.NoError(t, err)
	require.Equal(t, 1, counts.Episodic)
	require.Equal(t, 2, counts.Semantic)
	require.Equal(t, 0, counts.Procedural)
	require.Equal(t, 3, counts.Total())
}
