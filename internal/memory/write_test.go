package memory

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/audit"
	"github.com/conductor-sh/conductor/internal/config"
)

func TestWritePathsTagSource(t *testing.T) {
	ctx := context.Background()
	store := newFakeStore()
	rec := &recordingAudit{}
	m, err := New(ctx, enabledConfig(), WithStore(store), WithProjectID("proj"), withAuditWriter(rec))
	require.NoError(t, err)

	cases := []struct {
		name   string
		in     WriteInput
		source Source
	}{
		{"agent", WriteInput{Layer: LayerSemantic, ProjectID: "proj", Content: "a", Source: SourceAgentWritten}, SourceAgentWritten},
		{"auto", WriteInput{Layer: LayerSemantic, ProjectID: "proj", Content: "b", Source: SourceAutoExtracted}, SourceAutoExtracted},
		{"consolidated", WriteInput{Layer: LayerSemantic, ProjectID: "proj", Content: "c", Source: SourceConsolidated}, SourceConsolidated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, err := m.Write(ctx, tc.in)
			require.NoError(t, err)
			require.Equal(t, tc.source, e.Source)
		})
	}
	require.Equal(t, 3, rec.typesOf(audit.EventMemoryWritten))
}

func TestWriteValidationRejectsBadEntry(t *testing.T) {
	ctx := context.Background()
	m := newTestManager(t, newFakeStore(), nil, nil, nil)

	_, err := m.Write(ctx, WriteInput{Layer: "bogus", ProjectID: "proj", Content: "x", Source: SourceAgentWritten})
	require.ErrorIs(t, err, ErrWriteFailed)
	require.ErrorIs(t, err, ErrInvalidEntry)

	_, err = m.Write(ctx, WriteInput{Layer: LayerSemantic, ProjectID: "proj", Content: "   ", Source: SourceAgentWritten})
	require.ErrorIs(t, err, ErrWriteFailed)
}

func TestWriteFailureClassifiedAsWriteFailed(t *testing.T) {
	ctx := context.Background()
	store := newFakeStore()
	store.putErr = errFakeStore
	m := newTestManager(t, store, nil, nil, nil)

	_, err := m.Write(ctx, WriteInput{Layer: LayerSemantic, ProjectID: "proj", Content: "x", Source: SourceAgentWritten})
	require.ErrorIs(t, err, ErrWriteFailed)
	require.ErrorIs(t, err, errFakeStore)
}

func TestExtractParsesEachPattern(t *testing.T) {
	output := `Some normal text.
MEMORY: the build uses make test
<memory type="procedural">step one then step two</memory>
<memory type="episodic">attempt 2 failed on the health endpoint</memory>
<memory type="weird">unknown type body</memory>
trailing line`

	got := Extract(output)
	require.Len(t, got, 4)

	bySemantic := findContent(got, LayerSemantic)
	require.Contains(t, bySemantic, "the build uses make test")
	require.Contains(t, findContent(got, LayerProcedural), "step one then step two")
	episodic := findContent(got, LayerEpisodic)
	require.Contains(t, episodic, "attempt 2 failed on the health endpoint")
	// Unknown type downgrades to episodic.
	require.Contains(t, episodic, "unknown type body")
}

func TestIngestAgentOutputWritesAutoExtracted(t *testing.T) {
	ctx := context.Background()
	store := newFakeStore()
	m := newTestManager(t, store, nil, nil, nil)

	written, err := m.IngestAgentOutput(ctx, "proj", "ABC-1", "feature",
		"MEMORY: always run gofmt\n<memory type=\"episodic\">broke the lint</memory>")
	require.NoError(t, err)
	require.Len(t, written, 2)
	for _, e := range written {
		require.Equal(t, SourceAutoExtracted, e.Source)
	}
	// Episodic candidate scoped to the issue.
	ep, err := store.Query(ctx, QueryFilter{ProjectID: "proj", Layer: LayerEpisodic, IssueID: "ABC-1"})
	require.NoError(t, err)
	require.Len(t, ep, 1)
}

func TestRecordValidationFailureIsEpisodic(t *testing.T) {
	ctx := context.Background()
	store := newFakeStore()
	m := newTestManager(t, store, nil, nil, nil)

	e, err := m.RecordValidationFailure(ctx, "proj", "ABC-1", "lint", "unused import")
	require.NoError(t, err)
	require.Equal(t, LayerEpisodic, e.Layer)
	require.Equal(t, "ABC-1", e.IssueID)
	require.Equal(t, SourceValidationResult, e.Source)
	require.Contains(t, e.Content, "lint")
}

func TestDisabledManagerWriteIsNoop(t *testing.T) {
	ctx := context.Background()
	store := newFakeStore()
	m, err := New(ctx, config.Memory{}, WithStore(store), WithProjectID("proj"))
	require.NoError(t, err)

	e, err := m.Write(ctx, WriteInput{Layer: LayerSemantic, ProjectID: "proj", Content: "x", Source: SourceAgentWritten})
	require.NoError(t, err)
	require.Nil(t, e)
	require.Equal(t, 0, store.count())
}

func findContent(in []Extracted, layer Layer) []string {
	var out []string
	for _, e := range in {
		if e.Layer == layer {
			out = append(out, e.Content)
		}
	}
	return out
}
