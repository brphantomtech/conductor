package harness

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/config"
)

// captureWriter is an AuditWriter that retains every emitted event.
type captureWriter struct {
	mu     sync.Mutex
	events []AuditEvent
}

func (c *captureWriter) Write(_ context.Context, evt AuditEvent) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, evt)
	return nil
}

func (c *captureWriter) snapshot() []AuditEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]AuditEvent, len(c.events))
	copy(out, c.events)
	return out
}

func (c *captureWriter) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = nil
}

// waitFor polls cond every step until it returns true or deadline
// elapses. Returns the value of the last call.
func waitFor(deadline time.Duration, step time.Duration, cond func() bool) bool {
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if cond() {
			return true
		}
		time.Sleep(step)
	}
	return cond()
}

func newTestWatcher(t *testing.T, path string, initial *Definition) (*Watcher, *captureWriter) {
	t.Helper()
	writer := &captureWriter{}
	cfg := validCfg()
	w := NewWatcher(path, initial, cfg, writer, zerolog.Nop())
	// Drop debounce to a much smaller window for tests.
	w.debounce = 30 * time.Millisecond
	return w, writer
}

func TestWatcher_CleanReloadEmitsConfigReloaded(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "HARNESS.md", validHarnessYAML)

	def, err := Parse(path)
	require.NoError(t, err)

	w, writer := newTestWatcher(t, path, def)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = w.Run(ctx) }()

	// Let the watcher attach.
	time.Sleep(50 * time.Millisecond)

	// Modify the file: change project.id from `demo` to `demo2`.
	updated := validHarnessYAML
	require.NoError(t, os.WriteFile(path, []byte(updated+"\n# touched"), 0o644))

	ok := waitFor(2*time.Second, 25*time.Millisecond, func() bool {
		for _, e := range writer.snapshot() {
			if e.EventType == string(eventConfigReloaded) {
				return true
			}
		}
		return false
	})
	require.True(t, ok, "expected ConfigReloaded event, got %+v", writer.snapshot())

	live := w.Live()
	require.NotNil(t, live)
	require.Contains(t, live.PromptTemplates, "coder")
}

func TestWatcher_ParseFailurePreservesLastKnownGood(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "HARNESS.md", validHarnessYAML)

	def, err := Parse(path)
	require.NoError(t, err)

	w, writer := newTestWatcher(t, path, def)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Run(ctx) }()

	time.Sleep(50 * time.Millisecond)

	// Write malformed front matter (opening delimiter, no close).
	bad := "---\nproject:\n  id: foo\n## coder\nbody\n"
	require.NoError(t, os.WriteFile(path, []byte(bad), 0o644))

	ok := waitFor(2*time.Second, 25*time.Millisecond, func() bool {
		for _, e := range writer.snapshot() {
			if e.EventType == string(eventConfigReloadFailed) {
				return true
			}
		}
		return false
	})
	require.True(t, ok, "expected ConfigReloadFailed event, got %+v", writer.snapshot())

	// Live definition unchanged.
	live := w.Live()
	require.NotNil(t, live)
	project, _ := live.FrontMatter["project"].(map[string]any)
	require.Equal(t, "demo", project["id"])

	// Payload carries an error_code derived from the harness sentinel.
	var failed AuditEvent
	for _, e := range writer.snapshot() {
		if e.EventType == string(eventConfigReloadFailed) {
			failed = e
			break
		}
	}
	require.Equal(t, "harness_parse_error", failed.Payload["error_code"])
}

func TestWatcher_ValidationFailurePreservesLastKnownGood(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "HARNESS.md", validHarnessYAML)

	def, err := Parse(path)
	require.NoError(t, err)

	w, writer := newTestWatcher(t, path, def)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Run(ctx) }()
	time.Sleep(50 * time.Millisecond)

	// Rewrite the file so it parses but pipeline mentions a missing role.
	body := `---
project:
  id: demo
tracker:
  kind: linear
  api_key: secret
  project_slug: team
providers:
  default:
    provider: anthropic
    api_key: ank
routing:
  pipeline: [planner, coder, reviewer]
---

## planner

p

## coder

c
`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))

	ok := waitFor(2*time.Second, 25*time.Millisecond, func() bool {
		for _, e := range writer.snapshot() {
			if e.EventType == string(eventConfigReloadFailed) {
				return true
			}
		}
		return false
	})
	require.True(t, ok, "expected ConfigReloadFailed event")
	require.NotNil(t, w.Live())
}

func TestWatcher_BurstyWritesCoalesce(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "HARNESS.md", validHarnessYAML)

	def, err := Parse(path)
	require.NoError(t, err)

	w, writer := newTestWatcher(t, path, def)
	w.debounce = 80 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = w.Run(ctx) }()
	time.Sleep(50 * time.Millisecond)

	writer.reset()

	// Five quick rewrites within < debounce.
	for i := 0; i < 5; i++ {
		require.NoError(t, os.WriteFile(path, []byte(validHarnessYAML), 0o644))
		time.Sleep(10 * time.Millisecond)
	}
	// Wait long enough for the debounce to fire once.
	time.Sleep(300 * time.Millisecond)

	events := writer.snapshot()
	// At most one reload event for this burst.
	count := 0
	for _, e := range events {
		if e.EventType == string(eventConfigReloaded) {
			count++
		}
	}
	require.LessOrEqual(t, count, 1, "expected at most 1 ConfigReloaded across coalesced burst, got %d (events=%v)", count, events)
}

func TestWatcher_NilSafety(t *testing.T) {
	var w *Watcher
	require.Nil(t, w.Live())
	require.Equal(t, "", w.Path())
	err := w.Run(context.Background())
	require.Error(t, err)
}

func TestWatcher_NoWriterFallsBackToNop(t *testing.T) {
	cfg := validCfg()
	w := NewWatcher("HARNESS.md", validDef(), cfg, nil, zerolog.Nop())
	require.NotNil(t, w)
	// reloadOnce on a nonexistent path should produce a ConfigReloadFailed
	// classification but not panic. We just confirm no panic happens.
	w.reloadOnce(context.Background())
}

func TestClassifyAuditErrorCode(t *testing.T) {
	require.Equal(t, "missing_harness_file", classifyAuditErrorCode(ErrMissingHarnessFile))
	require.Equal(t, "harness_parse_error", classifyAuditErrorCode(ErrHarnessParse))
	require.Equal(t, "template_render_error", classifyAuditErrorCode(ErrTemplateRender))
	require.Equal(t, "harness_reload_failed", classifyAuditErrorCode(errors.New("nope")))
}

func TestWatcher_RunRequiresPath(t *testing.T) {
	w := NewWatcher("", nil, config.Config{}, nil, zerolog.Nop())
	require.Error(t, w.Run(context.Background()))
}
