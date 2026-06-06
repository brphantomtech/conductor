package memory

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/audit"
)

func TestRetrieveQuotasAndCap(t *testing.T) {
	ctx := context.Background()
	clk := newStepClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	store := newFakeStore()
	rec := &recordingAudit{}
	cfg := enabledConfig()
	cfg.MaxContextMemories = 5
	m, err := New(ctx, cfg, WithStore(store), WithProjectID("proj"),
		WithEmbedder(newFakeEmbedder()), WithClock(clk.Now), withAuditWriter(rec))
	require.NoError(t, err)

	// 5 episodic for the issue (quota 3), 5 semantic (quota 3), 2 procedural (quota 1).
	for i := 0; i < 5; i++ {
		clk.Advance(time.Minute)
		_, err := m.Write(ctx, WriteInput{Layer: LayerEpisodic, ProjectID: "proj", IssueID: "ABC-1", Content: "episodic " + string(rune('a'+i)), Source: SourceAutoExtracted})
		require.NoError(t, err)
	}
	for i := 0; i < 5; i++ {
		clk.Advance(time.Minute)
		_, err := m.Write(ctx, WriteInput{Layer: LayerSemantic, ProjectID: "proj", Content: "semantic " + string(rune('a'+i)), Source: SourceConsolidated})
		require.NoError(t, err)
	}
	for i := 0; i < 2; i++ {
		clk.Advance(time.Minute)
		_, err := m.Write(ctx, WriteInput{Layer: LayerProcedural, ProjectID: "proj", TaskType: "feature", Content: "proc " + string(rune('a'+i)), Source: SourceConsolidated})
		require.NoError(t, err)
	}

	got, err := m.Retrieve(ctx, RetrieveRequest{ProjectID: "proj", IssueID: "ABC-1", TaskType: "feature", Intent: "semantic a"})
	require.NoError(t, err)
	// quotas: 3 episodic + 3 semantic + 1 procedural = 7, capped to 5.
	require.Len(t, got, 5)
	require.Equal(t, 1, rec.typesOf(audit.EventMemoryRead))

	var ep, sem, proc int
	for _, e := range got {
		switch e.Layer {
		case LayerEpisodic:
			ep++
		case LayerSemantic:
			sem++
		case LayerProcedural:
			proc++
		}
	}
	require.LessOrEqual(t, ep, 3)
	require.LessOrEqual(t, sem, 3)
	require.LessOrEqual(t, proc, 1)
}

func TestRetrieveDedupesByContent(t *testing.T) {
	ctx := context.Background()
	clk := newStepClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	store := newFakeStore()
	m := newTestManager(t, store, newFakeEmbedder(), nil, clk.Now)

	// Same content in episodic and semantic; dedupe should keep one.
	clk.Advance(time.Minute)
	_, err := m.Write(ctx, WriteInput{Layer: LayerEpisodic, ProjectID: "proj", IssueID: "ABC-1", Content: "duplicate fact", Source: SourceAutoExtracted})
	require.NoError(t, err)
	clk.Advance(time.Minute)
	_, err = m.Write(ctx, WriteInput{Layer: LayerSemantic, ProjectID: "proj", Content: "duplicate fact", Source: SourceConsolidated})
	require.NoError(t, err)

	got, err := m.Retrieve(ctx, RetrieveRequest{ProjectID: "proj", IssueID: "ABC-1", Intent: "duplicate"})
	require.NoError(t, err)
	require.Len(t, got, 1)
}

func TestFormatSectionShape(t *testing.T) {
	entries := []MemoryEntry{
		{Layer: LayerEpisodic, Content: "tried ValidateJWT, removed in PR 89"},
		{Layer: LayerSemantic, Content: "uses the repository pattern"},
		{Layer: LayerProcedural, Content: "define types, add handler, register route"},
	}
	out := Format(entries)
	require.True(t, strings.HasPrefix(out, "## Relevant Memory"))
	require.Contains(t, out, headingEpisodic)
	require.Contains(t, out, headingSemantic)
	require.Contains(t, out, headingProcedural)
	require.Contains(t, out, "- tried ValidateJWT, removed in PR 89")

	require.Equal(t, "", Format(nil))
}

func TestFormatOmitsEmptySections(t *testing.T) {
	out := Format([]MemoryEntry{{Layer: LayerSemantic, Content: "only semantic"}})
	require.Contains(t, out, headingSemantic)
	require.NotContains(t, out, headingEpisodic)
	require.NotContains(t, out, headingProcedural)
}
