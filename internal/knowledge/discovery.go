package knowledge

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// defaultExcludePatterns are always skipped during discovery in addition to the
// configured knowledge.exclude_patterns. They keep the index free of VCS
// metadata, dependency caches, and build output that would otherwise dominate.
var defaultExcludePatterns = []string{
	"**/.git/**",
	"**/node_modules/**",
	"**/vendor/**",
	"**/.conductor/**",
	"**/*.db",
	"**/*.db-shm",
	"**/*.db-wal",
	"**/*.exe",
}

// discoveredFile is one file selected by discovery (SPEC §8.2 Stage 1).
type discoveredFile struct {
	// AbsPath is the absolute path on disk.
	AbsPath string
	// RelPath is the slash-separated path relative to the repo root; it is the
	// node path and the key for incremental checksum comparison.
	RelPath string
	// Checksum is the content hash for skip-unchanged comparison.
	Checksum string
	// Content is the file bytes (read once during discovery).
	Content []byte
}

// discoverer walks repo roots, applies include/exclude filters, and reads file
// contents + checksums. It is constructed per index cycle.
type discoverer struct {
	include []string
	exclude []string
}

// newDiscoverer builds a discoverer from the knowledge config patterns.
func newDiscoverer(include, exclude []string) *discoverer {
	return &discoverer{
		include: include,
		exclude: append(append([]string(nil), exclude...), defaultExcludePatterns...),
	}
}

// discover walks root and returns the files that pass the include/exclude
// filters. Symlinks and unreadable files are skipped.
func (d *discoverer) discover(root string) ([]discoveredFile, error) {
	var out []discoveredFile
	walkErr := filepath.WalkDir(root, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			// Skip unreadable subtrees rather than aborting the whole walk.
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if entry.IsDir() {
			if d.excluded(rel + "/") {
				return fs.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		if d.excluded(rel) || !d.included(rel) {
			return nil
		}
		content, readErr := os.ReadFile(p)
		if readErr != nil {
			return nil
		}
		out = append(out, discoveredFile{
			AbsPath:  p,
			RelPath:  rel,
			Checksum: Checksum(content),
			Content:  content,
		})
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("knowledge: discover %q: %w", root, walkErr)
	}
	return out, nil
}

// included reports whether rel matches any include pattern. An empty include
// list means "include everything" (filtered only by excludes).
func (d *discoverer) included(rel string) bool {
	if len(d.include) == 0 {
		return true
	}
	for _, pat := range d.include {
		if MatchPath(pat, rel) {
			return true
		}
	}
	return false
}

// excluded reports whether rel matches any exclude pattern.
func (d *discoverer) excluded(rel string) bool {
	for _, pat := range d.exclude {
		if MatchPath(pat, rel) {
			return true
		}
		// A directory exclude like "**/node_modules/**" should also match the
		// bare directory path with a trailing slash already appended by the
		// caller; additionally try the de-slashed form for leaf matches.
		if strings.HasSuffix(rel, "/") && MatchPath(pat, strings.TrimSuffix(rel, "/")) {
			return true
		}
	}
	return false
}
