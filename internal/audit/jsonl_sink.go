package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// JSONLSink appends one NDJSON record per event to a configured file path.
// The orchestrator wires one JSONLSink per workspace so each issue carries
// its own .conductor/audit.jsonl, satisfying the SPEC §14.1 / §17.3
// "local audit log" requirement.
type JSONLSink struct {
	path string

	mu sync.Mutex
	f  *os.File
}

// NewJSONLSink opens (or creates) the audit log at path. Parent directories
// are created on demand so callers do not need to coordinate workspace
// scaffolding with sink construction. Permissions are 0o755 for the
// directory tree and 0o644 for the file, matching the rest of the
// workspace layout.
func NewJSONLSink(path string) (*JSONLSink, error) {
	if path == "" {
		return nil, fmt.Errorf("audit: JSONL sink path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("audit: create audit log dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("audit: open audit log %q: %w", path, err)
	}
	return &JSONLSink{path: path, f: f}, nil
}

// Path returns the file the sink is writing to. Useful for diagnostics and
// for the API layer to surface in workspace metadata.
func (s *JSONLSink) Path() string { return s.path }

// Write encodes evt as a single JSON line and appends it. The mutex
// serializes writers so concurrent calls produce well-formed NDJSON.
func (s *JSONLSink) Write(_ context.Context, evt AuditEvent) error {
	if s == nil || s.f == nil {
		return fmt.Errorf("audit: JSONL sink %q not initialized", s.pathOrEmpty())
	}

	data, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("audit: marshal event %s: %w", evt.ID, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("audit: write event %s to %s: %w", evt.ID, s.path, err)
	}
	return nil
}

// Close flushes and closes the underlying file. Subsequent Writes return
// an error.
func (s *JSONLSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return nil
	}
	err := s.f.Close()
	s.f = nil
	if err != nil {
		return fmt.Errorf("audit: close %s: %w", s.path, err)
	}
	return nil
}

func (s *JSONLSink) pathOrEmpty() string {
	if s == nil {
		return ""
	}
	return s.path
}
