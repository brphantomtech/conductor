package knowledge

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"

	"github.com/conductor-sh/conductor/internal/config"
)

// OpenStore constructs the Store for the configured backend (SPEC §8.2 Stage 7).
// The default (and the only backend in the default build) is sqlite_vec. The
// qdrant backend is only available when the binary is built with the `qdrant`
// build tag; without it, selecting qdrant returns an error.
func OpenStore(ctx context.Context, cfg config.Knowledge, log zerolog.Logger) (Store, error) {
	backend := cfg.StoreBackend
	if backend == "" {
		backend = StoreBackendSQLiteVec
	}
	switch backend {
	case StoreBackendSQLiteVec, "sqlite":
		return NewSQLiteStore(ctx, cfg.StorePath, log)
	case "qdrant":
		return openQdrant(ctx, cfg, log)
	default:
		return nil, fmt.Errorf("knowledge: unsupported store_backend %q", backend)
	}
}
