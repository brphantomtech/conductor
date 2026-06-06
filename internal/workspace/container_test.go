package workspace

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/config"
)

// fakeDockerClient records the create request and returns scripted results.
type fakeDockerClient struct {
	mu         sync.Mutex
	lastCreate DockerCreateRequest
	started    []string
	removed    []string
	createID   string
	createErr  error
	startErr   error
	waitCode   int
	waitErr    error
}

func (f *fakeDockerClient) CreateContainer(_ context.Context, req DockerCreateRequest) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastCreate = req
	if f.createErr != nil {
		return "", f.createErr
	}
	id := f.createID
	if id == "" {
		id = "fake-container-id"
	}
	return id, nil
}

func (f *fakeDockerClient) StartContainer(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = append(f.started, id)
	return f.startErr
}

func (f *fakeDockerClient) WaitContainer(_ context.Context, _ string) (int, error) {
	return f.waitCode, f.waitErr
}

func (f *fakeDockerClient) RemoveContainer(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, id)
	return nil
}

func TestDockerRunner_LaunchesWithMountsAndLimits(t *testing.T) {
	t.Parallel()
	fc := &fakeDockerClient{waitCode: 0}
	runner := newDockerRunner(fc)

	spec := ContainerSpec{
		Image:          "conductor/agent:latest",
		Cmd:            []string{"agent", "run"},
		WorkspaceMount: ContainerMount{Source: "/host/ws", Target: containerWorkdir},
		SocketMount:    ContainerMount{Source: "/host/tool.sock", Target: containerSocketPath},
		ExtraMounts:    []ContainerMount{{Source: "/host/cache", Target: "/cache", ReadOnly: true}},
		MemoryLimit:    "512m",
		CPULimit:       "1.5",
	}

	code, err := runner.Run(context.Background(), spec)
	require.NoError(t, err)
	require.Equal(t, 0, code)

	req := fc.lastCreate
	require.Equal(t, "conductor/agent:latest", req.Image)
	require.Equal(t, []string{"agent", "run"}, req.Cmd)
	require.Equal(t, containerWorkdir, req.WorkingDir)
	// network defaults to none (air-gapped).
	require.Equal(t, "none", req.NetworkMode)
	require.Equal(t, "512m", req.MemoryLimit)
	require.Equal(t, "1.5", req.CPULimit)
	// Workspace is the first (writable) bind; socket and extra follow.
	require.Equal(t, "/host/ws:"+containerWorkdir, req.Binds[0])
	require.Contains(t, req.Binds, "/host/tool.sock:"+containerSocketPath)
	require.Contains(t, req.Binds, "/host/cache:/cache:ro")
	// container started and cleaned up.
	require.Equal(t, []string{"fake-container-id"}, fc.started)
	require.Equal(t, []string{"fake-container-id"}, fc.removed)
}

func TestDockerRunner_NonZeroExit(t *testing.T) {
	t.Parallel()
	fc := &fakeDockerClient{waitCode: 7}
	runner := newDockerRunner(fc)
	code, err := runner.Run(context.Background(), ContainerSpec{
		Image:          "img",
		WorkspaceMount: ContainerMount{Source: "/ws", Target: containerWorkdir},
	})
	require.NoError(t, err)
	require.Equal(t, 7, code)
}

func TestDockerRunner_CreateFailureClassified(t *testing.T) {
	t.Parallel()
	fc := &fakeDockerClient{createErr: errors.New("daemon down")}
	runner := newDockerRunner(fc)
	_, err := runner.Run(context.Background(), ContainerSpec{
		Image:          "img",
		WorkspaceMount: ContainerMount{Source: "/ws", Target: containerWorkdir},
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrContainerLaunch))
}

func TestBuildContainerSpec_NetworkDefaultsNone(t *testing.T) {
	t.Parallel()
	ws := &Workspace{Path: "/root/ABC-1"}
	spec, err := buildContainerSpec(&config.Container{Image: "img"}, ws, "/sock", "agent", []string{"run"})
	require.NoError(t, err)
	require.Equal(t, config.ContainerNetworkNone, spec.Network)
	require.Equal(t, "/root/ABC-1", spec.WorkspaceMount.Source)
	require.Equal(t, containerWorkdir, spec.WorkspaceMount.Target)
	require.False(t, spec.WorkspaceMount.ReadOnly, "workspace mount must be writable")
	require.Equal(t, "/sock", spec.SocketMount.Source)
	require.Equal(t, []string{"agent", "run"}, spec.Cmd)
	require.Contains(t, spec.Env, containerSocketEnv+"="+containerSocketPath)
}

func TestBuildContainerSpec_RequiresImage(t *testing.T) {
	t.Parallel()
	_, err := buildContainerSpec(&config.Container{}, &Workspace{Path: "/ws"}, "", "agent", nil)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrContainerConfig))
}

func TestBuildContainerSpec_HonorsExplicitNetwork(t *testing.T) {
	t.Parallel()
	spec, err := buildContainerSpec(&config.Container{Image: "img", Network: "bridge"}, &Workspace{Path: "/ws"}, "", "a", nil)
	require.NoError(t, err)
	require.Equal(t, "bridge", spec.Network)
}

func TestContainerSpec_MountsOrder(t *testing.T) {
	t.Parallel()
	spec := ContainerSpec{
		WorkspaceMount: ContainerMount{Source: "/ws", Target: containerWorkdir},
		SocketMount:    ContainerMount{Source: "/sock", Target: containerSocketPath},
		ExtraMounts:    []ContainerMount{{Source: "/a", Target: "/b"}},
	}
	mounts := spec.Mounts()
	require.Len(t, mounts, 3)
	require.Equal(t, "/ws", mounts[0].Source, "workspace mount must come first")
}

func TestNewDockerRunner_StubFailsFast(t *testing.T) {
	t.Parallel()
	// Without the docker_live build tag, the default runner has the placeholder
	// client and must fail fast with ErrContainerLaunch rather than hang.
	runner := NewDockerRunner(WithDockerLogger(zerolog.Nop()))
	_, err := runner.Run(context.Background(), ContainerSpec{
		Image:          "img",
		WorkspaceMount: ContainerMount{Source: "/ws", Target: containerWorkdir},
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrContainerLaunch))
}

func TestBindString(t *testing.T) {
	t.Parallel()
	require.Equal(t, "/a:/b", bindString(ContainerMount{Source: "/a", Target: "/b"}))
	require.Equal(t, "/a:/b:ro", bindString(ContainerMount{Source: "/a", Target: "/b", ReadOnly: true}))
	require.True(t, strings.HasSuffix(bindString(ContainerMount{Source: "/a", Target: "/b", ReadOnly: true}), ":ro"))
}
