package memory

import (
	"context"
	"time"
)

// QueryFilter selects entries for retrieval. Zero-value fields are
// wildcards: an empty Layer matches every layer, an empty IssueID matches
// every issue, and so on. Limit of zero means "no limit".
type QueryFilter struct {
	// ProjectID is required for every query; entries are project-scoped.
	ProjectID string
	// Layer restricts to a single layer when set.
	Layer Layer
	// IssueID restricts episodic queries to one issue when set.
	IssueID string
	// TaskType restricts procedural queries to one task type when set.
	TaskType string
	// Limit caps the number of rows returned. Zero means unlimited.
	Limit int
}

// MemoryStore is the persistence contract behind the Memory Manager. It is
// defined here, in the consumer package, per the project convention; the
// SQLite backend in this package is the only production implementation, and
// tests inject an in-memory fake.
//
//revive:disable-next-line:exported // "MemoryStore" is the SPEC's name for the memory persistence contract.
type MemoryStore interface {
	// Put inserts or replaces an entry by ID.
	Put(ctx context.Context, e MemoryEntry) error

	// Query returns entries matching the filter, ordered most-recent first.
	Query(ctx context.Context, f QueryFilter) ([]MemoryEntry, error)

	// Get returns a single entry by ID. Returns (nil, nil) when absent.
	Get(ctx context.Context, id string) (*MemoryEntry, error)

	// Retire deletes an entry by ID so it no longer appears in retrieval.
	Retire(ctx context.Context, id string) error

	// DeleteExpired removes episodic entries whose ExpiresAt is at or before
	// now, scoped to projectID. It returns the number deleted.
	DeleteExpired(ctx context.Context, projectID string, now time.Time) (int, error)

	// Close releases backing resources.
	Close() error
}
