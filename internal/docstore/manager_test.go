package docstore

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/config"
	"github.com/conductor-sh/conductor/internal/knowledge"
)

// fakeKnowledgeStore records upserts and deletes for assertions.
type fakeKnowledgeStore struct {
	mu       sync.Mutex
	nodes    map[string]knowledge.Node // node ID → node
	upserts  int
	deletes  []string
	upsertFn func() error
}

func newFakeKnowledgeStore() *fakeKnowledgeStore {
	return &fakeKnowledgeStore{nodes: map[string]knowledge.Node{}}
}

func (s *fakeKnowledgeStore) Upsert(_ context.Context, nodes []knowledge.Node) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.upsertFn != nil {
		if err := s.upsertFn(); err != nil {
			return err
		}
	}
	s.upserts++
	for _, n := range nodes {
		s.nodes[n.ID] = n
	}
	return nil
}

func (s *fakeKnowledgeStore) DeleteByPath(_ context.Context, _, path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deletes = append(s.deletes, path)
	for id, n := range s.nodes {
		if n.Path == path {
			delete(s.nodes, id)
		}
	}
	return nil
}

func (s *fakeKnowledgeStore) nodeCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.nodes)
}

// stubBackend is a controllable in-memory backend for scheduler tests.
type stubBackend struct {
	mu       sync.Mutex
	docs     map[string]string // path → content
	syncs    int
	syncErr  error
	fetchErr error
}

func newStubBackend(docs map[string]string) *stubBackend {
	return &stubBackend{docs: docs}
}

func (b *stubBackend) set(path, content string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.docs[path] = content
}

func (b *stubBackend) del(path string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.docs, path)
}

func (b *stubBackend) Sync(_ context.Context) ([]DocRef, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.syncs++
	if b.syncErr != nil {
		return nil, b.syncErr
	}
	refs := make([]DocRef, 0, len(b.docs))
	for path, content := range b.docs {
		refs = append(refs, DocRef{
			ID:          DocRefID("stub", path),
			Title:       titleFor(path, []byte(content)),
			StoreID:     "stub",
			PathOrID:    path,
			ContentHash: HashContent([]byte(content)),
		})
	}
	return refs, nil
}

func (b *stubBackend) Fetch(_ context.Context, ref DocRef) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.fetchErr != nil {
		return "", b.fetchErr
	}
	return b.docs[ref.PathOrID], nil
}

func (b *stubBackend) List(_ context.Context, _ DocFilter) ([]DocRef, error) {
	return b.Sync(context.Background())
}

func newTestManager(t *testing.T, backend Backend, store knowledgeStore, clk func() time.Time) *Manager {
	t.Helper()
	opts := []Option{
		WithProjectID("proj"),
		WithBackend("stub", backend, config.DocStoreConfig{ID: "stub", SyncIntervalMinutes: 10}),
		WithClock(clk),
	}
	if store != nil {
		opts = append(opts, WithKnowledgeStore(store))
	}
	m, err := New(config.Docs{Enabled: true}, "proj", opts...)
	require.NoError(t, err)
	return m
}

func TestSyncStoreIndexesDocs(t *testing.T) {
	backend := newStubBackend(map[string]string{
		"a.md": "# A\nalpha",
		"b.md": "# B\nbeta",
	})
	store := newFakeKnowledgeStore()
	m := newTestManager(t, backend, store, time.Now)

	require.NoError(t, m.SyncStore(context.Background(), "stub"))
	assert.Equal(t, 2, store.nodeCount())
	assert.Equal(t, 1, store.upserts)
}

func TestSyncStoreOnlyDownloadsChanged(t *testing.T) {
	backend := newStubBackend(map[string]string{
		"a.md": "# A\nalpha",
		"b.md": "# B\nbeta",
	})
	store := newFakeKnowledgeStore()
	m := newTestManager(t, backend, store, time.Now)

	require.NoError(t, m.SyncStore(context.Background(), "stub"))
	assert.Equal(t, 1, store.upserts)

	// Change one doc; re-sync should upsert exactly one node.
	backend.set("a.md", "# A\nalpha v2")
	store.upserts = 0
	require.NoError(t, m.SyncStore(context.Background(), "stub"))
	assert.Equal(t, 1, store.upserts)
	store.mu.Lock()
	changedNode := store.nodes[knowledge.NodeID("proj", "docs/stub/a.md", "")]
	store.mu.Unlock()
	assert.Contains(t, changedNode.Content, "v2")

	// No change → no upsert.
	store.upserts = 0
	require.NoError(t, m.SyncStore(context.Background(), "stub"))
	assert.Equal(t, 0, store.upserts)
}

func TestSyncStoreRemovesDeletedDocs(t *testing.T) {
	backend := newStubBackend(map[string]string{"a.md": "a", "b.md": "b"})
	store := newFakeKnowledgeStore()
	m := newTestManager(t, backend, store, time.Now)

	require.NoError(t, m.SyncStore(context.Background(), "stub"))
	assert.Equal(t, 2, store.nodeCount())

	backend.del("b.md")
	require.NoError(t, m.SyncStore(context.Background(), "stub"))
	assert.Equal(t, 1, store.nodeCount())
	assert.Contains(t, store.deletes, "docs/stub/b.md")
}

func TestSyncStoreFailureRetainsLastGood(t *testing.T) {
	backend := newStubBackend(map[string]string{"a.md": "a"})
	store := newFakeKnowledgeStore()
	m := newTestManager(t, backend, store, time.Now)
	require.NoError(t, m.SyncStore(context.Background(), "stub"))
	require.Equal(t, 1, store.nodeCount())

	// Backend sync now fails: prior set must be retained and error classified.
	backend.mu.Lock()
	backend.syncErr = errors.New("backend down")
	backend.mu.Unlock()

	err := m.SyncStore(context.Background(), "stub")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrSyncFailed))
	assert.Equal(t, 1, store.nodeCount(), "last-good index retained")

	m.mu.Lock()
	st := m.stores["stub"]
	m.mu.Unlock()
	assert.Len(t, st.refs, 1, "last-good DocRef set retained")
}

func TestSyncStoreIndexFailureRetainsLastGood(t *testing.T) {
	backend := newStubBackend(map[string]string{"a.md": "a"})
	store := newFakeKnowledgeStore()
	m := newTestManager(t, backend, store, time.Now)
	require.NoError(t, m.SyncStore(context.Background(), "stub"))

	// Change the doc and make the index upsert fail.
	backend.set("a.md", "a v2")
	store.upsertFn = func() error { return errors.New("upsert failed") }
	err := m.SyncStore(context.Background(), "stub")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrSyncFailed))

	m.mu.Lock()
	st := m.stores["stub"]
	prev := st.refs["a.md"]
	m.mu.Unlock()
	assert.Equal(t, HashContent([]byte("a")), prev.ContentHash, "snapshot not advanced on index failure")
}

func TestSyncPendingGatesOnInterval(t *testing.T) {
	backend := newStubBackend(map[string]string{"a.md": "a"})
	now := time.Date(2026, 6, 6, 0, 0, 0, 0, time.UTC)
	clk := func() time.Time { return now }
	m := newTestManager(t, backend, nil, clk)

	// First SyncPending: never synced → due.
	require.NoError(t, m.SyncPending(context.Background()))
	assert.Equal(t, 1, backend.syncs)

	// Within interval (10 min): not due.
	now = now.Add(5 * time.Minute)
	require.NoError(t, m.SyncPending(context.Background()))
	assert.Equal(t, 1, backend.syncs)

	// Past interval: due again.
	now = now.Add(10 * time.Minute)
	require.NoError(t, m.SyncPending(context.Background()))
	assert.Equal(t, 2, backend.syncs)
}

func TestSyncAllJoinsErrors(t *testing.T) {
	backend := newStubBackend(map[string]string{"a.md": "a"})
	backend.syncErr = errors.New("down")
	m := newTestManager(t, backend, nil, time.Now)
	err := m.SyncAll(context.Background())
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrSyncFailed))
}

func TestNewSkipsRemoteBackendsWithoutInjection(t *testing.T) {
	m, err := New(config.Docs{
		Enabled: true,
		Stores: []config.DocStoreConfig{
			{ID: "remote", Backend: BackendGitRepo, PathOrURL: "https://x/y.git"},
		},
	}, "proj")
	require.NoError(t, err)
	m.mu.Lock()
	_, ok := m.stores["remote"]
	m.mu.Unlock()
	assert.False(t, ok, "git_repo without runner is skipped")
}

func TestNewUnsupportedBackendErrors(t *testing.T) {
	_, err := New(config.Docs{
		Enabled: true,
		Stores:  []config.DocStoreConfig{{ID: "n", Backend: "notion"}},
	}, "proj")
	require.Error(t, err)
}
