package memory

import (
	"context"
	"math"
)

// Embedder produces similarity vectors for memory content. The Memory
// Manager uses it for semantic retrieval and consolidation clustering. It is
// satisfied in production by an adapter over provider.Adapter (wired by the
// caller) and in tests by a deterministic fake — no live API calls in CI.
type Embedder interface {
	// Embed returns a fixed-dimension vector for the given text.
	Embed(ctx context.Context, text string) ([]float32, error)
}

// Synthesizer collapses a set of related memory contents into a single
// consolidated statement (SPEC §9.5 step 2 / 4). In production it wraps the
// configured consolidation_provider; in tests a fake returns a deterministic
// synthesis.
type Synthesizer interface {
	// Synthesize returns one consolidated memory statement for the inputs.
	Synthesize(ctx context.Context, inputs []string) (string, error)
}

// cosine returns the cosine similarity of two equal-length vectors in the
// range [-1, 1]. Mismatched or empty vectors yield 0.
func cosine(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
