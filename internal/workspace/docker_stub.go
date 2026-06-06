//go:build !docker_live

package workspace

import (
	"context"
	"fmt"
)

// NewDockerRunner constructs a production DockerRunner. Without the docker_live
// build tag, no concrete Engine-API client is compiled in, so the returned
// runner fails fast on Run with ErrContainerLaunch. Container-isolation unit
// tests inject a fake client via newDockerRunner; a real daemon is wired by
// building with -tags docker_live (see docker_live.go).
func NewDockerRunner(opts ...DockerRunnerOption) *DockerRunner {
	return newDockerRunner(unbuiltDockerClient{}, opts...)
}

// unbuiltDockerClient is the placeholder client used when the docker_live tag
// is absent. Every call reports that container support was not built in.
type unbuiltDockerClient struct{}

func (unbuiltDockerClient) CreateContainer(context.Context, DockerCreateRequest) (string, error) {
	return "", fmt.Errorf("docker support not built in (build with -tags docker_live)")
}
func (unbuiltDockerClient) StartContainer(context.Context, string) error {
	return fmt.Errorf("docker support not built in (build with -tags docker_live)")
}
func (unbuiltDockerClient) WaitContainer(context.Context, string) (int, error) {
	return -1, fmt.Errorf("docker support not built in (build with -tags docker_live)")
}
func (unbuiltDockerClient) RemoveContainer(context.Context, string) error {
	return fmt.Errorf("docker support not built in (build with -tags docker_live)")
}
