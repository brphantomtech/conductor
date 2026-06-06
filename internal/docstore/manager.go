package docstore

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/conductor-sh/conductor/internal/audit"
	"github.com/conductor-sh/conductor/internal/config"
	"github.com/conductor-sh/conductor/internal/knowledge"
)

// defaultSyncInterval is the fallback resync cadence when a store omits
// sync_interval_minutes.
const defaultSyncInterval = 30 * time.Minute

// docContentLimit caps the content snippet stored on a `doc` node and embedded,
// mirroring the Knowledge Engine's per-node budget.
const docContentLimit = 4000

// storeState tracks the last successfully synced document set and sync time for
// one store, supporting checksum diffing and last-good retention.
type storeState struct {
	backend      Backend
	cfg          config.DocStoreConfig
	refs         map[string]DocRef // path_or_id → last-good DocRef
	lastSyncedAt time.Time
}

// Manager is the Doc Store Manager (SPEC §10): it owns the configured backends,
// runs the checksum-based sync scheduler, indexes changed documents into the
// Knowledge Engine, and supplies the orchestrator's doc-store-sync poll-loop
// seam. It is constructed once via New and is safe for concurrent use.
type Manager struct {
	cfg       config.Docs
	projectID string
	store     knowledgeStore
	embedder  docEmbedder
	audit     *audit.Writer
	clock     func() time.Time
	log       zerolog.Logger

	mu     sync.Mutex
	stores map[string]*storeState // store_id → state
}

// Option configures a Manager at construction.
type Option func(*Manager)

// WithKnowledgeStore injects the Knowledge Engine store used to index `doc`
// nodes. Required for documents to become searchable.
func WithKnowledgeStore(s knowledgeStore) Option {
	return func(m *Manager) {
		if s != nil {
			m.store = s
		}
	}
}

// WithEmbedder injects the embedder used to vectorize document bodies. A nil
// embedder leaves embeddings empty (structural search still works).
func WithEmbedder(e docEmbedder) Option {
	return func(m *Manager) {
		if e != nil {
			m.embedder = e
		}
	}
}

// WithAudit wires the audit writer for the DocStoreSynced event.
func WithAudit(w *audit.Writer) Option { return func(m *Manager) { m.audit = w } }

// WithClock overrides the time source. Tests inject a controllable clock.
func WithClock(fn func() time.Time) Option {
	return func(m *Manager) {
		if fn != nil {
			m.clock = fn
		}
	}
}

// WithLogger sets the structured logger.
func WithLogger(l zerolog.Logger) Option { return func(m *Manager) { m.log = l } }

// WithProjectID sets the project ID used in node IDs and audit payloads.
func WithProjectID(id string) Option {
	return func(m *Manager) {
		if id != "" {
			m.projectID = id
		}
	}
}

// WithBackend registers a pre-constructed backend for a store id, overriding any
// backend derived from config. Tests use this to inject fakes for git_repo/s3
// without exercising the wiring; the start command uses it for injected clients.
func WithBackend(storeID string, b Backend, cfg config.DocStoreConfig) Option {
	return func(m *Manager) {
		if b == nil {
			return
		}
		m.stores[storeID] = &storeState{backend: b, cfg: cfg, refs: map[string]DocRef{}}
	}
}

// New constructs a Manager. Backends declared in cfg.Stores that can be built
// without injected dependencies (local_fs) are constructed automatically;
// git_repo and s3 stores require their runner/client supplied via WithBackend.
func New(cfg config.Docs, projectID string, opts ...Option) (*Manager, error) {
	m := &Manager{
		cfg:       cfg,
		projectID: projectID,
		clock:     time.Now,
		log:       zerolog.Nop(),
		stores:    map[string]*storeState{},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(m)
		}
	}
	for _, sc := range cfg.Stores {
		if _, ok := m.stores[sc.ID]; ok {
			continue // already supplied via WithBackend
		}
		b, err := buildBackend(sc)
		if err != nil {
			return nil, err
		}
		if b == nil {
			// git_repo/s3 without an injected backend are skipped (logged) so the
			// manager still runs the stores it can build.
			m.log.Warn().Str("store_id", sc.ID).Str("backend", sc.Backend).
				Msg("docstore: store needs an injected client/runner; skipping")
			continue
		}
		m.stores[sc.ID] = &storeState{backend: b, cfg: sc, refs: map[string]DocRef{}}
	}
	return m, nil
}

// SyncAll syncs every configured store once, returning the joined error of any
// store failures. A failed store retains its last-good document set.
func (m *Manager) SyncAll(ctx context.Context) error {
	m.mu.Lock()
	ids := make([]string, 0, len(m.stores))
	for id := range m.stores {
		ids = append(ids, id)
	}
	m.mu.Unlock()

	var errs []error
	for _, id := range ids {
		if err := m.SyncStore(ctx, id); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// SyncPending implements the orchestrator's DocStoreSync seam (poll-loop step
// 4): it syncs each store whose interval has elapsed since its last sync. A
// per-store failure is logged and the loop continues (background-worker
// convention).
func (m *Manager) SyncPending(ctx context.Context) error {
	now := m.clock()
	m.mu.Lock()
	due := make([]string, 0, len(m.stores))
	for id, st := range m.stores {
		if st.lastSyncedAt.IsZero() || now.Sub(st.lastSyncedAt) >= interval(st.cfg) {
			due = append(due, id)
		}
	}
	m.mu.Unlock()

	for _, id := range due {
		if err := m.SyncStore(ctx, id); err != nil {
			m.log.Warn().Err(err).Str("store_id", id).Msg("docstore: pending sync failed; retaining last-good")
		}
	}
	return nil
}

// SyncStore syncs a single store: it lists the backend, diffs checksums against
// the last-good set, fetches and re-indexes only changed documents, removes
// deleted documents, emits a DocStoreSynced event, and on failure retains the
// prior set while classifying the error as ErrSyncFailed.
func (m *Manager) SyncStore(ctx context.Context, storeID string) error {
	m.mu.Lock()
	st, ok := m.stores[storeID]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("%w: unknown store %q", ErrSyncFailed, storeID)
	}

	refs, err := st.backend.Sync(ctx)
	if err != nil {
		return fmt.Errorf("docstore: sync %s: %w", storeID, errors.Join(ErrSyncFailed, err))
	}

	current := make(map[string]DocRef, len(refs))
	changed := make([]DocRef, 0)
	for _, r := range refs {
		current[r.PathOrID] = r
		prev, had := st.refs[r.PathOrID]
		if !had || prev.ContentHash != r.ContentHash {
			changed = append(changed, r)
		}
	}

	// Documents present last time but absent now were deleted.
	var deleted []DocRef
	for path, prev := range st.refs {
		if _, still := current[path]; !still {
			deleted = append(deleted, prev)
		}
	}

	now := m.clock().UTC()
	indexed, indexErr := m.indexChanged(ctx, st, changed, now)
	if indexErr != nil {
		// Retain the last-good set: do not overwrite st.refs on failure.
		return fmt.Errorf("docstore: index %s: %w", storeID, errors.Join(ErrSyncFailed, indexErr))
	}
	if err := m.removeDeleted(ctx, deleted); err != nil {
		return fmt.Errorf("docstore: remove %s: %w", storeID, errors.Join(ErrSyncFailed, err))
	}

	// Commit the new snapshot only after indexing succeeded.
	next := make(map[string]DocRef, len(current))
	for path, r := range current {
		r.LastSyncedAt = now
		next[path] = r
	}
	m.mu.Lock()
	st.refs = next
	st.lastSyncedAt = now
	m.mu.Unlock()

	m.emitSynced(ctx, storeID, len(refs), len(indexed), len(deleted))
	m.log.Info().
		Str("store_id", storeID).
		Int("documents", len(refs)).
		Int("changed", len(indexed)).
		Int("deleted", len(deleted)).
		Msg("docstore: store synced")
	return nil
}

// indexChanged fetches and upserts changed documents as `doc` nodes, returning
// the DocRefs successfully indexed.
func (m *Manager) indexChanged(
	ctx context.Context, st *storeState, changed []DocRef, now time.Time,
) ([]DocRef, error) {
	if len(changed) == 0 {
		return nil, nil
	}
	indexed := make([]DocRef, 0, len(changed))
	nodes := make([]knowledge.Node, 0, len(changed))
	for i := range changed {
		ref := changed[i]
		content, err := st.backend.Fetch(ctx, ref)
		if err != nil {
			return nil, fmt.Errorf("fetch %s: %w", ref.PathOrID, err)
		}
		node, err := m.docNode(ctx, ref, content, now)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
		indexed = append(indexed, ref)
	}
	if m.store == nil {
		return indexed, nil
	}
	if err := m.store.Upsert(ctx, nodes); err != nil {
		return nil, fmt.Errorf("upsert doc nodes: %w", err)
	}
	return indexed, nil
}

// docNode builds a `doc`-type knowledge node from a document.
func (m *Manager) docNode(ctx context.Context, ref DocRef, content string, now time.Time) (knowledge.Node, error) {
	snippet := truncate(content, docContentLimit)
	n := knowledge.Node{
		ID:            knowledge.NodeID(m.projectID, m.docPath(ref), ""),
		Type:          knowledge.NodeDoc,
		ProjectID:     m.projectID,
		Path:          m.docPath(ref),
		Name:          ref.Title,
		Summary:       firstLine(content),
		Content:       snippet,
		LastIndexedAt: now,
		Checksum:      ref.ContentHash,
	}
	if m.embedder != nil {
		emb, err := m.embedder.Embed(ctx, ref.Title+"\n"+snippet)
		if err != nil {
			return knowledge.Node{}, fmt.Errorf("embed %s: %w", ref.PathOrID, err)
		}
		n.Embedding = emb
	}
	return n, nil
}

// docPath returns the workspace-relative node path for a document so doc nodes
// share a stable namespace ("docs/<store_id>/<path_or_id>").
func (m *Manager) docPath(ref DocRef) string {
	return "docs/" + ref.StoreID + "/" + ref.PathOrID
}

// removeDeleted deletes the `doc` nodes of documents removed from the store.
func (m *Manager) removeDeleted(ctx context.Context, deleted []DocRef) error {
	if m.store == nil {
		return nil
	}
	for _, ref := range deleted {
		if err := m.store.DeleteByPath(ctx, m.projectID, m.docPath(ref)); err != nil {
			return fmt.Errorf("delete doc node %s: %w", ref.PathOrID, err)
		}
	}
	return nil
}

// emitSynced writes the DocStoreSynced audit event (SPEC §17.2).
func (m *Manager) emitSynced(ctx context.Context, storeID string, total, changed, deleted int) {
	if m.audit == nil {
		return
	}
	if err := m.audit.Write(ctx, audit.AuditEvent{
		ProjectID: m.projectID,
		EventType: audit.EventDocStoreSynced,
		Payload: map[string]any{
			"store_id":       storeID,
			"document_count": total,
			"changed_count":  changed,
			"deleted_count":  deleted,
		},
	}); err != nil {
		m.log.Error().Err(err).Str("store_id", storeID).Msg("audit write failed for DocStoreSynced")
	}
}

// Start runs the scheduler until ctx is cancelled: an initial SyncAll on
// startup (skipping stores synced within their interval) followed by periodic
// SyncPending ticks. It is the background-worker entry point; per-store failures
// are logged, never returned.
func (m *Manager) Start(ctx context.Context) error {
	if err := m.SyncPending(ctx); err != nil {
		m.log.Warn().Err(err).Msg("docstore: initial sync had failures")
	}
	ticker := time.NewTicker(m.tickInterval())
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("docstore: scheduler stopped: %w", ctx.Err())
		case <-ticker.C:
			if err := m.SyncPending(ctx); err != nil {
				m.log.Warn().Err(err).Msg("docstore: scheduled sync had failures")
			}
		}
	}
}

// tickInterval returns the smallest configured store interval so SyncPending is
// evaluated often enough to honor every store's cadence.
func (m *Manager) tickInterval() time.Duration {
	smallest := defaultSyncInterval
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, st := range m.stores {
		if iv := interval(st.cfg); iv < smallest {
			smallest = iv
		}
	}
	return smallest
}

// interval returns the configured resync interval for a store or the default.
func interval(cfg config.DocStoreConfig) time.Duration {
	if cfg.SyncIntervalMinutes > 0 {
		return time.Duration(cfg.SyncIntervalMinutes) * time.Minute
	}
	return defaultSyncInterval
}

// truncate clips s to at most n bytes on a rune boundary.
func truncate(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n]
}

// firstLine returns the trimmed first non-empty line of s as a short summary.
func firstLine(s string) string {
	for len(s) > 0 {
		i := indexByte(s, '\n')
		var line string
		if i < 0 {
			line, s = s, ""
		} else {
			line, s = s[:i], s[i+1:]
		}
		if t := trimSpace(line); t != "" {
			return t
		}
	}
	return ""
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func trimSpace(s string) string {
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r' || s[start] == '#') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}

var _ interface {
	SyncPending(context.Context) error
} = (*Manager)(nil)
