package docstore

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/config"
)

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
}

func TestLocalFSBackendListAndFetch(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "specs/a.md", "# Alpha\nbody a")
	writeFile(t, dir, "specs/b.md", "# Beta\nbody b")
	writeFile(t, dir, "ignore.txt", "not markdown")

	b := newLocalFSBackend("specs", dir, []string{"*.md"}, []string{"spec"})
	refs, err := b.Sync(context.Background())
	require.NoError(t, err)
	require.Len(t, refs, 2)

	assert.Equal(t, "specs/a.md", refs[0].PathOrID)
	assert.Equal(t, "Alpha", refs[0].Title)
	assert.Equal(t, []string{"spec"}, refs[0].Tags)
	assert.NotEmpty(t, refs[0].ContentHash)

	content, err := b.Fetch(context.Background(), refs[1])
	require.NoError(t, err)
	assert.Contains(t, content, "body b")
}

func TestLocalFSBackendListFilter(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "specs/a.md", "a")
	writeFile(t, dir, "adr/b.md", "b")

	b := newLocalFSBackend("docs", dir, nil, nil)
	refs, err := b.List(context.Background(), DocFilter{PathPrefix: "specs/"})
	require.NoError(t, err)
	require.Len(t, refs, 1)
	assert.Equal(t, "specs/a.md", refs[0].PathOrID)
}

// fakeGitRunner records clone/pull calls and populates the working tree.
type fakeGitRunner struct {
	clones int
	pulls  int
	files  map[string]string // rel → content, written on Clone
}

func (r *fakeGitRunner) Clone(_ context.Context, _, dir string, _ map[string]string) error {
	r.clones++
	for rel, content := range r.files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			return err
		}
	}
	// Mark as a git clone so a second Sync pulls rather than re-clones.
	return os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
}

func (r *fakeGitRunner) Pull(_ context.Context, _ string, _ map[string]string) error {
	r.pulls++
	return nil
}

func TestGitRepoBackendUsesInjectedRunner(t *testing.T) {
	runner := &fakeGitRunner{files: map[string]string{"README.md": "# Docs\nhi"}}
	work := t.TempDir()
	b := newGitRepoBackend("git", "https://example/repo.git", work, nil, []string{"*.md"}, nil, runner)

	refs, err := b.Sync(context.Background())
	require.NoError(t, err)
	require.Len(t, refs, 1)
	assert.Equal(t, 1, runner.clones)
	assert.Equal(t, 0, runner.pulls)

	// Second sync pulls (clone already present).
	_, err = b.Sync(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, runner.clones)
	assert.Equal(t, 1, runner.pulls)

	content, err := b.Fetch(context.Background(), refs[0])
	require.NoError(t, err)
	assert.Contains(t, content, "hi")
}

// fakeS3Client serves objects from an in-memory map.
type fakeS3Client struct {
	objects map[string]string
	lists   int
}

func (c *fakeS3Client) List(_ context.Context, prefix string) ([]S3Object, error) {
	c.lists++
	var out []S3Object
	for k, v := range c.objects {
		if prefix == "" || len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			out = append(out, S3Object{Key: k, Content: []byte(v)})
		}
	}
	return out, nil
}

func (c *fakeS3Client) Get(_ context.Context, key string) ([]byte, error) {
	v, ok := c.objects[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return []byte(v), nil
}

func TestS3BackendUsesInjectedClient(t *testing.T) {
	client := &fakeS3Client{objects: map[string]string{
		"docs/a.md": "# A\nalpha",
		"docs/b.md": "# B\nbeta",
	}}
	b := newS3Backend("s3", "docs/", []string{"*.md"}, []string{"s3"}, client)

	refs, err := b.Sync(context.Background())
	require.NoError(t, err)
	require.Len(t, refs, 2)
	assert.Equal(t, "a.md", refs[0].PathOrID)
	assert.Equal(t, 1, client.lists)

	content, err := b.Fetch(context.Background(), refs[0])
	require.NoError(t, err)
	assert.Contains(t, content, "alpha")
}

func TestBuildBackend(t *testing.T) {
	lfs, err := buildBackend(config.DocStoreConfig{ID: "x", Backend: BackendLocalFS, PathOrURL: t.TempDir()})
	require.NoError(t, err)
	require.NotNil(t, lfs)

	git, err := buildBackend(config.DocStoreConfig{ID: "g", Backend: BackendGitRepo})
	require.NoError(t, err)
	assert.Nil(t, git, "git_repo needs an injected runner")

	_, err = buildBackend(config.DocStoreConfig{ID: "n", Backend: "notion"})
	require.Error(t, err)
}
