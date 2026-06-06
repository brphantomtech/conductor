package knowledge

import (
	"path"
	"strings"
)

// MatchPath reports whether the slash-separated relative path matches the glob
// pattern. It supports the doublestar `**` segment (match any number of path
// segments) in addition to the single-segment semantics of path.Match, so the
// knowledge.include_patterns / exclude_patterns and layer_definitions globs
// behave like the gitignore-style patterns operators expect.
//
// Matching is anchored at both ends. A pattern with no slash (e.g. "*.go") is
// matched against the base name as well so "*.go" matches "internal/foo.go".
func MatchPath(pattern, p string) bool {
	pattern = filepathToSlash(pattern)
	p = filepathToSlash(p)

	if !strings.Contains(pattern, "/") && !strings.Contains(pattern, "**") {
		// Bare pattern: try against the base name first (gitignore-style),
		// then against the whole path so "*.go" still matches "a/b.go".
		if ok, _ := path.Match(pattern, path.Base(p)); ok {
			return true
		}
	}
	return matchSegments(strings.Split(pattern, "/"), strings.Split(p, "/"))
}

// matchSegments implements `**`-aware glob matching over path segments.
func matchSegments(pat, name []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			// Collapse consecutive ** segments.
			for len(pat) > 1 && pat[1] == "**" {
				pat = pat[1:]
			}
			if len(pat) == 1 {
				return true // trailing ** matches the remainder
			}
			// Try to match the rest of the pattern at every suffix of name.
			for i := 0; i <= len(name); i++ {
				if matchSegments(pat[1:], name[i:]) {
					return true
				}
			}
			return false
		}
		if len(name) == 0 {
			return false
		}
		if ok, _ := path.Match(pat[0], name[0]); !ok {
			return false
		}
		pat, name = pat[1:], name[1:]
	}
	return len(name) == 0
}

func filepathToSlash(p string) string {
	return strings.ReplaceAll(p, "\\", "/")
}
