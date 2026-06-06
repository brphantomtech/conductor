package docstore

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// localFSBackend is the local_fs Doc Store backend (SPEC §10.2): it walks a
// directory tree and exposes every matching file as a document. It is the only
// backend that touches the real filesystem.
type localFSBackend struct {
	storeID         string
	root            string
	includePatterns []string
	tags            []string
}

// newLocalFSBackend constructs a local_fs backend rooted at dir. includePatterns
// are slash-separated globs matched against the store-relative path; an empty
// list includes every file. tags are propagated to every document's DocRef.
func newLocalFSBackend(storeID, dir string, includePatterns, tags []string) *localFSBackend {
	return &localFSBackend{
		storeID:         storeID,
		root:            dir,
		includePatterns: includePatterns,
		tags:            tags,
	}
}

// Sync walks the directory and returns a DocRef (with ContentHash) for every
// included file.
func (b *localFSBackend) Sync(ctx context.Context) ([]DocRef, error) {
	var refs []DocRef
	walkErr := filepath.WalkDir(b.root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walk %s: %w", p, err)
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("walk cancelled: %w", ctxErr)
		}
		if d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(b.root, p)
		if relErr != nil {
			return fmt.Errorf("rel %s: %w", p, relErr)
		}
		rel = filepath.ToSlash(rel)
		if !b.included(rel) {
			return nil
		}
		content, readErr := os.ReadFile(p)
		if readErr != nil {
			return fmt.Errorf("read %s: %w", p, readErr)
		}
		refs = append(refs, DocRef{
			ID:          DocRefID(b.storeID, rel),
			Title:       titleFor(rel, content),
			StoreID:     b.storeID,
			PathOrID:    rel,
			ContentHash: HashContent(content),
			Tags:        append([]string(nil), b.tags...),
		})
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("docstore: local_fs sync %s: %w", b.root, walkErr)
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].PathOrID < refs[j].PathOrID })
	return refs, nil
}

// Fetch reads the document content from disk.
func (b *localFSBackend) Fetch(_ context.Context, ref DocRef) (string, error) {
	abs := filepath.Join(b.root, filepath.FromSlash(ref.PathOrID))
	content, err := os.ReadFile(abs)
	if err != nil {
		return "", fmt.Errorf("docstore: local_fs fetch %s: %w", ref.PathOrID, err)
	}
	return string(content), nil
}

// List re-walks the store and applies the filter.
func (b *localFSBackend) List(ctx context.Context, filter DocFilter) ([]DocRef, error) {
	refs, err := b.Sync(ctx)
	if err != nil {
		return nil, err
	}
	out := refs[:0]
	for _, r := range refs {
		if filter.matches(r) {
			out = append(out, r)
		}
	}
	return out, nil
}

// included reports whether the store-relative path matches the include patterns.
func (b *localFSBackend) included(rel string) bool {
	if len(b.includePatterns) == 0 {
		return true
	}
	for _, pat := range b.includePatterns {
		if matchGlob(pat, rel) {
			return true
		}
	}
	return false
}

// matchGlob matches a slash-separated path against a glob pattern. A bare
// pattern (no slash) is matched against the base name so "*.md" matches
// "specs/a.md".
func matchGlob(pattern, p string) bool {
	pattern = filepath.ToSlash(pattern)
	p = filepath.ToSlash(p)
	if !strings.Contains(pattern, "/") {
		if ok, _ := path.Match(pattern, path.Base(p)); ok {
			return true
		}
	}
	ok, _ := path.Match(pattern, p)
	return ok
}

// titleFor derives a document title from its first markdown heading, falling
// back to the base filename without extension.
func titleFor(rel string, content []byte) string {
	for _, line := range strings.Split(string(content), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			return strings.TrimSpace(strings.TrimLeft(trimmed, "# "))
		}
		if trimmed != "" {
			break
		}
	}
	base := path.Base(rel)
	return strings.TrimSuffix(base, path.Ext(base))
}

var _ Backend = (*localFSBackend)(nil)
