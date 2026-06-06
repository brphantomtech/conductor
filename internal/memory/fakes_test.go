package memory

import (
	"context"
	"errors"
	"hash/fnv"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/conductor-sh/conductor/internal/audit"
	"github.com/conductor-sh/conductor/internal/config"
)

// enabledConfig returns a memory config with the manager enabled and the
// SPEC defaults left to withMemoryDefaults.
func enabledConfig() config.Memory {
	return config.Memory{Enabled: true}
}

// fakeStore is an in-memory MemoryStore for tests. It mirrors the SQLite
// store's ordering (most-recent first) and scoping semantics without a
// database.
type fakeStore struct {
	mu      sync.Mutex
	entries map[string]MemoryEntry
	closed  bool
	putErr  error
}

func newFakeStore() *fakeStore {
	return &fakeStore{entries: map[string]MemoryEntry{}}
}

func (f *fakeStore) Put(_ context.Context, e MemoryEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.putErr != nil {
		return f.putErr
	}
	f.entries[e.ID] = e
	return nil
}

func (f *fakeStore) Query(_ context.Context, q QueryFilter) ([]MemoryEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []MemoryEntry
	for _, e := range f.entries {
		if e.ProjectID != q.ProjectID {
			continue
		}
		if q.Layer != "" && e.Layer != q.Layer {
			continue
		}
		if q.IssueID != "" && e.IssueID != q.IssueID {
			continue
		}
		if q.TaskType != "" && e.TaskType != q.TaskType {
			continue
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

func (f *fakeStore) Get(_ context.Context, id string) (*MemoryEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.entries[id]
	if !ok {
		return nil, nil
	}
	cp := e
	return &cp, nil
}

func (f *fakeStore) Retire(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.entries, id)
	return nil
}

func (f *fakeStore) DeleteExpired(_ context.Context, projectID string, now time.Time) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for id, e := range f.entries {
		if e.ProjectID != projectID || e.Layer != LayerEpisodic {
			continue
		}
		if e.ExpiresAt != nil && !e.ExpiresAt.After(now) {
			delete(f.entries, id)
			n++
		}
	}
	return n, nil
}

func (f *fakeStore) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakeStore) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.entries)
}

// fakeEmbedder returns a deterministic vector derived from the text. Texts
// sharing a configured cluster key get near-identical vectors so similarity
// clustering is testable without a live API.
type fakeEmbedder struct {
	// clusterOf maps a substring to a cluster id; texts in the same cluster
	// embed to the same dominant axis.
	clusterOf func(text string) int
	dim       int
}

func newFakeEmbedder() *fakeEmbedder {
	return &fakeEmbedder{dim: 8}
}

func (e *fakeEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	dim := e.dim
	if dim == 0 {
		dim = 8
	}
	vec := make([]float32, dim)
	if e.clusterOf != nil {
		axis := e.clusterOf(text) % dim
		vec[axis] = 1
		return vec, nil
	}
	// Deterministic hash-spread vector, normalized.
	h := fnv.New32a()
	_, _ = h.Write([]byte(text))
	seed := h.Sum32()
	var norm float64
	for i := range vec {
		v := float32((seed>>(uint(i)%32))&0xff) / 255
		vec[i] = v
		norm += float64(v) * float64(v)
	}
	if norm > 0 {
		n := float32(math.Sqrt(norm))
		for i := range vec {
			vec[i] /= n
		}
	}
	return vec, nil
}

// fakeSynth returns a deterministic synthesis recording its inputs.
type fakeSynth struct {
	mu     sync.Mutex
	calls  [][]string
	result string
	err    error
}

func (s *fakeSynth) Synthesize(_ context.Context, inputs []string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, inputs)
	if s.err != nil {
		return "", s.err
	}
	if s.result != "" {
		return s.result, nil
	}
	return "synthesized: " + inputs[0], nil
}

func (s *fakeSynth) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

// recordingAudit captures emitted events for assertions.
type recordingAudit struct {
	mu     sync.Mutex
	events []audit.AuditEvent
}

func (r *recordingAudit) Write(_ context.Context, evt audit.AuditEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, evt)
	return nil
}

func (r *recordingAudit) typesOf(t audit.EventType) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, e := range r.events {
		if e.EventType == t {
			n++
		}
	}
	return n
}

// stepClock is a manually-advanced clock for deterministic TTL/cadence tests.
type stepClock struct {
	mu  sync.Mutex
	now time.Time
}

func newStepClock(t time.Time) *stepClock { return &stepClock{now: t} }

func (c *stepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *stepClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

var errFakeStore = errors.New("fake store boom")
