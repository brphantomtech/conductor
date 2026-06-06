//go:build qdrant

package knowledge

import (
	"context"

	"github.com/rs/zerolog"

	"github.com/conductor-sh/conductor/internal/config"
)

// openQdrant constructs the qdrant backend when built with the `qdrant` tag.
func openQdrant(ctx context.Context, cfg config.Knowledge, log zerolog.Logger) (Store, error) {
	return NewQdrantStore(ctx, cfg.QdrantURL, cfg.QdrantCollection, log)
}
