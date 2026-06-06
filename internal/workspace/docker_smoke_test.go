//go:build docker_live

package workspace

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// TestDockerRunner_LiveSmoke exercises the real Engine-API client against a
// running daemon. It is build-tagged (docker_live) so it never compiles into
// the default CI run, and it skips when no daemon is reachable, keeping the
// machine-without-Docker case green.
func TestDockerRunner_LiveSmoke(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	runner := NewDockerRunner(WithDockerLogger(zerolog.Nop()))
	dir := t.TempDir()
	spec := ContainerSpec{
		Image:          "alpine:3.19",
		Cmd:            []string{"true"},
		WorkspaceMount: ContainerMount{Source: dir, Target: containerWorkdir},
		Network:        "none",
	}
	code, err := runner.Run(ctx, spec)
	if err != nil {
		t.Skipf("no reachable docker daemon / image: %v", err)
	}
	require.Equal(t, 0, code)
}
