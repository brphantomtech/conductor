package workspace

import (
	"context"
	"errors"
	"fmt"

	"github.com/conductor-sh/conductor/internal/config"
)

// SPEC §23 / §21.3 — container-isolation classifications owned by the
// workspace layer. The sentinel strings match the SPEC identifiers verbatim so
// they appear unchanged in audit logs and API responses.
var (
	// ErrContainerConfig classifies an invalid or incomplete
	// workspace.container block (for example a missing image).
	ErrContainerConfig = errors.New("container_config_invalid")

	// ErrContainerLaunch classifies any failure creating, starting, or
	// attaching to the agent container.
	ErrContainerLaunch = errors.New("container_launch_failed")
)

// ContainerRunner launches an agent run inside an isolated container with the
// workspace mounted, resource limits applied, and networking disabled by
// default (SPEC §14.3, §21.3). It is the container-isolation sibling of the
// subprocess AgentCommand path; the workspace Manager selects between them
// based on whether workspace.container is configured.
//
// The interface is defined at the consumer (the Manager) per the project
// interface convention: there is one production implementation (DockerRunner)
// plus a fake in tests.
type ContainerRunner interface {
	// Run launches the container described by spec, blocks until the agent
	// process exits, and returns its exit code. A non-nil error classifies a
	// launch/attach failure (ErrContainerLaunch), distinct from a non-zero
	// process exit which is reported via the returned code with a nil error.
	Run(ctx context.Context, spec ContainerSpec) (exitCode int, err error)
}

// ContainerMount is one host→container bind mount in a ContainerSpec.
type ContainerMount struct {
	// Source is the host path to mount.
	Source string
	// Target is the in-container mount path.
	Target string
	// ReadOnly mounts the bind read-only when true.
	ReadOnly bool
}

// ContainerSpec is the resolved, runtime description of one container-isolated
// agent run. It is derived from config.Container plus the live Workspace and
// the host tool socket, and is the single input to a ContainerRunner.
type ContainerSpec struct {
	// Image is the container image to launch from.
	Image string
	// Cmd is the agent command (argv) executed inside the container.
	Cmd []string
	// WorkspaceMount is the workspace bind mount — the only writable mount
	// (SPEC §21.3). Its Target is also the container working directory.
	WorkspaceMount ContainerMount
	// SocketMount is the host tool socket mounted into the container so the
	// in-container agent reaches Conductor while networking is disabled.
	SocketMount ContainerMount
	// ExtraMounts are opt-in additional binds from config.Container.
	ExtraMounts []ContainerMount
	// Network is the Docker network mode (default config.ContainerNetworkNone).
	Network string
	// MemoryLimit is the Docker memory cap (e.g. "512m"); empty means none.
	MemoryLimit string
	// CPULimit is the Docker --cpus value (e.g. "1.5"); empty means none.
	CPULimit string
	// Env is the environment passed to the container as KEY=VALUE strings.
	Env []string
}

// Mounts returns every bind mount the spec requires, in launch order:
// workspace first (the writable mount), then the tool socket, then any
// configured extra mounts.
func (s ContainerSpec) Mounts() []ContainerMount {
	mounts := make([]ContainerMount, 0, 2+len(s.ExtraMounts))
	mounts = append(mounts, s.WorkspaceMount)
	if s.SocketMount.Source != "" {
		mounts = append(mounts, s.SocketMount)
	}
	mounts = append(mounts, s.ExtraMounts...)
	return mounts
}

// buildContainerSpec resolves a config.Container plus a live workspace and the
// host tool socket into a ContainerSpec. The workspace is mounted read/write at
// containerWorkdir as the only writable mount; the socket is mounted read/write
// at containerSocketPath; extra mounts are appended; the network defaults to
// none (SPEC §21.3). The agent argv (name + args) runs inside the container.
func buildContainerSpec(c *config.Container, ws *Workspace, socketPath string, name string, args []string) (ContainerSpec, error) {
	if c == nil {
		return ContainerSpec{}, fmt.Errorf("workspace: nil container config: %w", ErrContainerConfig)
	}
	if c.Image == "" {
		return ContainerSpec{}, fmt.Errorf("workspace: container image is required: %w", ErrContainerConfig)
	}
	if ws == nil || ws.Path == "" {
		return ContainerSpec{}, fmt.Errorf("workspace: container run without workspace: %w", ErrUnsafePath)
	}

	network := c.Network
	if network == "" {
		network = config.ContainerNetworkNone
	}

	extra := make([]ContainerMount, 0, len(c.ExtraMounts))
	for _, m := range c.ExtraMounts {
		extra = append(extra, ContainerMount{Source: m.Source, Target: m.Target, ReadOnly: m.ReadOnly})
	}

	spec := ContainerSpec{
		Image: c.Image,
		Cmd:   append([]string{name}, args...),
		WorkspaceMount: ContainerMount{
			Source:   ws.Path,
			Target:   containerWorkdir,
			ReadOnly: false,
		},
		ExtraMounts: extra,
		Network:     network,
		MemoryLimit: c.MemoryLimit,
		CPULimit:    c.CPULimit,
		Env:         []string{containerSocketEnv + "=" + containerSocketPath},
	}
	if socketPath != "" {
		spec.SocketMount = ContainerMount{
			Source:   socketPath,
			Target:   containerSocketPath,
			ReadOnly: false,
		}
	}
	return spec, nil
}

// Container layout constants for an isolated agent run.
const (
	// containerWorkdir is where the workspace is mounted and the agent runs.
	containerWorkdir = "/workspace"
	// containerSocketPath is where the host tool socket is mounted inside the
	// container.
	containerSocketPath = "/run/conductor/tool.sock"
	// containerSocketEnv tells the in-container agent where to reach Conductor.
	containerSocketEnv = "CONDUCTOR_TOOL_SOCKET"
)
