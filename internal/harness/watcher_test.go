package harness_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/audit"
	"github.com/conductor-sh/conductor/internal/harness"
)

// recorderSink captures every AuditEvent the Watcher emits so tests can
// assert ConfigReloaded / ConfigReloadFailed are reaching the audit
// pipeline. Implements audit.Sink.
type recorderSink struct {
	mu     sync.Mutex
	events []audit.AuditEvent
}

func (r *recorderSink) Write(_ context.Context, evt audit.AuditEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, evt)
	return nil
}

func (r *recorderSink) Close() error { return nil }

func (r *recorderSink) snapshot() []audit.AuditEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]audit.AuditEvent, len(r.events))
	copy(out, r.events)
	return out
}

func newWatcherTest(t *testing.T, body string) (*harness.Watcher, *recorderSink, string, func()) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "HARNESS.md")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	rec := &recorderSink{}
	writer := audit.NewWriter(zerolog.Nop())
	writer.AddSink(rec)

	w, err := harness.NewWatcher(harness.WatchOptions{
		Path:     path,
		Audit:    writer,
		Logger:   zerolog.Nop(),
		Debounce: 25 * time.Millisecond,
	})
	require.NoError(t, err)

	cleanup := func() {
		_ = w.Close()
		_ = writer.Close()
	}
	return w, rec, path, cleanup
}

func TestWatcher_InitialLoad(t *testing.T) {
	t.Parallel()

	w, _, _, cleanup := newWatcherTest(t, validHarnessSrc)
	defer cleanup()

	cur := w.Current()
	require.NotNil(t, cur.Definition)
	require.Equal(t, "demo", cur.Config.Project.ID)
}

func TestWatcher_ReloadsOnFileChange(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "HARNESS.md")
	require.NoError(t, os.WriteFile(path, []byte(validHarnessSrc), 0o600))

	rec := &recorderSink{}
	writer := audit.NewWriter(zerolog.Nop())
	writer.AddSink(rec)
	t.Cleanup(func() { _ = writer.Close() })

	updated := make(chan struct{}, 4)
	var updates int32
	w, err := harness.NewWatcher(harness.WatchOptions{
		Path:     path,
		Audit:    writer,
		Logger:   zerolog.Nop(),
		Debounce: 25 * time.Millisecond,
		OnUpdate: func(_ harness.LoadResult) {
			atomic.AddInt32(&updates, 1)
			select {
			case updated <- struct{}{}:
			default:
			}
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = w.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, w.Start(ctx))

	// Mutate the polling.interval_ms so we can detect the swap.
	require.NoError(t, os.WriteFile(path, []byte(`---
project: { id: demo }
tracker:
  kind: linear
  api_key: t
  project_slug: x
providers:
  default:
    provider: openrouter
    api_key: sk
polling:
  interval_ms: 9999
---

## planner
Plan {{ issue.identifier }}.
## coder
Implement {{ issue.title }}.
## verifier
Verify {{ issue.identifier }}.
`), 0o600))

	select {
	case <-updated:
	case <-time.After(3 * time.Second):
		t.Fatal("watcher did not fire OnUpdate within 3s")
	}

	cur := w.Current()
	require.Equal(t, 9999, cur.Config.Polling.IntervalMS)

	// Settle window for any trailing event before we inspect the audit
	// log; fsnotify on Windows can fire a Chmod after a Write.
	time.Sleep(100 * time.Millisecond)

	events := rec.snapshot()
	hasReloaded := false
	for _, e := range events {
		if e.EventType == audit.EventConfigReloaded {
			hasReloaded = true
			break
		}
	}
	require.True(t, hasReloaded,
		"successful reload must emit ConfigReloaded (SPEC §17.2)")
}

func TestWatcher_KeepsLastGoodOnReloadFailure(t *testing.T) {
	t.Parallel()

	w, rec, path, cleanup := newWatcherTest(t, validHarnessSrc)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, w.Start(ctx))

	good := w.Current()
	require.Equal(t, "demo", good.Config.Project.ID)

	// Write a malformed front matter block.
	require.NoError(t, os.WriteFile(path, []byte(`---
- not a map
---

## coder
body
`), 0o600))

	require.Eventually(t, func() bool {
		for _, e := range rec.snapshot() {
			if e.EventType == audit.EventConfigReloadFailed {
				return true
			}
		}
		return false
	}, 3*time.Second, 25*time.Millisecond,
		"failed reload must emit ConfigReloadFailed (SPEC §17.2)")

	cur := w.Current()
	require.Equal(t, "demo", cur.Config.Project.ID,
		"failed reload must leave the last known good Config in place (SPEC §6.3)")
}

func TestNewWatcher_RejectsEmptyPath(t *testing.T) {
	t.Parallel()
	_, err := harness.NewWatcher(harness.WatchOptions{})
	require.Error(t, err)
}

func TestNewWatcher_FailsOnMissingFile(t *testing.T) {
	t.Parallel()
	_, err := harness.NewWatcher(harness.WatchOptions{
		Path:   filepath.Join(t.TempDir(), "no-such.md"),
		Logger: zerolog.Nop(),
	})
	require.Error(t, err)
	require.ErrorIs(t, err, harness.ErrMissingHarnessFile)
}

func TestEnsureExists(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.ErrorIs(t,
		harness.EnsureExists(filepath.Join(dir, "missing.md")),
		harness.ErrMissingHarnessFile)
	require.ErrorIs(t,
		harness.EnsureExists(dir),
		harness.ErrMissingHarnessFile,
		"directories must be reported as missing-file errors")

	path := filepath.Join(dir, "HARNESS.md")
	require.NoError(t, os.WriteFile(path, []byte("hi"), 0o600))
	require.NoError(t, harness.EnsureExists(path))
}

func TestWatcher_StartIsIdempotent(t *testing.T) {
	t.Parallel()

	w, _, _, cleanup := newWatcherTest(t, validHarnessSrc)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, w.Start(ctx))
	require.Error(t, w.Start(ctx),
		"calling Start a second time must return an error rather than panicking")
}

func TestWatcher_ConfigLevelValidationStillReturnsResult(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "HARNESS.md")
	// Pipeline references a role that has no `## planner` template, but
	// the harness itself parses cleanly. NewWatcher should surface the
	// validation error AND populate Current().
	require.NoError(t, os.WriteFile(path, []byte(`---
project: { id: demo }
tracker:
  kind: linear
  api_key: t
  project_slug: x
providers:
  default:
    provider: openrouter
    api_key: sk
---

## coder
Implement.
`), 0o600))

	w, err := harness.NewWatcher(harness.WatchOptions{
		Path:   path,
		Logger: zerolog.Nop(),
	})
	require.Error(t, err,
		"validation failures at startup must be returned to the caller (SPEC §6.4)")
	require.NotNil(t, w,
		"NewWatcher must still return a Watcher when the failure is at validation time, not parse time")
	t.Cleanup(func() { _ = w.Close() })

	cur := w.Current()
	require.Equal(t, "demo", cur.Config.Project.ID,
		"Current must surface the parsed config even when validation reported issues")
}
