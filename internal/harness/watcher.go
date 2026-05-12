package harness

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog"

	"github.com/conductor-sh/conductor/internal/config"
)

// DefaultReloadDebounce is the SPEC §6.3 reload coalescing window. 250ms
// is short enough to feel snappy after a single save and long enough to
// swallow the "atomic save" rename-then-create patterns several editors
// emit on Windows.
const DefaultReloadDebounce = 250 * time.Millisecond

// auditEventType mirrors the audit package's EventType without importing
// it. We keep the strings in sync via tests rather than a Go import so
// the harness package stays a pure Tier-2 domain engine (audit is
// Tier 1; this file's events are emitted *through* an injected writer,
// not by reaching into the audit package directly).
type auditEventType string

const (
	eventConfigReloaded     auditEventType = "ConfigReloaded"
	eventConfigReloadFailed auditEventType = "ConfigReloadFailed"
)

// AuditEvent is the minimal payload the watcher emits. It is
// structurally compatible with `internal/audit`.AuditEvent so cmd-layer
// callers can wrap an audit.Writer once and forward straight through.
type AuditEvent struct {
	ProjectID string
	EventType string
	Payload   map[string]any
}

// AuditWriter is the consumer-defined interface (per
// docs/conventions.md §3.1) the watcher uses to publish reload audit
// events. The cmd layer adapts internal/audit.Writer to this shape.
type AuditWriter interface {
	Write(ctx context.Context, evt AuditEvent) error
}

// nopAuditWriter is the safe default when the caller does not inject a
// writer (tests that only care about live-pointer behavior).
type nopAuditWriter struct{}

func (nopAuditWriter) Write(_ context.Context, _ AuditEvent) error { return nil }

// Watcher is the fsnotify-backed reload pump. It owns:
//
//   - the path to HARNESS.md;
//   - an atomic pointer to the currently-active Definition;
//   - an AuditWriter that receives ConfigReloaded / ConfigReloadFailed
//     events;
//   - the merged Config snapshot used for revalidation.
//
// Downstream readers (orchestrator, router) call (*Watcher).Live() once
// per dispatch tick to get the freshest valid Definition without
// blocking. The pointer is only swapped after a successful parse +
// validate, so readers never observe an intermediate state.
type Watcher struct {
	path     string
	debounce time.Duration

	live   *atomic.Pointer[Definition]
	cfg    *atomic.Pointer[config.Config]
	writer AuditWriter
	log    zerolog.Logger
}

// NewWatcher constructs a Watcher with the SPEC §6.3 defaults. The
// `initial` Definition becomes the first "live" snapshot — typically the
// one returned by Load on startup. The `cfg` is the merged config
// snapshot validation will run against on reload; if reload-time config
// re-derivation is desired, callers can re-create the Watcher.
//
// writer may be nil; the watcher substitutes a nop sink so callers that
// do not want audit emissions can keep using it the same way.
func NewWatcher(path string, initial *Definition, cfg config.Config, writer AuditWriter, log zerolog.Logger) *Watcher {
	w := &Watcher{
		path:     path,
		debounce: DefaultReloadDebounce,
		live:     &atomic.Pointer[Definition]{},
		cfg:      &atomic.Pointer[config.Config]{},
		writer:   writer,
		log:      log.With().Str("subsystem", "harness").Logger(),
	}
	if writer == nil {
		w.writer = nopAuditWriter{}
	}
	if initial != nil {
		w.live.Store(initial)
	}
	cfgCopy := cfg
	w.cfg.Store(&cfgCopy)
	return w
}

// Live returns the active Definition, or nil if no successful load has
// happened yet. Safe to call concurrently with Run.
func (w *Watcher) Live() *Definition {
	if w == nil {
		return nil
	}
	return w.live.Load()
}

// Path returns the file the watcher is monitoring.
func (w *Watcher) Path() string {
	if w == nil {
		return ""
	}
	return w.path
}

// Run starts the fsnotify watch loop. It blocks until ctx is cancelled
// or the underlying fsnotify watcher errors. Returns nil on graceful
// shutdown via ctx; a non-nil return is always a setup error.
//
// Run watches the *directory* containing the harness file (not the file
// itself) because several editors implement atomic saves by writing a
// temp file and renaming it over the target — an inode-level rename
// drops a file-level watch.
func (w *Watcher) Run(ctx context.Context) error {
	if w == nil {
		return errors.New("harness: Run on nil Watcher")
	}
	if w.path == "" {
		return errors.New("harness: Watcher has empty path")
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("harness: fsnotify.NewWatcher: %w", err)
	}
	defer func() { _ = fsw.Close() }()

	dir := filepath.Dir(w.path)
	if err := fsw.Add(dir); err != nil {
		return fmt.Errorf("harness: fsnotify.Add(%s): %w", dir, err)
	}

	base := filepath.Base(w.path)
	var debounceTimer *time.Timer
	defer func() {
		if debounceTimer != nil {
			debounceTimer.Stop()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case err, ok := <-fsw.Errors:
			if !ok {
				return nil
			}
			w.log.Warn().Err(err).Msg("harness watcher error")
		case evt, ok := <-fsw.Events:
			if !ok {
				return nil
			}
			if filepath.Base(evt.Name) != base {
				continue
			}
			// Coalesce: reset the debounce window on every event.
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceTimer = time.AfterFunc(w.debounce, func() {
				w.reloadOnce(ctx)
			})
		}
	}
}

// reloadOnce performs one debounced reload: re-parse + re-validate.
// Success → swap the live pointer, emit ConfigReloaded. Failure → leave
// the live pointer untouched, emit ConfigReloadFailed. Either way, the
// audit event is best-effort — sink failures are logged, not propagated.
func (w *Watcher) reloadOnce(ctx context.Context) {
	def, err := Parse(w.path)
	if err != nil {
		w.emitFailed(ctx, err)
		return
	}

	cfgPtr := w.cfg.Load()
	var cfg config.Config
	if cfgPtr != nil {
		cfg = *cfgPtr
	}
	if err := Validate(def, cfg); err != nil {
		w.emitFailed(ctx, err)
		return
	}

	w.live.Store(def)
	w.emitReloaded(ctx, def)
}

func (w *Watcher) emitReloaded(ctx context.Context, def *Definition) {
	templates := make([]string, 0, len(def.PromptTemplates))
	for name := range def.PromptTemplates {
		templates = append(templates, name)
	}
	sort.Strings(templates)

	payload := map[string]any{
		"path":      w.path,
		"templates": templates,
	}
	if cfgPtr := w.cfg.Load(); cfgPtr != nil {
		payload["project_id"] = cfgPtr.Project.ID
	}

	evt := AuditEvent{
		EventType: string(eventConfigReloaded),
		Payload:   payload,
	}
	if cfgPtr := w.cfg.Load(); cfgPtr != nil {
		evt.ProjectID = cfgPtr.Project.ID
	}

	if err := w.writer.Write(ctx, evt); err != nil {
		w.log.Warn().Err(err).Msg("emit ConfigReloaded failed")
		return
	}
	w.log.Info().
		Str("event_type", evt.EventType).
		Strs("templates", templates).
		Str("path", w.path).
		Msg("harness reloaded")
}

func (w *Watcher) emitFailed(ctx context.Context, err error) {
	code := classifyAuditErrorCode(err)
	payload := map[string]any{
		"path":          w.path,
		"error_code":    code,
		"error_message": err.Error(),
	}
	evt := AuditEvent{
		EventType: string(eventConfigReloadFailed),
		Payload:   payload,
	}
	if cfgPtr := w.cfg.Load(); cfgPtr != nil {
		evt.ProjectID = cfgPtr.Project.ID
	}
	if wErr := w.writer.Write(ctx, evt); wErr != nil {
		w.log.Warn().Err(wErr).Msg("emit ConfigReloadFailed failed")
	}
	w.log.Warn().
		Err(err).
		Str("event_type", evt.EventType).
		Str("error_code", code).
		Str("path", w.path).
		Msg("harness reload failed; keeping last known good")
}

// classifyAuditErrorCode picks the SPEC §23 identifier closest to the
// underlying error. Falls back to "harness_reload_failed" for anything
// unrecognized so audit consumers always see a stable code.
func classifyAuditErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrMissingHarnessFile):
		return ErrMissingHarnessFile.Error()
	case errors.Is(err, ErrHarnessFrontMatterShape):
		return ErrHarnessFrontMatterShape.Error()
	case errors.Is(err, ErrHarnessParse):
		return ErrHarnessParse.Error()
	case errors.Is(err, ErrTemplateParse):
		return ErrTemplateParse.Error()
	case errors.Is(err, ErrTemplateRender):
		return ErrTemplateRender.Error()
	}
	return "harness_reload_failed"
}
