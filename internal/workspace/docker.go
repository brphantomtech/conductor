package workspace

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"
)

// dockerClient is the minimal Docker Engine surface the DockerRunner needs. It
// is injected so unit tests run against a fake — CI has no live Docker daemon
// (design.md "No Docker in CI"). The concrete production client lives behind a
// build tag (docker_live.go) and is constructed only when the daemon is present.
type dockerClient interface {
	// CreateContainer creates a stopped container from req and returns its id.
	CreateContainer(ctx context.Context, req DockerCreateRequest) (id string, err error)
	// StartContainer starts a previously created container.
	StartContainer(ctx context.Context, id string) error
	// WaitContainer blocks until the container exits and returns its code.
	WaitContainer(ctx context.Context, id string) (exitCode int, err error)
	// RemoveContainer force-removes the container (best effort cleanup).
	RemoveContainer(ctx context.Context, id string) error
}

// DockerCreateRequest is the resolved create payload the dockerClient turns
// into an Engine-API container create call. It is the translation of a
// ContainerSpec into Docker's create + host-config shape.
type DockerCreateRequest struct {
	// Image is the image reference to launch from.
	Image string
	// Cmd is the container command (argv).
	Cmd []string
	// WorkingDir is the in-container working directory (the workspace mount).
	WorkingDir string
	// Env is the container environment (KEY=VALUE).
	Env []string
	// NetworkMode is the Docker network mode ("none" by default).
	NetworkMode string
	// Binds are bind mounts in Docker "src:dst[:ro]" form.
	Binds []string
	// MemoryLimit is the Docker memory cap (e.g. "512m"); empty means none.
	MemoryLimit string
	// CPULimit is the Docker --cpus value (e.g. "1.5"); empty means none.
	CPULimit string
}

// DockerRunner is the production ContainerRunner: it launches an agent run in a
// Docker container with the workspace mounted as the only writable mount,
// resource limits applied, and networking disabled by default (SPEC §21.3). It
// talks to Docker through an injected dockerClient so it is testable without a
// daemon.
type DockerRunner struct {
	client dockerClient
	log    zerolog.Logger
}

// DockerRunnerOption configures a DockerRunner at construction time.
type DockerRunnerOption func(*DockerRunner)

// WithDockerLogger sets the structured logger. The default is a no-op logger.
func WithDockerLogger(l zerolog.Logger) DockerRunnerOption {
	return func(r *DockerRunner) { r.log = l }
}

// withDockerClient injects the Docker client (test seam; production wiring uses
// NewDockerRunner with the live client built behind the docker_live tag).
func withDockerClient(c dockerClient) DockerRunnerOption {
	return func(r *DockerRunner) {
		if c != nil {
			r.client = c
		}
	}
}

// newDockerRunner constructs a DockerRunner around an injected client. The
// exported NewDockerRunner (docker_live.go, build-tagged) wires the real
// Engine-API client; tests use this constructor with a fake.
func newDockerRunner(client dockerClient, opts ...DockerRunnerOption) *DockerRunner {
	r := &DockerRunner{
		client: client,
		log:    zerolog.Nop(),
	}
	for _, o := range opts {
		if o != nil {
			o(r)
		}
	}
	return r
}

// Run launches the container described by spec, waits for it to exit, and
// returns the exit code. The container is force-removed after exit regardless
// of outcome. A launch/attach failure is wrapped with ErrContainerLaunch; a
// non-zero process exit is returned as the code with a nil error.
func (r *DockerRunner) Run(ctx context.Context, spec ContainerSpec) (int, error) {
	if r.client == nil {
		return -1, fmt.Errorf("workspace: docker runner has no client: %w", ErrContainerLaunch)
	}
	req := dockerCreateRequest(spec)

	id, err := r.client.CreateContainer(ctx, req)
	if err != nil {
		return -1, fmt.Errorf("workspace: create container from %q: %w: %w", spec.Image, err, ErrContainerLaunch)
	}
	defer func() {
		if rmErr := r.client.RemoveContainer(context.WithoutCancel(ctx), id); rmErr != nil {
			r.log.Warn().Err(rmErr).Str("container_id", id).Msg("workspace: remove container")
		}
	}()

	if err := r.client.StartContainer(ctx, id); err != nil {
		return -1, fmt.Errorf("workspace: start container %s: %w: %w", id, err, ErrContainerLaunch)
	}

	code, err := r.client.WaitContainer(ctx, id)
	if err != nil {
		return -1, fmt.Errorf("workspace: wait container %s: %w: %w", id, err, ErrContainerLaunch)
	}
	return code, nil
}

// dockerCreateRequest translates a ContainerSpec into the Docker create
// payload: the workspace mount becomes the working dir, the network defaults to
// none, and all mounts are rendered as Docker bind strings.
func dockerCreateRequest(spec ContainerSpec) DockerCreateRequest {
	network := spec.Network
	if network == "" {
		network = "none"
	}
	mounts := spec.Mounts()
	binds := make([]string, 0, len(mounts))
	for _, m := range mounts {
		binds = append(binds, bindString(m))
	}
	return DockerCreateRequest{
		Image:       spec.Image,
		Cmd:         spec.Cmd,
		WorkingDir:  spec.WorkspaceMount.Target,
		Env:         spec.Env,
		NetworkMode: network,
		Binds:       binds,
		MemoryLimit: spec.MemoryLimit,
		CPULimit:    spec.CPULimit,
	}
}

// bindString renders a ContainerMount as a Docker "src:dst[:ro]" bind.
func bindString(m ContainerMount) string {
	b := m.Source + ":" + m.Target
	if m.ReadOnly {
		b += ":ro"
	}
	return b
}
