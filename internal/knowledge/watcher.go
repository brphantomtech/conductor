package knowledge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog"
)

// DebounceWindow is the SPEC §8.3 incremental re-index coalescing window.
const DebounceWindow = 500 * time.Millisecond

// Watcher incrementally re-indexes workspace repo roots on filesystem changes
// (SPEC §8.3): Write/Create re-parse + re-embed + upsert; Remove deletes the
// file's nodes; events are batched within a 500 ms debounce window. It mirrors
// the Phase 2 harness watcher shape.
type Watcher struct {
	engine   *Engine
	roots    []string
	debounce time.Duration
	clock    func() time.Time
	log      zerolog.Logger

	fs *fsnotify.Watcher

	mu      sync.Mutex
	pending map[string]fsnotify.Op // rel-or-abs path → coalesced op

	stopOnce sync.Once
	started  bool
	stopped  chan struct{}
	done     chan struct{}
}

// WatchOption configures a Watcher.
type WatchOption func(*Watcher)

// WithWatchDebounce overrides the debounce window (tests use a small value).
func WithWatchDebounce(d time.Duration) WatchOption {
	return func(w *Watcher) {
		if d > 0 {
			w.debounce = d
		}
	}
}

// WithWatchClock overrides the watcher's time source.
func WithWatchClock(fn func() time.Time) WatchOption {
	return func(w *Watcher) {
		if fn != nil {
			w.clock = fn
		}
	}
}

// WithWatchLogger sets the watcher logger.
func WithWatchLogger(l zerolog.Logger) WatchOption { return func(w *Watcher) { w.log = l } }

// NewWatcher constructs a Watcher over the given repo roots. It does not start
// watching — call Start with a context controlling its lifetime.
func NewWatcher(e *Engine, roots []string, opts ...WatchOption) *Watcher {
	w := &Watcher{
		engine:   e,
		roots:    append([]string(nil), roots...),
		debounce: DebounceWindow,
		clock:    time.Now,
		log:      zerolog.Nop(),
		pending:  map[string]fsnotify.Op{},
		stopped:  make(chan struct{}),
		done:     make(chan struct{}),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(w)
		}
	}
	return w
}

// Start begins the watch loop. Safe to call once.
func (w *Watcher) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.started {
		w.mu.Unlock()
		return errors.New("knowledge: watcher already started")
	}
	fs, err := fsnotify.NewWatcher()
	if err != nil {
		w.mu.Unlock()
		return fmt.Errorf("knowledge: create fsnotify watcher: %w", err)
	}
	for _, root := range w.roots {
		if err := addRecursive(fs, root); err != nil {
			_ = fs.Close()
			w.mu.Unlock()
			return fmt.Errorf("knowledge: watch %q: %w", root, err)
		}
	}
	w.fs = fs
	w.started = true
	w.mu.Unlock()

	go w.run(ctx)
	return nil
}

// Close stops the watcher and releases OS resources. Idempotent.
func (w *Watcher) Close() error {
	var err error
	w.stopOnce.Do(func() {
		w.mu.Lock()
		started := w.started
		w.mu.Unlock()
		close(w.stopped)
		if started {
			<-w.done
		}
		if w.fs != nil {
			err = w.fs.Close()
		}
	})
	if err != nil {
		return fmt.Errorf("knowledge: close fsnotify: %w", err)
	}
	return nil
}

// run is the event loop: it coalesces events into the pending set and flushes
// them after the debounce window elapses.
func (w *Watcher) run(ctx context.Context) {
	defer close(w.done)

	var (
		timer *time.Timer
		tick  <-chan time.Time
	)
	reset := func() {
		if timer == nil {
			timer = time.NewTimer(w.debounce)
		} else {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(w.debounce)
		}
		tick = timer.C
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopped:
			return
		case ev, ok := <-w.fs.Events:
			if !ok {
				return
			}
			if w.record(ev) {
				reset()
			}
		case err, ok := <-w.fs.Errors:
			if !ok {
				return
			}
			w.log.Warn().Err(err).Msg("knowledge watcher fsnotify error")
		case <-tick:
			tick = nil
			w.flush(ctx)
		}
	}
}

// record folds an event into the pending set. It returns false for events that
// should not trigger a re-index (chmod-only, directory creation handled by
// adding a watch). New directories are added to the watch recursively.
func (w *Watcher) record(ev fsnotify.Event) bool {
	if ev.Op == fsnotify.Chmod {
		return false
	}
	if ev.Op&fsnotify.Create != 0 {
		if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
			_ = addRecursive(w.fs, ev.Name)
			return false
		}
	}
	w.mu.Lock()
	w.pending[ev.Name] = w.pending[ev.Name] | ev.Op
	w.mu.Unlock()
	return true
}

// flush drains the pending set and applies each change to the index.
func (w *Watcher) flush(ctx context.Context) {
	w.mu.Lock()
	pending := w.pending
	w.pending = map[string]fsnotify.Op{}
	w.mu.Unlock()

	for absPath, op := range pending {
		root := w.rootFor(absPath)
		if root == "" {
			continue
		}
		rel, err := filepath.Rel(root, absPath)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)

		if op&fsnotify.Remove != 0 || op&fsnotify.Rename != 0 {
			if _, statErr := os.Stat(absPath); statErr != nil {
				if err := w.engine.RemoveFile(ctx, rel); err != nil {
					w.log.Warn().Err(err).Str("path", rel).Msg("knowledge: remove file failed")
				}
				continue
			}
		}
		if err := w.engine.IndexFile(ctx, root, rel); err != nil {
			w.log.Warn().Err(err).Str("path", rel).Msg("knowledge: re-index file failed")
		}
	}
	w.engine.setStatus(StatusReady)
}

// rootFor returns the watched root that contains absPath, or "".
func (w *Watcher) rootFor(absPath string) string {
	for _, root := range w.roots {
		abs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(abs, absPath)
		if err == nil && !filepath.IsAbs(rel) && rel != ".." &&
			!hasDotDotPrefix(rel) {
			return root
		}
	}
	return ""
}

func hasDotDotPrefix(rel string) bool {
	return len(rel) >= 2 && rel[0] == '.' && rel[1] == '.'
}

// addRecursive registers root and all of its subdirectories with the watcher,
// skipping the default-excluded directories so the watch set stays bounded.
func addRecursive(fs *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		name := d.Name()
		if name == ".git" || name == "node_modules" || name == "vendor" || name == ".conductor" {
			return filepath.SkipDir
		}
		_ = fs.Add(p)
		return nil
	})
}
