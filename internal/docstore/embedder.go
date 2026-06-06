package docstore

import (
	"context"
	"crypto/sha256"
	"math"
	"strings"
)

// DefaultEmbedDim is the dimension of the deterministic fallback embedder. It
// mirrors the Knowledge Engine's offline embedder so doc-node vectors and search
// queries share a comparable space when no live embedding provider is wired.
const DefaultEmbedDim = 256

// HashEmbedder is a deterministic, dependency-free embedder (bag-of-tokens
// hashed into a fixed-dimension, L2-normalized vector). It is NOT a semantic
// model, but it gives stable, content-sensitive vectors so doc search is
// exercised end-to-end without a live embedding provider. The same instance
// must embed both indexed documents and search queries for results to align.
type HashEmbedder struct {
	dim int
}

// NewHashEmbedder constructs a HashEmbedder of the given dimension; a dim <= 0
// uses DefaultEmbedDim.
func NewHashEmbedder(dim int) *HashEmbedder {
	if dim <= 0 {
		dim = DefaultEmbedDim
	}
	return &HashEmbedder{dim: dim}
}

// Embed hashes whitespace-delimited tokens into the vector space and normalizes.
func (h *HashEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
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

var (
	_ docEmbedder = (*HashEmbedder)(nil)
)
