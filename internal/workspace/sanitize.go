package workspace

import (
	"path/filepath"
	"regexp"
	"strings"
)

// keyDisallowed matches every character outside the SPEC §14.2 Invariant 3 /
// §4.2 allow-list. Matches operate on runes, so a multi-byte character counts
// as one replacement.
var keyDisallowed = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// SanitizeKey implements the SPEC §4.2 / §14.2 Invariant 3 workspace-key rule:
// only [A-Za-z0-9._-] survive; every other character becomes '_'. It does not
// resolve traversal sequences made of allowed characters (e.g. ".."); the
// withinRoot confinement check (Invariant 2) is the second line of defense.
func SanitizeKey(s string) string {
	return keyDisallowed.ReplaceAllString(s, "_")
}

// withinRoot reports whether p is a path strictly inside root (SPEC §14.2
// Invariant 2). Both arguments are cleaned; p must be a proper descendant —
// root itself, the parent, or any traversal outside returns false.
func withinRoot(root, p string) bool {
	root = filepath.Clean(root)
	p = filepath.Clean(p)
	if root == "" || p == root {
		return false
	}
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return !filepath.IsAbs(rel)
}
