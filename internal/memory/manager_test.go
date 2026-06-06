package memory

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/config"
)

func TestNewOpensOwnSQLiteStore(t *testing.T) {
	ctx := context.Background()
	cfg := enabledConfig() // empty StorePath → in-memory
	m, err := New(ctx, cfg, WithProjectID("proj"))
	require.NoError(t, err)
	defer func() { _ = m.Close() }()

	_, err = m.Write(ctx, WriteInput{Layer: LayerSemantic, ProjectID: "proj", Content: "persisted", Source: SourceAgentWritten})
	require.NoError(t, err)

	list, err := m.List(ctx, "proj")
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, "persisted", list[0].Content)
}

func TestDefaultsApplied(t *testing.T) {
	m := newTestManager(t, newFakeStore(), nil, nil, nil)
	require.Equal(t, 90, m.cfg.EpisodicTTLDays)
	require.Equal(t, 7, m.cfg.MaxContextMemories)
	require.Equal(t, 24, m.cfg.ConsolidationIntervalHours)
}

func TestStartRunsConsolidationOnTick(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	clk := newStepClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	store := newFakeStore()
	rec := &recordingAudit{}
	cfg := enabledConfig()
	cfg.ConsolidationEnabled = true
	cfg.ConsolidationIntervalHours = 1
	m, err := New(ctx, cfg, WithStore(store), WithProjectID("proj"), WithClock(clk.Now), withAuditWriter(rec))
	require.NoError(t, err)

	// Seed an expired episodic so a tick has observable work.
	exp := clk.Now().Add(-time.Hour)
	require.NoError(t, store.Put(ctx, MemoryEntry{
		ID: "e1", Layer: LayerEpisodic, ProjectID: "proj", IssueID: "i",
		Content: "expired", Source: SourceAutoExtracted, CreatedAt: clk.Now().Add(-2 * time.Hour), ExpiresAt: &exp,
	}))

	done := make(chan struct{})
	go func() {
		_ = m.Start(ctx)
		close(done)
	}()

	require.Eventually(t, func() bool {
		return rec.typesOf("MemoryConsolidated") >= 1
	}, 5*time.Second, 20*time.Millisecond)

	cancel()
	<-done

	left, err := store.Query(ctx, QueryFilter{ProjectID: "proj", Layer: LayerEpisodic})
	require.NoError(t, err)
	require.Empty(t, left)
}

func TestStartDisabledBlocksUntilCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	m, err := New(ctx, config.Memory{}, WithStore(newFakeStore()))
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() { done <- m.Start(ctx) }()
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after cancel")
	}
}
