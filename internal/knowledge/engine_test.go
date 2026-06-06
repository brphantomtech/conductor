package knowledge

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/audit"
	"github.com/conductor-sh/conductor/internal/config"
)

// captureSink records every event for assertion.
type captureSink struct {
	mu     sync.Mutex
	events []audit.AuditEvent
}

func (c *captureSink) Write(_ context.Context, evt audit.AuditEvent) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, evt)
	return nil
}
func (c *captureSink) Close() error { return nil }
func (c *captureSink) byType(t audit.EventType) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, e := range c.events {
		if e.EventType == t {
			n++
		}
	}
	return n
}

func testEngine(t *testing.T, cfg config.Knowledge, opts ...Option) (*Engine, *captureSink) {
	t.Helper()
	store := newTestStore(t)
	sink := &captureSink{}
	writer := audit.NewWriter(zerolog.Nop())
	writer.AddSink(sink)
	base := []Option{
		WithStore(store),
		WithAudit(writer),
		WithClock(fixedClock(time.Unix(1700000000, 0).UTC())),
	}
	eng := New(cfg, "proj", append(base, opts...)...)
	return eng, sink
}

func TestIndexEndToEnd(t *testing.T) {
	root := writeFixtureTree(t)
	cfg := config.Knowledge{
		Enabled: true,
		UseAST:  true,
		LayerDefinitions: map[string][]string{
			"app":  {"main.go"},
			"auth": {"internal/auth/**"},
		},
	}
	eng, sink := testEngine(t, cfg, WithEmbedder(newFakeEmbedder()), WithSummarizer(&fakeSummarizer{}))

	count, err := eng.Index(context.Background(), []string{root})
	require.NoError(t, err)
	require.Greater(t, count, 0)
	require.Equal(t, StatusReady, eng.Status())
	require.Equal(t, 1, sink.byType(audit.EventKnowledgeIndexed))

	// Symbol nodes from the Go file were persisted with the auth layer.
	syms, err := eng.QueryStructural(context.Background(),
		Filter{Types: []NodeType{NodeSymbol}, Layers: []string{"auth"}}, 0)
	require.NoError(t, err)
	require.NotEmpty(t, syms)

	// The file node carries an import edge to the in-project types target.
	fileNodes, err := eng.QueryStructural(context.Background(),
		Filter{Types: []NodeType{NodeFile}, PathPattern: "internal/auth/auth.go"}, 0)
	require.NoError(t, err)
	require.Len(t, fileNodes, 1)
	var hasImport bool
	for _, e := range fileNodes[0].OutgoingEdges {
		if e.EdgeType == EdgeImports {
			hasImport = true
		}
	}
	require.True(t, hasImport)
}

func TestIndexUnchangedReusesCache(t *testing.T) {
	root := writeFixtureTree(t)
	cfg := config.Knowledge{Enabled: true}
	emb := newFakeEmbedder()
	sum := &fakeSummarizer{}
	eng, _ := testEngine(t, cfg, WithEmbedder(emb), WithSummarizer(sum))

	_, err := eng.Index(context.Background(), []string{root})
	require.NoError(t, err)
	firstEmb := emb.callCount()
	firstSum := sum.callCount()
	require.Greater(t, firstEmb, 0)

	// Re-index without changes: no new embedding/summary calls.
	_, err = eng.Index(context.Background(), []string{root})
	require.NoError(t, err)
	require.Equal(t, firstEmb, emb.callCount(), "unchanged files must not re-embed")
	require.Equal(t, firstSum, sum.callCount(), "unchanged files must not re-summarize")
}

func TestIndexEmbeddingErrorClassified(t *testing.T) {
	root := writeFixtureTree(t)
	cfg := config.Knowledge{Enabled: true}
	emb := newFakeEmbedder()
	emb.fail = true
	eng, _ := testEngine(t, cfg, WithEmbedder(emb), WithSummarizer(&fakeSummarizer{}))

	_, err := eng.Index(context.Background(), []string{root})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrIndexFailed), "index surfaces ErrIndexFailed")
	require.True(t, errors.Is(err, ErrEmbeddingRequestFailed), "underlying embedding error classified")
	require.Equal(t, StatusStale, eng.Status())
}

func TestIncrementalIndexFileAndRemove(t *testing.T) {
	root := writeFixtureTree(t)
	cfg := config.Knowledge{Enabled: true}
	eng, _ := testEngine(t, cfg, WithEmbedder(newFakeEmbedder()), WithSummarizer(&fakeSummarizer{}))
	_, err := eng.Index(context.Background(), []string{root})
	require.NoError(t, err)

	before, err := eng.QueryStructural(context.Background(), Filter{PathPattern: "main.go"}, 0)
	require.NoError(t, err)
	require.NotEmpty(t, before)

	require.NoError(t, eng.RemoveFile(context.Background(), "main.go"))
	after, err := eng.QueryStructural(context.Background(), Filter{PathPattern: "main.go"}, 0)
	require.NoError(t, err)
	require.Empty(t, after, "removed file purged")

	// Re-index just that file.
	require.NoError(t, eng.IndexFile(context.Background(), root, "main.go"))
	again, err := eng.QueryStructural(context.Background(), Filter{PathPattern: "main.go"}, 0)
	require.NoError(t, err)
	require.NotEmpty(t, again)
}
