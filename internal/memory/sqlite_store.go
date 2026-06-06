package memory

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/conductor-sh/conductor/internal/db"
)

// sqliteStore is the production MemoryStore. It reuses the internal/db
// connection pool and keeps the memory entries in a single self-managed
// table; embeddings are stored as a little-endian float32 blob and scanned
// in-process for brute-force cosine similarity (memory volume is small
// relative to the codebase graph, see design.md).
type sqliteStore struct {
	db    *db.DB
	owned bool
}

// newSQLiteStore opens (or reuses) a SQLite database at dsn and ensures the
// memory_entries table exists. An empty dsn opens a shared in-memory
// database, matching the internal/db convention.
func newSQLiteStore(ctx context.Context, dsn string) (*sqliteStore, error) {
	d, err := db.Open(ctx, db.Options{Driver: db.DriverSQLite, DSN: dsn})
	if err != nil {
		return nil, fmt.Errorf("memory: open store: %w", err)
	}
	s := &sqliteStore{db: d, owned: true}
	if err := s.ensureSchema(ctx); err != nil {
		_ = d.Close()
		return nil, err
	}
	return s, nil
}

// newSQLiteStoreFromDB wraps an already-open *db.DB. The caller retains
// ownership of the connection pool (Close is a no-op on the store).
func newSQLiteStoreFromDB(ctx context.Context, d *db.DB) (*sqliteStore, error) {
	s := &sqliteStore{db: d, owned: false}
	if err := s.ensureSchema(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *sqliteStore) ensureSchema(ctx context.Context) error {
	const ddl = `
CREATE TABLE IF NOT EXISTS memory_entries (
    id          TEXT PRIMARY KEY,
    layer       TEXT NOT NULL,
    project_id  TEXT NOT NULL,
    issue_id    TEXT NOT NULL DEFAULT '',
    task_type   TEXT NOT NULL DEFAULT '',
    content     TEXT NOT NULL,
    embedding   BLOB,
    tags        TEXT NOT NULL DEFAULT '',
    source      TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    expires_at  TEXT
);
CREATE INDEX IF NOT EXISTS idx_memory_scope
    ON memory_entries (project_id, layer, issue_id, task_type);`
	if _, err := s.db.Exec(ctx, ddl); err != nil {
		return fmt.Errorf("memory: create schema: %w", err)
	}
	return nil
}

// Put satisfies MemoryStore.
func (s *sqliteStore) Put(ctx context.Context, e MemoryEntry) error {
	var expires any
	if e.ExpiresAt != nil {
		expires = e.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	_, err := s.db.Exec(ctx, `
INSERT INTO memory_entries
    (id, layer, project_id, issue_id, task_type, content, embedding, tags, source, created_at, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    layer=excluded.layer, project_id=excluded.project_id, issue_id=excluded.issue_id,
    task_type=excluded.task_type, content=excluded.content, embedding=excluded.embedding,
    tags=excluded.tags, source=excluded.source, created_at=excluded.created_at,
    expires_at=excluded.expires_at`,
		e.ID, string(e.Layer), e.ProjectID, e.IssueID, e.TaskType, e.Content,
		encodeEmbedding(e.Embedding), strings.Join(e.Tags, "\x1f"), string(e.Source),
		e.CreatedAt.UTC().Format(time.RFC3339Nano), expires,
	)
	if err != nil {
		return fmt.Errorf("memory: put %s: %w", e.ID, err)
	}
	return nil
}

// Query satisfies MemoryStore.
func (s *sqliteStore) Query(ctx context.Context, f QueryFilter) ([]MemoryEntry, error) {
	var (
		conds = []string{"project_id = ?"}
		args  = []any{f.ProjectID}
	)
	if f.Layer != "" {
		conds = append(conds, "layer = ?")
		args = append(args, string(f.Layer))
	}
	if f.IssueID != "" {
		conds = append(conds, "issue_id = ?")
		args = append(args, f.IssueID)
	}
	if f.TaskType != "" {
		conds = append(conds, "task_type = ?")
		args = append(args, f.TaskType)
	}
	q := "SELECT id, layer, project_id, issue_id, task_type, content, embedding, tags, source, created_at, expires_at " +
		"FROM memory_entries WHERE " + strings.Join(conds, " AND ") + " ORDER BY created_at DESC, id DESC"
	if f.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", f.Limit)
	}
	rows, err := s.db.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("memory: query: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanEntries(rows)
}

// Get satisfies MemoryStore.
func (s *sqliteStore) Get(ctx context.Context, id string) (*MemoryEntry, error) {
	rows, err := s.db.Query(ctx, `
SELECT id, layer, project_id, issue_id, task_type, content, embedding, tags, source, created_at, expires_at
FROM memory_entries WHERE id = ?`, id)
	if err != nil {
		return nil, fmt.Errorf("memory: get %s: %w", id, err)
	}
	defer func() { _ = rows.Close() }()
	entries, err := scanEntries(rows)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, nil
	}
	return &entries[0], nil
}

// Retire satisfies MemoryStore.
func (s *sqliteStore) Retire(ctx context.Context, id string) error {
	if _, err := s.db.Exec(ctx, `DELETE FROM memory_entries WHERE id = ?`, id); err != nil {
		return fmt.Errorf("memory: retire %s: %w", id, err)
	}
	return nil
}

// DeleteExpired satisfies MemoryStore.
func (s *sqliteStore) DeleteExpired(ctx context.Context, projectID string, now time.Time) (int, error) {
	res, err := s.db.Exec(ctx, `
DELETE FROM memory_entries
WHERE project_id = ? AND layer = ? AND expires_at IS NOT NULL AND expires_at <= ?`,
		projectID, string(LayerEpisodic), now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, fmt.Errorf("memory: delete expired: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("memory: delete expired rows: %w", err)
	}
	return int(n), nil
}

// Close satisfies MemoryStore. It only closes connections it owns.
func (s *sqliteStore) Close() error {
	if !s.owned {
		return nil
	}
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("memory: close store: %w", err)
	}
	return nil
}

func scanEntries(rows *sql.Rows) ([]MemoryEntry, error) {
	var out []MemoryEntry
	for rows.Next() {
		var (
			e       MemoryEntry
			layer   string
			source  string
			emb     []byte
			tags    string
			created string
			expires sql.NullString
		)
		if err := rows.Scan(&e.ID, &layer, &e.ProjectID, &e.IssueID, &e.TaskType,
			&e.Content, &emb, &tags, &source, &created, &expires); err != nil {
			return nil, fmt.Errorf("memory: scan entry: %w", err)
		}
		e.Layer = Layer(layer)
		e.Source = Source(source)
		e.Embedding = decodeEmbedding(emb)
		if tags != "" {
			e.Tags = strings.Split(tags, "\x1f")
		}
		t, err := time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return nil, fmt.Errorf("memory: parse created_at %q: %w", created, err)
		}
		e.CreatedAt = t.UTC()
		if expires.Valid && expires.String != "" {
			et, err := time.Parse(time.RFC3339Nano, expires.String)
			if err != nil {
				return nil, fmt.Errorf("memory: parse expires_at %q: %w", expires.String, err)
			}
			etUTC := et.UTC()
			e.ExpiresAt = &etUTC
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("memory: iterate rows: %w", err)
	}
	return out, nil
}

// encodeEmbedding serializes a float32 vector as a little-endian blob.
// A nil/empty vector encodes to nil so the column stays NULL.
func encodeEmbedding(v []float32) []byte {
	if len(v) == 0 {
		return nil
	}
	b := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

// decodeEmbedding reverses encodeEmbedding. A blob whose length is not a
// multiple of four is treated as absent.
func decodeEmbedding(b []byte) []float32 {
	if len(b) == 0 || len(b)%4 != 0 {
		return nil
	}
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return out
}

// compile-time assertion that sqliteStore satisfies MemoryStore.
var _ MemoryStore = (*sqliteStore)(nil)
