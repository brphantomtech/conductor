package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoad_ContainerBlockDecodes(t *testing.T) {
	t.Parallel()
	cfg, err := Load(LoadOptions{
		FrontMatter: map[string]any{
			"workspace": map[string]any{
				"root": "/tmp/ws",
				"container": map[string]any{
					"image":        "conductor/agent:latest",
					"network":      "bridge",
					"memory_limit": "512m",
					"cpu_limit":    "1.5",
					"extra_mounts": []any{
						map[string]any{"source": "/host/cache", "target": "/cache", "read_only": true},
					},
				},
			},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, cfg.Workspace.Container)
	c := cfg.Workspace.Container
	require.Equal(t, "conductor/agent:latest", c.Image)
	require.Equal(t, "bridge", c.Network)
	require.Equal(t, "512m", c.MemoryLimit)
	require.Equal(t, "1.5", c.CPULimit)
	require.Len(t, c.ExtraMounts, 1)
	require.Equal(t, "/host/cache", c.ExtraMounts[0].Source)
	require.Equal(t, "/cache", c.ExtraMounts[0].Target)
	require.True(t, c.ExtraMounts[0].ReadOnly)
}

func TestLoad_ContainerNetworkDefaultsToNone(t *testing.T) {
	t.Parallel()
	cfg, err := Load(LoadOptions{
		FrontMatter: map[string]any{
			"workspace": map[string]any{
				"container": map[string]any{
					"image": "conductor/agent:latest",
				},
			},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, cfg.Workspace.Container)
	require.Equal(t, ContainerNetworkNone, cfg.Workspace.Container.Network)
	require.Equal(t, "none", cfg.Workspace.Container.Network)
}

func TestLoad_AbsentContainerLeavesWorkspaceNil(t *testing.T) {
	t.Parallel()
	cfg, err := Load(LoadOptions{
		FrontMatter: map[string]any{
			"workspace": map[string]any{"root": "/tmp/ws"},
		},
	})
	require.NoError(t, err)
	require.Nil(t, cfg.Workspace.Container, "absent container block must leave workspace config as before")
	require.NotEmpty(t, cfg.Workspace.Root)
}

func TestLoad_DefaultsHaveNoContainer(t *testing.T) {
	t.Parallel()
	cfg, err := Load(LoadOptions{})
	require.NoError(t, err)
	require.Nil(t, cfg.Workspace.Container)
}
