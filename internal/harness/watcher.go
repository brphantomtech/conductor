package harness

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

	"github.com/conductor-sh/conductor/internal/audit"
	"github.com/conductor-sh/conductor/internal/config"
)

// SPEC §6.3 — Dynamic Reload.
//
// Watcher tails HARNESS.md and atomically swaps the in-memory LoadResult
// whenever a successful reload completes. Failed reloads (parse error,
// validation error, $VAR expansion failure) leave the previous good
// LoadResult in place and emit a ConfigReloadFailed audit event.

// DefaultDebounce is the SPEC §6.3 default reload coalescing window.
// Editors often emit a flurry of write/rename/chmod events when saving;
// debouncing lets the watcher absorb that burst into a single reload.
const DefaultDebounce = 250 * time.Millisecond

// WatchOptions configures NewWatcher. The zero value is not usable: Path
// is required.
type WatchOptions struct {
	// Path is the absolute or workspace-relative HARNESS.md location.
	Path string

	// LoadOpts is forwarded to config.Load on every reload. The watcher
	// overwrites LoadOpts.FrontMatter from the parsed Definition before
	// each load, so the caller's value (if any) is ignored.
	LoadOpts config.LoadOptions

	// Audit is the writer that receives ConfigReloaded /
	// ConfigReloadFailed events. nil disables audit emission.
	Audit *audit.Writer

	// Debounce coalesces rapid event bursts. Zero falls back to
	// DefaultDebounce.
	Debounce time.Duration

	// Logger is used for structured logs around reload outcomes. The
	// zerolog zero value (a disabled logger) is acceptable.
	Logger zerolog.Logger

	// OnUpdate fires once per successful reload, on the watcher's
	// goroutine. It is invoked AFTER the in-memory LoadResult has been
	// swapped, so a callback that calls Current sees the new value. nil
	// disables the callback.
	OnUpdate func(LoadResult)
}

// Watcher hot-reloads HARNESS.md. The orchestrator constructs one at
// startup, calls Start, and reads Current under its hot path.
type Watcher struct {
	opts     WatchOptions
	fs       *fsnotify.Watcher
	dir      string
	filename string

	mu      sync.RWMutex
	current LoadResult

	stopOnce sync.Once
	started  bool
	stopped  chan struct{}
	done     chan struct{}
}

// NewWatcher performs the initial load and prepares an fsnotify subscription
// on the parent directory of opts.Path. It does NOT start the watch loop —
// call Start once you have a context that controls the watcher's lifetime.
//
// NewWatcher returns the initial load error verbatim so callers can decide
// whether to abort startup (SPEC §6.4 hard-fail) or fall back. The returned
// Watcher is non-nil whenever the initial parse and config-decode succeeded;
// validation issues alone leave Current populated and Start callable.
func NewWatcher(opts WatchOptions) (*Watcher, error) {
	if opts.Path == "" {
		return nil, errors.New("harness: watcher requires a non-empty path")
	}
	abs, err := filepath.Abs(opts.Path)
	if err != nil {
		return nil, fmt.Errorf("harness: resolve watch path %q: %w", opts.Path, err)
	}
	opts.Path = abs

	if opts.Debounce <= 0 {
		opts.Debounce = DefaultDebounce
	}

	res, loadErr := Load(opts.Path, opts.LoadOpts)
	if loadErr != nil && res.Definition == nil {
		// Parse-level failure: cannot even produce a LoadResult.
		return nil, loadErr
	}

	w := &Watcher{
		opts:     opts,
		dir:      filepath.Dir(opts.Path),
		filename: filepath.Base(opts.Path),
		current:  res,
		stopped:  make(chan struct{}),
		done:     make(chan struct{}),
	}
	// Surface the (non-fatal) validation error so the caller can decide
	// whether to abort. Returning both lets the CLI's `validate`
	// subcommand display the LoadResult while still reporting non-zero.
	return w, loadErr
}

// Start begins the watch loop. It returns once the underlying fsnotify
// watcher has been created and the watch goroutine is running. Start is
// safe to call exactly once; subsequent calls return an error.
func (w *Watcher) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.started {
		w.mu.Unlock()
		return errors.New("harness: watcher already started")
	}
	fs, err := fsnotify.NewWatcher()
	if err != nil {
		w.mu.Unlock()
		return fmt.Errorf("harness: create fsnotify watcher: %w", err)
	}
	if aerr := fs.Add(w.dir); aerr != nil {
		_ = fs.Close()
		w.mu.Unlock()
		return fmt.Errorf("harness: watch dir %q: %w", w.dir, aerr)
	}
	w.fs = fs
	w.started = true
	w.mu.Unlock()

	go w.run(ctx)
	return nil
}

// Current returns the most recent successful LoadResult, or the result
// captured at NewWatcher time if no reload has succeeded since.
func (w *Watcher) Current() LoadResult {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.current
}

// Close stops the watcher and releases its OS resources. It is safe to
// call multiple times, and safe to call when Start was never invoked.
func (w *Watcher) Close() error {
	var err error
	w.stopOnce.Do(func() {
		w.mu.RLock()
		started := w.started
		w.mu.RUnlock()

		close(w.stopped)
		if started {
			<-w.done
		}
		if w.fs != nil {
			err = w.fs.Close()
		}
	})
	if err != nil {
		return fmt.Errorf("harness: close fsnotify: %w", err)
	}
	return nil
}

// run is the watcher's event loop. It runs until ctx is cancelled, Close
// is called, or fsnotify returns a fatal error on its event channel.
func (w *Watcher) run(ctx context.Context) {
	defer close(w.done)

	var (
		debounceTimer *time.Timer
		debounceC     <-chan time.Time
	)
	resetDebounce := func() {
		if debounceTimer == nil {
			debounceTimer = time.NewTimer(w.opts.Debounce)
		} else {
			if !debounceTimer.Stop() {
				select {
				case <-debounceTimer.C:
				default:
				}
			}
			debounceTimer.Reset(w.opts.Debounce)
		}
		debounceC = debounceTimer.C
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
			if !w.eventConcernsHarness(ev) {
				continue
			}
			resetDebounce()
		case err, ok := <-w.fs.Errors:
			if !ok {
				return
			}
			w.opts.Logger.Warn().Err(err).Msg("fsnotify reported an error")
		case <-debounceC:
			debounceC = nil
			w.reload(ctx)
		}
	}
}

// eventConcernsHarness reports whether ev names the watched HARNESS.md.
// Watching the parent directory is safer than watching the file directly
// (atomic-replace saves on Windows / Linux drop the file watch); we filter
// here so unrelated files in the same directory don't trigger reloads.
func (w *Watcher) eventConcernsHarness(ev fsnotify.Event) bool {
	if filepath.Base(ev.Name) != w.filename {
		return false
	}
	// Chmod-only events are noise on some filesystems and never reflect
	// content changes; ignore them to keep the audit log quiet.
	if ev.Op == fsnotify.Chmod {
		return false
	}
	return true
}

// reload re-runs Load, swaps the result on success, and emits the
// appropriate audit event. Validation failures emit ConfigReloadFailed but
// keep the last known good result in place.
func (w *Watcher) reload(ctx context.Context) {
	res, err := Load(w.opts.Path, w.opts.LoadOpts)

	if err != nil {
		w.emitFailure(ctx, err)
		return
	}

	w.mu.Lock()
	w.current = res
	w.mu.Unlock()

	w.emitSuccess(ctx, res)

	if w.opts.OnUpdate != nil {
		w.opts.OnUpdate(res)
	}
}

// emitSuccess writes a ConfigReloaded audit event and a structured info
// log line. The payload mirrors the SPEC §17.2 event registry.
func (w *Watcher) emitSuccess(ctx context.Context, res LoadResult) {
	w.opts.Logger.Info().
		Str("path", w.opts.Path).
		Bool("validation_errors", res.Validation.HasErrors()).
		Msg("HARNESS.md reloaded")

	if w.opts.Audit == nil {
		return
	}
	if err := w.opts.Audit.Write(ctx, audit.AuditEvent{
		ProjectID: res.Config.Project.ID,
		EventType: audit.EventConfigReloaded,
		Payload: map[string]any{
			"path":              w.opts.Path,
			"validation_errors": len(res.Validation.Issues),
			"prompt_roles":      promptRoleList(res.Definition),
		},
	}); err != nil {
		w.opts.Logger.Error().Err(err).Msg("audit write failed")
	}
}

// emitFailure writes a ConfigReloadFailed audit event when a reload throws
// any error path — parse, decode, $VAR expansion, validation. The previous
// good LoadResult stays installed so the orchestrator keeps running with
// the last known good config.
func (w *Watcher) emitFailure(ctx context.Context, err error) {
	w.opts.Logger.Warn().
		Err(err).
		Str("path", w.opts.Path).
		Msg("HARNESS.md reload failed; keeping last known good config")

	if w.opts.Audit == nil {
		return
	}
	cur := w.Current()
	projectID := ""
	if cur.Definition != nil {
		projectID = cur.Config.Project.ID
	}
	if werr := w.opts.Audit.Write(ctx, audit.AuditEvent{
		ProjectID: projectID,
		EventType: audit.EventConfigReloadFailed,
		Payload: map[string]any{
			"path":  w.opts.Path,
			"error": err.Error(),
		},
	}); werr != nil {
		w.opts.Logger.Error().Err(werr).Msg("audit write failed")
	}
}

// promptRoleList returns the role names from a Definition in deterministic
// order so audit payloads are stable across runs.
func promptRoleList(def *Definition) []string {
	if def == nil {
		return nil
	}
	roles := make([]string, 0, len(def.PromptTemplates))
	for r := range def.PromptTemplates {
		roles = append(roles, r)
	}
	// Sort to keep audit payloads deterministic. We intentionally avoid
	// sort.Strings here so the package keeps a single sort dependency
	// surface; Go map iteration plus a manual insertion is fine for a
	// list that is never larger than the role registry.
	for i := 1; i < len(roles); i++ {
		for j := i; j > 0 && roles[j-1] > roles[j]; j-- {
			roles[j-1], roles[j] = roles[j], roles[j-1]
		}
	}
	return roles
}

// EnsureExists returns ErrMissingHarnessFile when path does not resolve to
// an existing regular file. Callers that want to fail fast at startup can
// use this before constructing a Watcher.
func EnsureExists(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%w: %s", ErrMissingHarnessFile, path)
		}
		return fmt.Errorf("harness: stat %q: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%w: %s is a directory, expected a file", ErrMissingHarnessFile, path)
	}
	return nil
}
