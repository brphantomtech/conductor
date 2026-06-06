package memory

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEpisodicTTLSet(t *testing.T) {
	ctx := context.Background()
	clk := newStepClock(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	store := newFakeStore()
	m := newTestManager(t, store, nil, nil, clk.Now)

	e, err := m.Write(ctx, WriteInput{Layer: LayerEpisodic, ProjectID: "proj", IssueID: "ABC-1", Content: "tried X", Source: SourceAutoExtracted})
	require.NoError(t, err)
	require.NotNil(t, e.ExpiresAt)
	require.Equal(t, clk.Now().AddDate(0, 0, 90), *e.ExpiresAt)
}

func TestSemanticAndProceduralNoTTL(t *testing.T) {
	ctx := context.Background()
	store := newFakeStore()
	m := newTestManager(t, store, nil, nil, nil)

	sem, err := m.Write(ctx, WriteInput{Layer: LayerSemantic, ProjectID: "proj", Content: "uses repo pattern", Source: SourceAgentWritten})
	require.NoError(t, err)
	require.Nil(t, sem.ExpiresAt)

	proc, err := m.Write(ctx, WriteInput{Layer: LayerProcedural, ProjectID: "proj", TaskType: "feature", Content: "do A then B", Source: SourceAgentWritten})
	require.NoError(t, err)
	require.Nil(t, proc.ExpiresAt)
}

func TestDeleteExpiredOnlyRemovesExpired(t *testing.T) {
	ctx := context.Background()
	clk := newStepClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	store := newFakeStore()
	m := newTestManager(t, store, nil, nil, clk.Now)

	// Two episodic with 90-day TTL plus one semantic (never expires).
	_, err := m.Write(ctx, WriteInput{Layer: LayerEpisodic, ProjectID: "proj", IssueID: "ABC-1", Content: "old episodic", Source: SourceAutoExtracted})
	require.NoError(t, err)
	_, err = m.Write(ctx, WriteInput{Layer: LayerSemantic, ProjectID: "proj", Content: "durable semantic", Source: SourceConsolidated})
	require.NoError(t, err)

	// Advance past the episodic TTL, then write a fresh episodic that should survive.
	clk.Advance(91 * 24 * time.Hour)
	_, err = m.Write(ctx, WriteInput{Layer: LayerEpisodic, ProjectID: "proj", IssueID: "ABC-2", Content: "fresh episodic", Source: SourceAutoExtracted})
	require.NoError(t, err)

	n, err := store.DeleteExpired(ctx, "proj", clk.Now())
	require.NoError(t, err)
	require.Equal(t, 1, n)

	remaining, err := store.Query(ctx, QueryFilter{ProjectID: "proj"})
	require.NoError(t, err)
	require.Len(t, remaining, 2) // fresh episodic + semantic
}
