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

func TestParseDocsURI(t *testing.T) {
	tests := []struct {
		name      string
		uri       string
		wantStore string
		wantPath  string
		wantErr   bool
	}{
		{"valid", "docs://specs/HARNESS.md", "specs", "HARNESS.md", false},
		{"nested path", "docs://specs/a/b/c.md", "specs", "a/b/c.md", false},
		{"wrong scheme", "file://x/y", "", "", true},
		{"no path", "docs://specs/", "", "", true},
		{"no store", "docs:///HARNESS.md", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, path, err := ParseDocsURI(tt.uri)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantStore, store)
			assert.Equal(t, tt.wantPath, path)
		})
	}
}

func TestIsDocsURI(t *testing.T) {
	assert.True(t, IsDocsURI("docs://x/y"))
	assert.False(t, IsDocsURI("./HARNESS.md"))
}

func TestResolveCachesLocally(t *testing.T) {
	backend := newStubBackend(map[string]string{"HARNESS.md": "# Harness\nfront matter"})
	m, err := New(config.Docs{Enabled: true}, "proj",
		WithBackend("specs", backend, config.DocStoreConfig{ID: "specs"}),
	)
	require.NoError(t, err)

	cacheDir := t.TempDir()
	local, err := m.Resolve(context.Background(), "docs://specs/HARNESS.md", cacheDir)
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(cacheDir, "specs", "HARNESS.md"), local)
	content, err := os.ReadFile(local)
	require.NoError(t, err)
	assert.Contains(t, string(content), "front matter")
}

func TestResolveUnknownStore(t *testing.T) {
	m, err := New(config.Docs{Enabled: true}, "proj")
	require.NoError(t, err)
	_, err = m.Resolve(context.Background(), "docs://missing/x.md", t.TempDir())
	require.Error(t, err)
}
