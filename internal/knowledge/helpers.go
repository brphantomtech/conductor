package knowledge

import (
	"context"
	"crypto/sha256"
	"math"
	"path"
	"strings"
)

// hashEmbedDim is the dimension of the deterministic fallback embedder. It is
// small but sufficient to give the brute-force cosine search a stable, content-
// sensitive signal when no real embedding provider is configured.
const hashEmbedDim = 256

// staticSummarizer is the default Summarizer used when no provider summarizer is
// injected. It extracts the first non-blank line(s) of content as a cheap,
// deterministic, provider-free summary so the engine is usable offline.
type staticSummarizer struct{}

// Summarize returns the first non-empty line of content, trimmed.
func (staticSummarizer) Summarize(_ context.Context, _ string, content string) (string, error) {
	for _, line := range strings.Split(content, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return truncate(t, 200), nil
		}
	}
	return "", nil
}

// hashEmbedder is the default Embedder: a deterministic, dependency-free hashing
// embedder (bag-of-tokens hashed into a fixed-dimension vector, L2-normalized).
// It is NOT a semantic model, but it gives stable, content-sensitive vectors so
// semantic search is exercised end-to-end without a live embedding provider.
type hashEmbedder struct {
	dim int
}

func newHashEmbedder(dim int) *hashEmbedder { return &hashEmbedder{dim: dim} }

// Embed hashes whitespace-delimited tokens into the vector space and normalizes.
func (h *hashEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	v := make([]float32, h.dim)
	for _, tok := range strings.Fields(strings.ToLower(text)) {
		sum := sha256.Sum256([]byte(tok))
		idx := (int(sum[0]) | int(sum[1])<<8) % h.dim
		sign := float32(1)
		if sum[2]&1 == 1 {
			sign = -1
		}
		v[idx] += sign
	}
	var norm float64
	for _, f := range v {
		norm += float64(f) * float64(f)
	}
	if norm > 0 {
		inv := float32(1 / math.Sqrt(norm))
		for i := range v {
			v[i] *= inv
		}
	}
	return v, nil
}

var _ Embedder = (*hashEmbedder)(nil)

// baseName returns the file base name for a slash-separated relative path.
func baseName(relPath string) string { return path.Base(filepathToSlash(relPath)) }

// unitName returns the display name for a parsed unit: the symbol name, or the
// file base name for chunk units.
func unitName(u parsedUnit, relPath string) string {
	if u.Name != "" {
		return u.Name
	}
	return baseName(relPath)
}

// fileSnippet returns a bounded snippet of file content for the file node.
func fileSnippet(content []byte) string {
	return truncate(string(content), contentSnippetLimit)
}

// truncate clips s to at most max runes, appending nothing (the embedder and
// the context formatter handle ellipsis separately where it matters).
func truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max]
}

// resolveImportPath turns a raw import target into an in-project relative path
// when it looks like one, or "" when it is an external dependency. A target
// containing a dot in its first segment (e.g. "github.com/foo/bar") or no slash
// at all is treated as external. Relative-looking targets ("./foo", "../bar",
// "internal/x") are normalized to a slash path.
func resolveImportPath(imp string) string {
	imp = strings.TrimSpace(imp)
	if imp == "" {
		return ""
	}
	imp = filepathToSlash(imp)
	imp = strings.TrimPrefix(imp, "./")
	// External module paths (have a dotted host in the first segment).
	first := imp
	if i := strings.IndexByte(imp, '/'); i >= 0 {
		first = imp[:i]
	}
	if strings.Contains(first, ".") {
		return ""
	}
	return imp
}
