package knowledge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/conductor-sh/conductor/internal/audit"
	"github.com/conductor-sh/conductor/internal/config"
)

// defaultTopK is the SPEC §8.4 default search result count.
const defaultTopK = 10

// contentSnippetLimit caps the raw content stored per node and embedded
// alongside the summary (SPEC §8.2 Stage 4 "truncated to the model limit").
// The value is a conservative character budget; the embedding model's true
// token limit is provider-specific and the embedder may truncate further.
const contentSnippetLimit = 4000

// IndexStatus mirrors the orchestrator's knowledge_index_status enum
// (SPEC §4.1.11) so the engine can report runtime state without importing the
// orchestrator package.
type IndexStatus string

// Knowledge index status values (SPEC §4.1.11).
const (
	StatusDisabled IndexStatus = "disabled"
	StatusIndexing IndexStatus = "indexing"
	StatusReady    IndexStatus = "ready"
	StatusStale    IndexStatus = "stale"
)

// Engine is the Knowledge Engine (SPEC §8): it indexes workspace repos into a
// persistent graph and serves hybrid search. It is constructed once via New and
// is safe for concurrent use.
type Engine struct {
	cfg        config.Knowledge
	projectID  string
	store      Store
	summarizer Summarizer
	embedder   Embedder
	audit      *audit.Writer
	clock      func() time.Time
	log        zerolog.Logger

	mu     sync.RWMutex
	status IndexStatus
}

// Option configures an Engine at construction.
type Option func(*Engine)

// WithStore injects the persistence backend. Required for indexing/search.
func WithStore(s Store) Option { return func(e *Engine) { e.store = s } }

// WithSummarizer injects the Stage 3 summarizer.
func WithSummarizer(s Summarizer) Option {
	return func(e *Engine) {
		if s != nil {
			e.summarizer = s
		}
	}
}

// WithEmbedder injects the Stage 4 embedder.
func WithEmbedder(em Embedder) Option {
	return func(e *Engine) {
		if em != nil {
			e.embedder = em
		}
	}
}

// WithAudit wires the audit writer for the KnowledgeIndexed event.
func WithAudit(w *audit.Writer) Option { return func(e *Engine) { e.audit = w } }

// WithClock overrides the time source. Tests inject a controllable clock.
func WithClock(fn func() time.Time) Option {
	return func(e *Engine) {
		if fn != nil {
			e.clock = fn
		}
	}
}

// WithLogger sets the structured logger.
func WithLogger(l zerolog.Logger) Option { return func(e *Engine) { e.log = l } }

// WithProjectID overrides the project ID used in node IDs and audit payloads.
func WithProjectID(id string) Option {
	return func(e *Engine) {
		if id != "" {
			e.projectID = id
		}
	}
}

// New constructs an Engine. A nil summarizer defaults to a static extractive
// summarizer (no provider calls); a nil embedder defaults to a deterministic
// hashing embedder so the engine is usable (and testable) without a live
// embedding provider.
func New(cfg config.Knowledge, projectID string, opts ...Option) *Engine {
	e := &Engine{
		cfg:        cfg,
		projectID:  projectID,
		summarizer: staticSummarizer{},
		embedder:   newHashEmbedder(hashEmbedDim),
		clock:      time.Now,
		log:        zerolog.Nop(),
		status:     StatusDisabled,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(e)
		}
	}
	if cfg.Enabled {
		e.setStatus(StatusStale)
	}
	return e
}

// Status returns the current index status (SPEC §4.1.11).
func (e *Engine) Status() IndexStatus {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.status
}

func (e *Engine) setStatus(s IndexStatus) {
	e.mu.Lock()
	e.status = s
	e.mu.Unlock()
}

// topK returns the configured top_k or the SPEC default.
func (e *Engine) topK() int {
	if e.cfg.TopK > 0 {
		return e.cfg.TopK
	}
	return defaultTopK
}

// Index runs the seven-stage pipeline (SPEC §8.2) over every root in roots,
// emits a KnowledgeIndexed audit event on success, and reports the resulting
// node count. A failure during any stage is classified as ErrIndexFailed.
func (e *Engine) Index(ctx context.Context, roots []string) (int, error) {
	if e.store == nil {
		return 0, fmt.Errorf("%w: no store configured", ErrIndexFailed)
	}
	e.setStatus(StatusIndexing)

	disc := newDiscoverer(e.cfg.IncludePatterns, e.cfg.ExcludePatterns)
	indexed := 0
	for _, root := range roots {
		files, err := disc.discover(root)
		if err != nil {
			e.setStatus(StatusStale)
			return indexed, wrapIndexFailed(err)
		}
		for i := range files {
			if err := ctx.Err(); err != nil {
				e.setStatus(StatusStale)
				return indexed, wrapIndexFailed(err)
			}
			n, err := e.indexFile(ctx, files[i])
			if err != nil {
				e.setStatus(StatusStale)
				return indexed, wrapIndexFailed(err)
			}
			indexed += n
		}
	}

	e.setStatus(StatusReady)
	e.emitIndexed(ctx, indexed)
	return indexed, nil
}

// wrapIndexFailed classifies an index error as ErrIndexFailed while preserving
// any sentinel (e.g. ErrEmbeddingRequestFailed) already in the chain, so both
// are reachable via errors.Is (SPEC §23.5).
func wrapIndexFailed(err error) error {
	if errors.Is(err, ErrIndexFailed) {
		return err
	}
	return fmt.Errorf("knowledge: index: %w", errors.Join(ErrIndexFailed, err))
}

// IndexFile re-indexes a single file at the slash-separated path rel under root
// (SPEC §8.3 Write/Create). It is the incremental-update entry point the watcher
// calls; failures are classified as ErrIndexFailed.
func (e *Engine) IndexFile(ctx context.Context, root, rel string) error {
	if e.store == nil {
		return fmt.Errorf("%w: no store configured", ErrIndexFailed)
	}
	abs := filepath.Join(root, filepath.FromSlash(rel))
	content, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Errorf("%w: read %s: %v", ErrIndexFailed, rel, err)
	}
	disc := newDiscoverer(e.cfg.IncludePatterns, e.cfg.ExcludePatterns)
	if disc.excluded(rel) || !disc.included(rel) {
		return nil
	}
	f := discoveredFile{AbsPath: abs, RelPath: rel, Checksum: Checksum(content), Content: content}
	if _, err := e.indexFile(ctx, f); err != nil {
		return err
	}
	return nil
}

// RemoveFile deletes a file's nodes and edges from the store (SPEC §8.3 Remove).
func (e *Engine) RemoveFile(ctx context.Context, rel string) error {
	if e.store == nil {
		return fmt.Errorf("%w: no store configured", ErrIndexFailed)
	}
	if err := e.store.DeleteByPath(ctx, e.projectID, rel); err != nil {
		return fmt.Errorf("%w: %v", ErrIndexFailed, err)
	}
	return nil
}

// indexFile runs stages 2–7 for a single discovered file and returns the number
// of nodes persisted. Unchanged files (matching the stored checksum on every
// produced node) are skipped without re-summarizing or re-embedding.
func (e *Engine) indexFile(ctx context.Context, f discoveredFile) (int, error) {
	cached, err := e.cachedByChecksum(ctx, f.RelPath, f.Checksum)
	if err != nil {
		return 0, err
	}

	pf := parserFor(f.RelPath, e.cfg.UseAST).parse(f.RelPath, f.Content)
	now := e.clock().UTC()

	nodes, err := e.buildNodes(ctx, f, pf, now, cached)
	if err != nil {
		return 0, err
	}
	if len(nodes) == 0 {
		return 0, nil
	}
	if err := e.store.Upsert(ctx, nodes); err != nil {
		return 0, err
	}
	return len(nodes), nil
}

// buildNodes turns parsed units into persisted nodes, assigning summaries,
// embeddings (reusing cache on an unchanged checksum), layer IDs, and edges.
func (e *Engine) buildNodes(
	ctx context.Context, f discoveredFile, pf parsedFile, now time.Time, cached map[string]Node,
) ([]Node, error) {
	layer := e.assignLayer(f.RelPath)
	nodes := make([]Node, 0, len(pf.Units)+1)

	// One file-level node carries the import edges; symbol/chunk nodes carry
	// their own content for retrieval.
	fileNode := Node{
		ID:            NodeID(e.projectID, f.RelPath, ""),
		Type:          NodeFile,
		ProjectID:     e.projectID,
		Path:          f.RelPath,
		Name:          baseName(f.RelPath),
		Language:      pf.Language,
		LayerID:       layer,
		LastIndexedAt: now,
		Checksum:      f.Checksum,
		OutgoingEdges: e.importEdges(f.RelPath, pf.Imports),
	}
	if err := e.enrich(ctx, &fileNode, fileSnippet(f.Content), cached); err != nil {
		return nil, err
	}
	nodes = append(nodes, fileNode)

	for _, u := range pf.Units {
		if u.Kind == NodeFile {
			continue
		}
		n := Node{
			ID:            NodeID(e.projectID, f.RelPath, u.Name),
			Type:          u.Kind,
			ProjectID:     e.projectID,
			Path:          f.RelPath,
			Name:          unitName(u, f.RelPath),
			Language:      pf.Language,
			LayerID:       layer,
			LineStart:     u.LineStart,
			LineEnd:       u.LineEnd,
			LastIndexedAt: now,
			Checksum:      f.Checksum,
		}
		if err := e.enrich(ctx, &n, u.Content, cached); err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	return nodes, nil
}

// enrich fills a node's Content, Summary, and Embedding. When a cached node with
// the same ID and checksum exists, its summary and embedding are reused and no
// provider calls are made (SPEC §8.2 Stage 3 caching, §8.3 incremental).
func (e *Engine) enrich(ctx context.Context, n *Node, content string, cached map[string]Node) error {
	n.Content = truncate(content, contentSnippetLimit)

	if prev, ok := cached[n.ID]; ok && prev.Checksum == n.Checksum && len(prev.Embedding) > 0 {
		n.Summary = prev.Summary
		n.Embedding = prev.Embedding
		return nil
	}

	summary, err := e.summarizer.Summarize(ctx, n.Path, n.Content)
	if err != nil {
		// Summaries are best-effort: a summarizer failure degrades to an empty
		// summary rather than failing the whole index. Embedding failures, by
		// contrast, are classified (SPEC §23.5).
		e.log.Warn().Err(err).Str("path", n.Path).Msg("summarization failed; continuing")
		summary = ""
	}
	n.Summary = summary

	emb, err := e.embedder.Embed(ctx, n.Summary+"\n"+n.Content)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrEmbeddingRequestFailed, err)
	}
	n.Embedding = emb
	return nil
}

// cachedByChecksum loads the currently stored nodes for path keyed by node ID so
// enrich can reuse summaries/embeddings when the checksum is unchanged.
func (e *Engine) cachedByChecksum(ctx context.Context, path, _ string) (map[string]Node, error) {
	existing, err := e.store.QueryStructural(ctx, Filter{PathPattern: path}, 0)
	if err != nil {
		return nil, err
	}
	out := make(map[string]Node, len(existing))
	for _, n := range existing {
		if n.Path == path {
			out[n.ID] = n
		}
	}
	return out, nil
}

// importEdges builds directed import edges from a file node to the resolved
// targets. Targets that resolve to no in-project node still produce an edge to
// the would-be node ID so dependency queries are stable once the target is
// indexed.
func (e *Engine) importEdges(relPath string, imports []string) []Edge {
	if len(imports) == 0 {
		return nil
	}
	from := NodeID(e.projectID, relPath, "")
	edges := make([]Edge, 0, len(imports))
	for _, imp := range imports {
		target := resolveImportPath(imp)
		if target == "" {
			continue
		}
		edges = append(edges, Edge{
			FromID:   from,
			ToID:     NodeID(e.projectID, target, ""),
			EdgeType: EdgeImports,
		})
	}
	return edges
}

// emitIndexed writes the KnowledgeIndexed audit event (SPEC §17.2).
func (e *Engine) emitIndexed(ctx context.Context, nodeCount int) {
	if e.audit == nil {
		return
	}
	if err := e.audit.Write(ctx, audit.AuditEvent{
		ProjectID: e.projectID,
		EventType: audit.EventKnowledgeIndexed,
		Payload: map[string]any{
			"node_count":    nodeCount,
			"store_backend": e.backendName(),
		},
	}); err != nil {
		e.log.Error().Err(err).Msg("audit write failed for KnowledgeIndexed")
	}
}

func (e *Engine) backendName() string {
	if e.cfg.StoreBackend != "" {
		return e.cfg.StoreBackend
	}
	return StoreBackendSQLiteVec
}
