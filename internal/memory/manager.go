package memory

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/rs/zerolog"

	"github.com/conductor-sh/conductor/internal/audit"
	"github.com/conductor-sh/conductor/internal/config"
	"github.com/conductor-sh/conductor/internal/db"
)

// auditWriter is the small slice of *audit.Writer the manager depends on.
// Defining it here keeps the manager testable with a recording fake and
// matches the consumer-defined-interface convention.
type auditWriter interface {
	Write(ctx context.Context, evt audit.AuditEvent) error
}

// Manager is the three-layer Memory Manager (SPEC §9). It owns the store,
// retrieval and formatting, the three write paths, TTL enforcement, and the
// consolidation worker. All time-dependent behavior is driven by an injected
// clock so TTL and consolidation cadence are deterministic in tests.
type Manager struct {
	cfg    config.Memory
	store  MemoryStore
	embed  Embedder
	synth  Synthesizer
	audit  auditWriter
	log    zerolog.Logger
	clock  func() time.Time
	projID string
}

// options collects the manager's injectable dependencies.
type options struct {
	store  MemoryStore
	embed  Embedder
	synth  Synthesizer
	audit  auditWriter
	logger zerolog.Logger
	clock  func() time.Time
	db     *db.DB
	projID string
}

// Option is the functional-options handle for Manager construction.
type Option func(*options)

// WithStore injects a MemoryStore. When set, the manager does not open its
// own store from config. Tests use this to inject the in-memory fake.
func WithStore(s MemoryStore) Option { return func(o *options) { o.store = s } }

// WithEmbedder injects the similarity-vector source.
func WithEmbedder(e Embedder) Option { return func(o *options) { o.embed = e } }

// WithSynthesizer injects the consolidation synthesizer.
func WithSynthesizer(s Synthesizer) Option { return func(o *options) { o.synth = s } }

// WithAudit injects the audit writer used to emit memory events.
func WithAudit(w *audit.Writer) Option {
	return func(o *options) {
		if w != nil {
			o.audit = w
		}
	}
}

// withAuditWriter injects an auditWriter directly. Used by tests to record
// emitted events without standing up a full *audit.Writer with sinks.
func withAuditWriter(w auditWriter) Option {
	return func(o *options) {
		if w != nil {
			o.audit = w
		}
	}
}

// WithLogger overrides the default no-op logger.
func WithLogger(l zerolog.Logger) Option { return func(o *options) { o.logger = l } }

// WithClock overrides time.Now. Tests use a fixed/steppable clock so TTL and
// consolidation cadence assertions are exact.
func WithClock(f func() time.Time) Option {
	return func(o *options) {
		if f != nil {
			o.clock = f
		}
	}
}

// WithDB reuses an already-open database for the store backend instead of
// opening one from config. The caller retains ownership of the pool.
func WithDB(d *db.DB) Option { return func(o *options) { o.db = d } }

// WithProjectID sets the project scope applied when the caller does not
// supply one explicitly (e.g. the post-processor and consolidation worker).
func WithProjectID(id string) Option { return func(o *options) { o.projID = id } }

// New constructs a Manager from config plus options. When no store is
// injected it opens a SQLite store from cfg.StorePath (or the injected DB).
// A nil Embedder degrades semantic retrieval to recency only; a nil
// Synthesizer disables semantic consolidation. New never starts the
// consolidation worker — callers invoke Start for that.
func New(ctx context.Context, cfg config.Memory, opts ...Option) (*Manager, error) {
	o := options{
		logger: zerolog.Nop(),
		clock:  time.Now,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}

	store := o.store
	if store == nil {
		var err error
		switch {
		case o.db != nil:
			store, err = newSQLiteStoreFromDB(ctx, o.db)
		default:
			store, err = newSQLiteStore(ctx, cfg.StorePath)
		}
		if err != nil {
			return nil, err
		}
	}

	return &Manager{
		cfg:    withMemoryDefaults(cfg),
		store:  store,
		embed:  o.embed,
		synth:  o.synth,
		audit:  o.audit,
		log:    o.logger.With().Str("subsystem", "memory").Logger(),
		clock:  o.clock,
		projID: o.projID,
	}, nil
}

// withMemoryDefaults fills the SPEC §5.3.9 defaults for fields left zero.
func withMemoryDefaults(cfg config.Memory) config.Memory {
	if cfg.EpisodicTTLDays == 0 {
		cfg.EpisodicTTLDays = 90
	}
	if cfg.MaxContextMemories == 0 {
		cfg.MaxContextMemories = 7
	}
	if cfg.ConsolidationIntervalHours == 0 {
		cfg.ConsolidationIntervalHours = 24
	}
	return cfg
}

// Enabled reports whether the manager should perform real work. A disabled
// manager makes every public method a no-op so callers can wire it
// unconditionally (memory.enabled defaults to false; SPEC §5.3.9).
func (m *Manager) Enabled() bool { return m != nil && m.cfg.Enabled }

// Close releases the store if the manager owns it.
func (m *Manager) Close() error {
	if m == nil || m.store == nil {
		return nil
	}
	if err := m.store.Close(); err != nil {
		return fmt.Errorf("memory: close store: %w", err)
	}
	return nil
}

// newID returns a fresh 16-byte hex identifier, matching the provider/audit
// id convention.
func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("memory: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

// emit writes one memory audit event, swallowing sink errors (the audit
// writer logs them itself). A nil audit writer is a no-op.
func (m *Manager) emit(ctx context.Context, evt audit.AuditEvent) {
	if m.audit == nil {
		return
	}
	if evt.ProjectID == "" {
		evt.ProjectID = m.projID
	}
	if err := m.audit.Write(ctx, evt); err != nil {
		m.log.Warn().Err(err).Str("event_type", string(evt.EventType)).Msg("memory audit emit failed")
	}
}

// resolveProject returns the explicit project id when set, falling back to
// the manager's default scope.
func (m *Manager) resolveProject(projectID string) string {
	if projectID != "" {
		return projectID
	}
	return m.projID
}

// wrapWrite classifies any store/embedding failure on the write path as the
// SPEC §23.5 memory_write_failed sentinel.
func wrapWrite(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrWriteFailed, err)
}

// wrapRead classifies any store/embedding failure on the read path as the
// SPEC §23.5 memory_read_failed sentinel.
func wrapRead(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrReadFailed, err)
}
