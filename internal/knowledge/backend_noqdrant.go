//go:build !qdrant

package knowledge

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"

	"github.com/conductor-sh/conductor/internal/config"
)

// openQdrant is the default-build stub: the qdrant backend requires the `qdrant`
// build tag (which pulls in the external client). Without it, selecting qdrant
// is a configuration error rather than a silent fallback.
func openQdrant(_ context.Context, _ config.Knowledge, _ zerolog.Logger) (Store, error) {
	return nil, fmt.Errorf("knowledge: store_backend %q requires a build with the 'qdrant' tag", "qdrant")
}
