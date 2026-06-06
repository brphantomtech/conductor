package docstore

import (
	"context"

	"github.com/conductor-sh/conductor/internal/knowledge"
)

// knowledgeStore is the slice of the Knowledge Engine store the manager needs to
// index documents as `doc` nodes (SPEC §10.1). Defined at the consumer per
// docs/conventions.md §3; production passes a *knowledge.Engine-backed store and
// tests pass a fake or a real knowledge.Store.
type knowledgeStore interface {
	// Upsert idempotently writes nodes keyed by node ID.
	Upsert(ctx context.Context, nodes []knowledge.Node) error
	// DeleteByPath removes every node at the given relative path.
	DeleteByPath(ctx context.Context, projectID, path string) error
}

// docEmbedder produces a dense vector for the document body so the `doc` node is
// retrievable through the Knowledge Engine's semantic path. Defined at the
// consumer; a nil embedder leaves embeddings empty (structural search still
// works).
type docEmbedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// knowledge.Store satisfies knowledgeStore for the Upsert/DeleteByPath subset.
var _ knowledgeStore = knowledge.Store(nil)
