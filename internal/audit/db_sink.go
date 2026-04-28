package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/conductor-sh/conductor/internal/db"
)

// DBSink persists audit events to the central audit_events table. It owns
// only the prepared statement; the *db.DB lifecycle is managed by the
// caller (typically the cmd layer).
type DBSink struct {
	db *db.DB
	mu sync.Mutex
}

// NewDBSink wires a DBSink against an open database. The caller is
// responsible for ensuring Migrate has run so the audit_events table
// exists.
func NewDBSink(d *db.DB) *DBSink {
	return &DBSink{db: d}
}

const insertAuditEventSQL = `
INSERT INTO audit_events (
	id, timestamp, project_id, issue_id, session_id, agent_role,
	event_type, payload, parent_event_id, duration_ms
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`

// Write inserts the event into audit_events. The mutex is purely defensive
// — modernc-sqlite serializes writes internally, but holding the lock keeps
// transient lock errors out of the hot path and matches the contract Sink
// implementations advertise.
func (s *DBSink) Write(ctx context.Context, evt AuditEvent) error {
	if s == nil || s.db == nil {
		return errors.New("audit: DBSink not initialized")
	}

	payload, err := json.Marshal(evt.Payload)
	if err != nil {
		return fmt.Errorf("audit: marshal payload: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	_, err = s.db.Exec(ctx, insertAuditEventSQL,
		evt.ID,
		evt.Timestamp.UTC().Format("2006-01-02T15:04:05.000Z07:00"),
		evt.ProjectID,
		nullableString(evt.IssueID),
		nullableString(evt.SessionID),
		nullableString(evt.AgentRole),
		string(evt.EventType),
		string(payload),
		nullableString(evt.ParentEventID),
		nullableInt(evt.DurationMS),
	)
	if err != nil {
		return fmt.Errorf("audit: insert event %s: %w", evt.ID, err)
	}
	return nil
}

// Close is a no-op for DBSink: the *db.DB is owned by the caller. The
// method exists so DBSink satisfies the Sink interface uniformly.
func (s *DBSink) Close() error { return nil }

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableInt(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}
