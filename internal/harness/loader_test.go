package harness

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte(body), 0o644))
	return p
}

const validHarnessYAML = `---
project:
  id: demo
tracker:
  kind: linear
  api_key: secret
  project_slug: team
providers:
  default:
    provider: anthropic
    api_key: ank
routing:
  pipeline: [planner, coder, verifier]
---

## planner

plan

## coder

code

## verifier

verify
`

func TestLoad_EnvOnlyFlow(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "HARNESS.md", validHarnessYAML)

	res, err := Load(LoadOptions{
		Env: mapEnv(map[string]string{EnvHarnessPath: path}),
		Cwd: dir,
	})
	require.NoError(t, err)
	require.Equal(t, SourceEnv, res.Source)
	require.Equal(t, path, res.Path)
	require.Equal(t, "demo", res.Config.Project.ID)
	require.Equal(t, "linear", res.Config.Tracker.Kind)
	require.Contains(t, res.Definition.PromptTemplates, "coder")
}

func TestLoad_FlagFlow(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "any-name.md", validHarnessYAML)

	res, err := Load(LoadOptions{
		Flag: path,
		Env:  mapEnv(nil),
		Cwd:  dir,
	})
	require.NoError(t, err)
	require.Equal(t, SourceFlag, res.Source)
	require.Equal(t, path, res.Path)
}

func TestLoad_ValidationFailureShortCircuits(t *testing.T) {
	dir := t.TempDir()
	// Missing project.id and unsupported provider.
	body := `---
tracker:
  kind: linear
  api_key: secret
  project_slug: team
providers:
  default:
    provider: nope
---

## planner

p

## coder

c

## verifier

v
`
	path := writeFile(t, dir, "HARNESS.md", body)

	res, err := Load(LoadOptions{
		Flag: path,
		Env:  mapEnv(nil),
		Cwd:  dir,
	})
	require.Error(t, err)
	// Definition and Path should still be populated so the CLI can name
	// the file in its error output.
	require.NotNil(t, res.Definition)
	require.Equal(t, path, res.Path)
	require.True(t, errors.Is(err, ErrProjectIDMissing))
	require.True(t, errors.Is(err, ErrProviderUnsupported))
}

func TestLoad_CLIFlagsOverrideFrontMatter(t *testing.T) {
	dir := t.TempDir()
	// Front matter sets polling.interval_ms to 5000.
	body := `---
project:
  id: demo
tracker:
  kind: linear
  api_key: secret
  project_slug: team
providers:
  default:
    provider: anthropic
    api_key: ank
routing:
  pipeline: [planner, coder, verifier]
polling:
  interval_ms: 5000
---

## planner

p

## coder

c

## verifier

v
`
	path := writeFile(t, dir, "HARNESS.md", body)

	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	fs.Int("polling-interval", 0, "")
	require.NoError(t, fs.Parse([]string{"--polling-interval=1000"}))

	res, err := Load(LoadOptions{
		Flag:     path,
		Env:      mapEnv(nil),
		Cwd:      dir,
		CLIFlags: fs,
	})
	require.NoError(t, err)
	require.Equal(t, 1000, res.Config.Polling.IntervalMS)
}

func TestLoad_MissingFileReturnsMissingHarness(t *testing.T) {
	dir := t.TempDir()
	res, err := Load(LoadOptions{
		Env: mapEnv(nil),
		Cwd: dir,
	})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrMissingHarnessFile))
	require.Nil(t, res.Definition)
}
