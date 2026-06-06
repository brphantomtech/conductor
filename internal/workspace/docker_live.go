//go:build docker_live

package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"time"
)

// NewDockerRunner constructs a production DockerRunner wired to the local
// Docker daemon over its default socket/named pipe. Built only with the
// docker_live tag so CI (which has no daemon) compiles the stub instead.
func NewDockerRunner(opts ...DockerRunnerOption) *DockerRunner {
	return newDockerRunner(newEngineClient(), opts...)
}

// engineClient is a minimal Docker Engine-API client over the local
// socket/named pipe using stdlib net/http only — no SDK dependency.
type engineClient struct {
	http *http.Client
	host string // base URL host placeholder for the unix transport
}

func newEngineClient() *engineClient {
	dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		d := net.Dialer{Timeout: 5 * time.Second}
		if runtime.GOOS == "windows" {
			return d.DialContext(ctx, "tcp", "127.0.0.1:2375")
		}
		sock := os.Getenv("DOCKER_HOST")
		if sock == "" {
			sock = "/var/run/docker.sock"
		}
		return d.DialContext(ctx, "unix", sock)
	}
	return &engineClient{
		http: &http.Client{Transport: &http.Transport{DialContext: dial}},
		host: "http://docker",
	}
}

func (c *engineClient) CreateContainer(ctx context.Context, req DockerCreateRequest) (string, error) {
	body := map[string]any{
		"Image":      req.Image,
		"Cmd":        req.Cmd,
		"WorkingDir": req.WorkingDir,
		"Env":        req.Env,
		"HostConfig": hostConfig(req),
	}
	var out struct {
		ID string `json:"Id"`
	}
	if err := c.do(ctx, http.MethodPost, "/containers/create", body, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

func hostConfig(req DockerCreateRequest) map[string]any {
	hc := map[string]any{
		"Binds":       req.Binds,
		"NetworkMode": req.NetworkMode,
		"AutoRemove":  false,
	}
	if req.MemoryLimit != "" {
		if n, err := parseMemory(req.MemoryLimit); err == nil {
			hc["Memory"] = n
		}
	}
	if req.CPULimit != "" {
		if f, err := strconv.ParseFloat(req.CPULimit, 64); err == nil {
			// NanoCPUs is CPUs * 1e9.
			hc["NanoCpus"] = int64(f * 1e9)
		}
	}
	return hc
}

func (c *engineClient) StartContainer(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/containers/"+id+"/start", nil, nil)
}

func (c *engineClient) WaitContainer(ctx context.Context, id string) (int, error) {
	var out struct {
		StatusCode int `json:"StatusCode"`
	}
	if err := c.do(ctx, http.MethodPost, "/containers/"+id+"/wait", nil, &out); err != nil {
		return -1, err
	}
	return out.StatusCode, nil
}

func (c *engineClient) RemoveContainer(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/containers/"+id+"?force=true", nil, nil)
}

func (c *engineClient) do(ctx context.Context, method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("workspace: marshal docker request: %w", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.host+path, rdr)
	if err != nil {
		return fmt.Errorf("workspace: build docker request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("workspace: docker request %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("workspace: docker %s %s: status %d: %s", method, path, resp.StatusCode, string(msg))
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil && err != io.EOF {
			return fmt.Errorf("workspace: decode docker response: %w", err)
		}
	}
	return nil
}

// parseMemory parses Docker memory suffixes (b, k, m, g) into bytes.
func parseMemory(s string) (int64, error) {
	if s == "" {
		return 0, fmt.Errorf("empty memory limit")
	}
	mult := int64(1)
	last := s[len(s)-1]
	num := s
	switch last {
	case 'b', 'B':
		num = s[:len(s)-1]
	case 'k', 'K':
		mult, num = 1024, s[:len(s)-1]
	case 'm', 'M':
		mult, num = 1024*1024, s[:len(s)-1]
	case 'g', 'G':
		mult, num = 1024*1024*1024, s[:len(s)-1]
	}
	n, err := strconv.ParseInt(num, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse memory %q: %w", s, err)
	}
	return n * mult, nil
}
