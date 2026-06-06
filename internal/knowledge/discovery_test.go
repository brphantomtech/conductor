package knowledge

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// writeFixtureTree creates a small multi-file tree and returns its root.
func writeFixtureTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"main.go":                   "package main\nfunc main() {}\n",
		"internal/auth/auth.go":     goSample,
		"docs/readme.md":            "# Readme\nSome documentation.\n",
		"node_modules/dep/index.js": "module.exports = {}\n",
		".git/config":               "[core]\n",
		"config.yaml":               "key: value\n",
	}
	for rel, content := range files {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
		require.NoError(t, os.WriteFile(abs, []byte(content), 0o644))
	}
	return root
}

func relPaths(files []discoveredFile) map[string]bool {
	out := map[string]bool{}
	for _, f := range files {
		out[f.RelPath] = true
	}
	return out
}

func TestDiscoverRespectsExcludes(t *testing.T) {
	root := writeFixtureTree(t)
	d := newDiscoverer(nil, nil)
	files, err := d.discover(root)
	require.NoError(t, err)

	got := relPaths(files)
	require.True(t, got["main.go"])
	require.True(t, got["internal/auth/auth.go"])
	require.True(t, got["docs/readme.md"])
	require.True(t, got["config.yaml"])
	require.False(t, got["node_modules/dep/index.js"], "node_modules excluded by default")
	require.False(t, got[".git/config"], ".git excluded by default")
}

func TestDiscoverIncludePatterns(t *testing.T) {
	root := writeFixtureTree(t)
	d := newDiscoverer([]string{"**/*.go"}, nil)
	files, err := d.discover(root)
	require.NoError(t, err)

	got := relPaths(files)
	require.True(t, got["main.go"])
	require.True(t, got["internal/auth/auth.go"])
	require.False(t, got["docs/readme.md"], "non-go excluded by include filter")
	require.False(t, got["config.yaml"])
}

func TestDiscoverUserExcludePattern(t *testing.T) {
	root := writeFixtureTree(t)
	d := newDiscoverer(nil, []string{"docs/**"})
	files, err := d.discover(root)
	require.NoError(t, err)

	require.False(t, relPaths(files)["docs/readme.md"])
}
