package docstore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DocsScheme is the URI scheme for Doc Store HARNESS references (SPEC §10.4).
const DocsScheme = "docs://"

// DefaultCacheDir is the workspace-relative cache directory docs:// references
// are materialized into before the harness loader parses them.
const DefaultCacheDir = ".conductor/doc-cache"

// IsDocsURI reports whether ref is a docs:// reference.
func IsDocsURI(ref string) bool {
	return strings.HasPrefix(ref, DocsScheme)
}

// ParseDocsURI splits a docs://<store>/<path> reference into its store id and
// document path. It errors when the scheme is wrong or either component is
// missing.
func ParseDocsURI(uri string) (storeID, docPath string, err error) {
	if !IsDocsURI(uri) {
		return "", "", fmt.Errorf("docstore: %q is not a docs:// reference", uri)
	}
	rest := strings.TrimPrefix(uri, DocsScheme)
	idx := strings.IndexByte(rest, '/')
	if idx <= 0 || idx == len(rest)-1 {
		return "", "", fmt.Errorf("docstore: malformed docs:// reference %q (want docs://<store>/<path>)", uri)
	}
	return rest[:idx], rest[idx+1:], nil
}

// Resolve resolves a docs://<store>/<path> reference: it fetches the document
// from the configured store, writes it to a local cache file under cacheDir, and
// returns the local path for the harness loader to parse. A zero cacheDir uses
// DefaultCacheDir.
func (m *Manager) Resolve(ctx context.Context, uri, cacheDir string) (string, error) {
	storeID, docPath, err := ParseDocsURI(uri)
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	st, ok := m.stores[storeID]
	m.mu.Unlock()
	if !ok {
		return "", fmt.Errorf("docstore: docs:// references unknown store %q", storeID)
	}

	content, err := st.backend.Fetch(ctx, DocRef{StoreID: storeID, PathOrID: docPath})
	if err != nil {
		return "", fmt.Errorf("docstore: resolve %s: %w", uri, err)
	}

	if cacheDir == "" {
		cacheDir = DefaultCacheDir
	}
	local := filepath.Join(cacheDir, filepath.FromSlash(storeID), filepath.FromSlash(docPath))
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		return "", fmt.Errorf("docstore: cache dir %s: %w", local, err)
	}
	if err := os.WriteFile(local, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("docstore: cache write %s: %w", local, err)
	}
	return local, nil
}
