package knowledge

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/rs/zerolog"

	"github.com/conductor-sh/conductor/internal/db"
)

// StoreBackendSQLiteVec is the default store_backend identifier (SPEC §5.3.8).
const StoreBackendSQLiteVec = "sqlite_vec"

// sqliteStore is the default Store backend (SPEC §8.2 Stage 7). Node metadata
// lives in a relational table; embeddings live in a sibling table keyed by node
// ID.
//
// The reference target is the sqlite-vec extension's virtual vector table. The
// pure-Go modernc.org/sqlite driver Conductor uses (see internal/db) cannot
// load that native extension, so this backend stores embeddings as a packed
// float32 BLOB and performs brute-force cosine similarity in Go. The behavior
// (semantic ranking) is identical; only the index data structure differs. The
// fallback is logged once at construction. If a build is produced against a
// driver that does carry sqlite-vec, the virtual-table path can be added behind
// a build tag without changing the Store contract.
type sqliteStore struct {
	db  *db.DB
	log zerolog.Logger
}

// NewSQLiteStore opens (or creates) the sqlite_vec store at dsn and ensures the
// schema exists. An empty dsn opens an in-memory store (tests).
func NewSQLiteStore(ctx context.Context, dsn string, log zerolog.Logger) (Store, error) {
	d, err := db.Open(ctx, db.Options{Driver: db.DriverSQLite, DSN: dsn})
	if err != nil {
		return nil, fmt.Errorf("knowledge: open store %q: %w", dsn, err)
	}
	s := &sqliteStore{db: d, log: log.With().Str("subsystem", "knowledge-store").Logger()}
	if err := s.ensureSchema(ctx); err != nil {
		_ = d.Close()
		return nil, err
	}
	s.log.Warn().Msg("sqlite-vec extension unavailable with the pure-Go driver; " +
		"using brute-force cosine similarity for semantic search")
	return s, nil
}

func (s *sqliteStore) ensureSchema(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS knowledge_nodes (
			id              TEXT PRIMARY KEY,
			type            TEXT NOT NULL,
			project_id      TEXT NOT NULL,
			path            TEXT NOT NULL,
			name            TEXT NOT NULL,
			summary         TEXT NOT NULL DEFAULT '',
			content         TEXT NOT NULL DEFAULT '',
			embedding       BLOB,
			layer_id        TEXT NOT NULL DEFAULT '',
			language        TEXT NOT NULL DEFAULT '',
			line_start      INTEGER NOT NULL DEFAULT 0,
			line_end        INTEGER NOT NULL DEFAULT 0,
			last_indexed_at TEXT NOT NULL DEFAULT '',
			checksum        TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_knowledge_nodes_path
			ON knowledge_nodes (project_id, path)`,
		`CREATE TABLE IF NOT EXISTS knowledge_edges (
			from_id   TEXT NOT NULL,
			to_id     TEXT NOT NULL,
			edge_type TEXT NOT NULL,
			weight    REAL NOT NULL DEFAULT 0,
			PRIMARY KEY (from_id, to_id, edge_type)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_knowledge_edges_to
			ON knowledge_edges (to_id)`,
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(ctx, q); err != nil {
			return fmt.Errorf("knowledge: ensure schema: %w", err)
		}
	}
	return nil
}

// Upsert replaces nodes (and their outgoing edges) keyed by node ID.
func (s *sqliteStore) Upsert(ctx context.Context, nodes []Node) error {
	tx, err := s.db.SQL().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("knowledge: begin upsert: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for i := range nodes {
		n := &nodes[i]
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO knowledge_nodes
				(id, type, project_id, path, name, summary, content, embedding,
				 layer_id, language, line_start, line_end, last_indexed_at, checksum)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
				type=excluded.type, project_id=excluded.project_id, path=excluded.path,
				name=excluded.name, summary=excluded.summary, content=excluded.content,
				embedding=excluded.embedding, layer_id=excluded.layer_id,
				language=excluded.language, line_start=excluded.line_start,
				line_end=excluded.line_end, last_indexed_at=excluded.last_indexed_at,
				checksum=excluded.checksum`,
			n.ID, string(n.Type), n.ProjectID, n.Path, n.Name, n.Summary, n.Content,
			packEmbedding(n.Embedding), n.LayerID, n.Language, n.LineStart, n.LineEnd,
			n.LastIndexedAt.UTC().Format("2006-01-02T15:04:05.000Z07:00"), n.Checksum,
		); err != nil {
			return fmt.Errorf("knowledge: upsert node %s: %w", n.ID, err)
		}

		if _, err := tx.ExecContext(ctx,
			`DELETE FROM knowledge_edges WHERE from_id = ?`, n.ID); err != nil {
			return fmt.Errorf("knowledge: clear edges %s: %w", n.ID, err)
		}
		for _, e := range n.OutgoingEdges {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO knowledge_edges (from_id, to_id, edge_type, weight)
				VALUES (?, ?, ?, ?)
				ON CONFLICT(from_id, to_id, edge_type) DO UPDATE SET weight=excluded.weight`,
				e.FromID, e.ToID, string(e.EdgeType), e.Weight); err != nil {
				return fmt.Errorf("knowledge: upsert edge %s->%s: %w", e.FromID, e.ToID, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("knowledge: commit upsert: %w", err)
	}
	return nil
}

// DeleteByPath removes nodes at path plus all incident edges.
func (s *sqliteStore) DeleteByPath(ctx context.Context, projectID, path string) error {
	tx, err := s.db.SQL().BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("knowledge: begin delete: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.QueryContext(ctx,
		`SELECT id FROM knowledge_nodes WHERE project_id = ? AND path = ?`, projectID, path)
	if err != nil {
		return fmt.Errorf("knowledge: select for delete: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return fmt.Errorf("knowledge: scan delete id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("knowledge: iterate delete ids: %w", err)
	}
	_ = rows.Close()

	for _, id := range ids {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM knowledge_edges WHERE from_id = ? OR to_id = ?`, id, id); err != nil {
			return fmt.Errorf("knowledge: delete edges %s: %w", id, err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM knowledge_nodes WHERE project_id = ? AND path = ?`, projectID, path); err != nil {
		return fmt.Errorf("knowledge: delete nodes %s: %w", path, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("knowledge: commit delete: %w", err)
	}
	return nil
}

// SearchSemantic ranks the filtered candidate nodes by cosine similarity.
func (s *sqliteStore) SearchSemantic(
	ctx context.Context, query []float32, filter Filter, topK int,
) ([]ScoredNode, error) {
	nodes, err := s.loadNodes(ctx, filter)
	if err != nil {
		return nil, err
	}
	scored := make([]ScoredNode, 0, len(nodes))
	for i := range nodes {
		if len(nodes[i].Embedding) == 0 {
			continue
		}
		scored = append(scored, ScoredNode{Node: nodes[i], Score: cosine(query, nodes[i].Embedding)})
	}
	sort.SliceStable(scored, func(i, j int) bool { return scored[i].Score > scored[j].Score })
	if topK > 0 && len(scored) > topK {
		scored = scored[:topK]
	}
	return scored, nil
}

// QueryStructural returns nodes matching the filter.
func (s *sqliteStore) QueryStructural(ctx context.Context, filter Filter, topK int) ([]Node, error) {
	nodes, err := s.loadNodes(ctx, filter)
	if err != nil {
		return nil, err
	}
	if topK > 0 && len(nodes) > topK {
		nodes = nodes[:topK]
	}
	return nodes, nil
}

// Neighbors returns nodes one edge hop from id in the given direction.
func (s *sqliteStore) Neighbors(ctx context.Context, id string, dir Direction) ([]Node, error) {
	var q string
	switch dir {
	case Outgoing:
		q = `SELECT to_id FROM knowledge_edges WHERE from_id = ?`
	case Incoming:
		q = `SELECT from_id FROM knowledge_edges WHERE to_id = ?`
	default:
		return nil, fmt.Errorf("knowledge: unknown direction %q", dir)
	}
	rows, err := s.db.Query(ctx, q, id)
	if err != nil {
		return nil, fmt.Errorf("knowledge: neighbors %s: %w", id, err)
	}
	defer func() { _ = rows.Close() }()

	var ids []string
	for rows.Next() {
		var nid string
		if err := rows.Scan(&nid); err != nil {
			return nil, fmt.Errorf("knowledge: scan neighbor: %w", err)
		}
		ids = append(ids, nid)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("knowledge: iterate neighbors: %w", err)
	}

	out := make([]Node, 0, len(ids))
	for _, nid := range ids {
		n, err := s.nodeByID(ctx, nid)
		if err != nil {
			return nil, err
		}
		if n != nil {
			out = append(out, *n)
		}
	}
	return out, nil
}

// AllForLayerCheck returns every node for the project with edges populated.
func (s *sqliteStore) AllForLayerCheck(ctx context.Context, projectID string) ([]Node, error) {
	return s.loadNodes(ctx, Filter{})
}

// Count returns the number of persisted nodes for the project.
func (s *sqliteStore) Count(ctx context.Context, projectID string) (int, error) {
	var n int
	row := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM knowledge_nodes WHERE project_id = ?`, projectID)
	if err := row.Scan(&n); err != nil {
		return 0, fmt.Errorf("knowledge: count: %w", err)
	}
	return n, nil
}

// Close releases the underlying connection pool.
func (s *sqliteStore) Close() error { return s.db.Close() }

// loadNodes reads every node, applies the in-Go filter, and attaches edges.
func (s *sqliteStore) loadNodes(ctx context.Context, filter Filter) ([]Node, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, type, project_id, path, name, summary, content, embedding,
		       layer_id, language, line_start, line_end, last_indexed_at, checksum
		FROM knowledge_nodes ORDER BY path, line_start`)
	if err != nil {
		return nil, fmt.Errorf("knowledge: load nodes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		if filter.matches(&n) {
			out = append(out, n)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("knowledge: iterate nodes: %w", err)
	}
	for i := range out {
		edges, err := s.edgesFrom(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].OutgoingEdges = edges
	}
	return out, nil
}

func (s *sqliteStore) nodeByID(ctx context.Context, id string) (*Node, error) {
	row := s.db.QueryRow(ctx, `
		SELECT id, type, project_id, path, name, summary, content, embedding,
		       layer_id, language, line_start, line_end, last_indexed_at, checksum
		FROM knowledge_nodes WHERE id = ?`, id)
	n, err := scanNode(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	edges, err := s.edgesFrom(ctx, n.ID)
	if err != nil {
		return nil, err
	}
	n.OutgoingEdges = edges
	return &n, nil
}

func (s *sqliteStore) edgesFrom(ctx context.Context, id string) ([]Edge, error) {
	rows, err := s.db.Query(ctx,
		`SELECT from_id, to_id, edge_type, weight FROM knowledge_edges WHERE from_id = ?`, id)
	if err != nil {
		return nil, fmt.Errorf("knowledge: load edges %s: %w", id, err)
	}
	defer func() { _ = rows.Close() }()
	var out []Edge
	for rows.Next() {
		var e Edge
		var et string
		if err := rows.Scan(&e.FromID, &e.ToID, &et, &e.Weight); err != nil {
			return nil, fmt.Errorf("knowledge: scan edge: %w", err)
		}
		e.EdgeType = EdgeType(et)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("knowledge: iterate edges: %w", err)
	}
	return out, nil
}

// scanner abstracts *sql.Row and *sql.Rows for shared node scanning.
type scanner interface {
	Scan(dest ...any) error
}

func scanNode(sc scanner) (Node, error) {
	var (
		n       Node
		typ     string
		emb     []byte
		indexed string
	)
	if err := sc.Scan(&n.ID, &typ, &n.ProjectID, &n.Path, &n.Name, &n.Summary, &n.Content,
		&emb, &n.LayerID, &n.Language, &n.LineStart, &n.LineEnd, &indexed, &n.Checksum); err != nil {
		return Node{}, fmt.Errorf("knowledge: scan node: %w", err)
	}
	n.Type = NodeType(typ)
	n.Embedding = unpackEmbedding(emb)
	if indexed != "" {
		// Best-effort parse; an unparseable timestamp leaves the zero value.
		if t, err := parseStoreTime(indexed); err == nil {
			n.LastIndexedAt = t
		}
	}
	return n, nil
}

// packEmbedding serializes a float32 vector to a little-endian BLOB.
func packEmbedding(v []float32) []byte {
	if len(v) == 0 {
		return nil
	}
	b := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

// unpackEmbedding reverses packEmbedding.
func unpackEmbedding(b []byte) []float32 {
	if len(b) < 4 {
		return nil
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

// cosine returns the cosine similarity of two equal-length vectors. Mismatched
// or zero-magnitude vectors score 0.
func cosine(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

var _ Store = (*sqliteStore)(nil)
