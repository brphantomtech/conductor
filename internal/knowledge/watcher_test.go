package knowledge

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/config"
)

// newWatchEngine builds an engine over a fixture root and indexes it once.
func newWatchEngine(t *testing.T) (*Engine, string) {
	t.Helper()
	root := writeFixtureTree(t)
	eng, _ := testEngine(t, config.Knowledge{Enabled: true},
		WithEmbedder(newFakeEmbedder()), WithSummarizer(&fakeSummarizer{}))
	_, err := eng.Index(context.Background(), []string{root})
	require.NoError(t, err)
	return eng, root
}

func TestWatcherFlushReindexesChangedFile(t *testing.T) {
	eng, root := newWatchEngine(t)
	w := NewWatcher(eng, []string{root}, WithWatchDebounce(time.Millisecond))

	// Change main.go on disk.
	abs := filepath.Join(root, "main.go")
	require.NoError(t, os.WriteFile(abs, []byte("package main\nfunc main() { _ = 1 }\nfunc extra() {}\n"), 0o644))

	require.True(t, w.record(fsnotify.Event{Name: abs, Op: fsnotify.Write}))
	w.flush(context.Background())

	nodes, err := eng.QueryStructural(context.Background(), Filter{PathPattern: "main.go"}, 0)
	require.NoError(t, err)
	require.NotEmpty(t, nodes)
	// The new checksum must be persisted.
	content, _ := os.ReadFile(abs)
	require.Equal(t, Checksum(content), nodes[0].Checksum)
}

func TestWatcherFlushPurgesRemovedFile(t *testing.T) {
	eng, root := newWatchEngine(t)
	w := NewWatcher(eng, []string{root}, WithWatchDebounce(time.Millisecond))

	abs := filepath.Join(root, "main.go")
	require.NoError(t, os.Remove(abs))

	require.True(t, w.record(fsnotify.Event{Name: abs, Op: fsnotify.Remove}))
	w.flush(context.Background())

	nodes, err := eng.QueryStructural(context.Background(), Filter{PathPattern: "main.go"}, 0)
	require.NoError(t, err)
	require.Empty(t, nodes, "removed file purged from index")
}

func TestWatcherCoalescesBurst(t *testing.T) {
	eng, root := newWatchEngine(t)
	w := NewWatcher(eng, []string{root}, WithWatchDebounce(time.Millisecond))

	abs := filepath.Join(root, "main.go")
	// A burst of events for the same file coalesces into one pending entry.
	w.record(fsnotify.Event{Name: abs, Op: fsnotify.Write})
	w.record(fsnotify.Event{Name: abs, Op: fsnotify.Write})
	w.record(fsnotify.Event{Name: abs, Op: fsnotify.Chmod}) // ignored

	w.mu.Lock()
	pendingCount := len(w.pending)
	w.mu.Unlock()
	require.Equal(t, 1, pendingCount, "burst coalesced to one pending file")
}

func TestWatcherChmodIgnored(t *testing.T) {
	eng, root := newWatchEngine(t)
	w := NewWatcher(eng, []string{root})
	require.False(t, w.record(fsnotify.Event{Name: filepath.Join(root, "main.go"), Op: fsnotify.Chmod}))
}
