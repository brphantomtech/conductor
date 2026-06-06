//go:build qdrant

package knowledge

import (
	"context"
	"errors"

	"github.com/rs/zerolog"
)

// StoreBackendQdrant is the qdrant store_backend identifier (SPEC §5.3.8).
const StoreBackendQdrant = "qdrant"

// errQdrantNotImplemented is returned by the placeholder qdrant backend. The
// qdrant client (metadata payload + vector) is gated behind the `qdrant` build
// tag so the default CI build pulls in no external client or daemon dependency
// (design.md "Storage behind a KnowledgeStore interface"). When the real client
// is wired, replace this file's body with the HTTP/gRPC implementation that
// satisfies the Store interface.
var errQdrantNotImplemented = errors.New("knowledge: qdrant backend not implemented")

// NewQdrantStore is the qdrant backend constructor. It currently returns
// errQdrantNotImplemented; the build tag keeps the symbol out of the default
// binary entirely so the contract is documented without shipping a stub that
// silently misbehaves.
func NewQdrantStore(_ context.Context, _, _ string, _ zerolog.Logger) (Store, error) {
	return nil, errQdrantNotImplemented
}
