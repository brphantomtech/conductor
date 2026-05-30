package harness_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/config"
	"github.com/conductor-sh/conductor/internal/harness"
)

// validHarnessSrc is the minimum HARNESS.md that satisfies both parser and
// validator with the default routing pipeline {planner, coder, verifier}.
const validHarnessSrc = `---
project:
  id: demo
tracker:
  kind: linear
  api_key: token-xyz
  project_slug: demo-team
providers:
  default:
    provider: openrouter
    api_key: sk-test
---

## planner

Plan {{ issue.identifier }}.

## coder

Implement {{ issue.title }}.

## verifier

Verify {{ issue.identifier }}.
`

func writeHarness(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "HARNESS.md")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func TestLoad_HappyPath(t *testing.T) {
	t.Parallel()

	path := writeHarness(t, validHarnessSrc)
	res, err := harness.Load(path, config.LoadOptions{})
	require.NoError(t, err)

	require.NotNil(t, res.Definition)
	require.Equal(t, path, res.Definition.Source)
	require.Equal(t, "demo", res.Config.Project.ID)
	require.Equal(t, "openrouter", res.Config.Providers.Default.Provider)
	require.False(t, res.Validation.HasErrors())
}

func TestLoad_MissingFile(t *testing.T) {
	t.Parallel()

	_, err := harness.Load(filepath.Join(t.TempDir(), "missing.md"), config.LoadOptions{})
	require.Error(t, err)
	require.True(t, errors.Is(err, harness.ErrMissingHarnessFile))
}

func TestLoad_TemplateRenderFailure(t *testing.T) {
	t.Parallel()

	path := writeHarness(t, `---
project: { id: demo }
---

## coder

Hello {{ totally_unknown_variable }}
`)

	res, err := harness.Load(path, config.LoadOptions{})
	require.Error(t, err)
	require.True(t, errors.Is(err, harness.ErrTemplateRender),
		"unknown variable must classify as ErrTemplateRender, got %v", err)
	require.NotNil(t, res.Definition,
		"validation failures must still return the parsed Definition for diagnostic display")
	require.True(t, res.Validation.HasErrors())
}

func TestLoad_PipelineRoleMissing(t *testing.T) {
	t.Parallel()

	// Default routing pipeline expects planner+coder+verifier; the
	// harness only ships coder.
	path := writeHarness(t, `---
project: { id: demo }
tracker:
  kind: linear
  api_key: t
  project_slug: x
providers:
  default:
    provider: openrouter
    api_key: sk
---

## coder

Implement.
`)

	res, err := harness.Load(path, config.LoadOptions{})
	require.Error(t, err)
	require.True(t, errors.Is(err, harness.ErrHarnessParse))

	missing := map[string]bool{}
	for _, iss := range res.Validation.Issues {
		missing[iss.Role] = true
	}
	require.True(t, missing["planner"])
	require.True(t, missing["verifier"])
}

func TestLoadBytes_RoundTrip(t *testing.T) {
	t.Parallel()

	res, err := harness.LoadBytes([]byte(validHarnessSrc), config.LoadOptions{})
	require.NoError(t, err)
	require.Equal(t, "", res.Definition.Source,
		"LoadBytes must leave Source empty so callers can populate it explicitly")
	require.Equal(t, "demo", res.Config.Project.ID)
}

func TestResolvePath_Precedence(t *testing.T) {
	t.Parallel()

	// envSet simulates $CONDUCTOR_HARNESS_PATH = "from-env" being present.
	envSet := func(string) (string, bool) { return "from-env", true }
	// envUnset simulates the variable being absent.
	envUnset := func(string) (string, bool) { return "", false }
	// envEmpty simulates the variable set to the empty string, which SPEC
	// §5.1 treats the same as unset (fall through to the cwd default).
	envEmpty := func(string) (string, bool) { return "", true }

	t.Run("flag wins over env and default", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "from-flag", harness.ResolvePath("from-flag", envSet))
	})

	t.Run("env used when flag empty", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, "from-env", harness.ResolvePath("", envSet))
	})

	t.Run("default used when flag and env empty", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, config.DefaultHarnessPath, harness.ResolvePath("", envUnset))
		require.Equal(t, config.DefaultHarnessPath, harness.ResolvePath("", envEmpty))
	})
}

func TestLoad_FrontMatterOverridesDefaults(t *testing.T) {
	t.Parallel()

	path := writeHarness(t, `---
project: { id: demo }
polling:
  interval_ms: 1234
---

## coder

Hi.
`)

	// Validation will report missing planner/verifier templates against the
	// default pipeline, but the typed Config still decodes; the test cares
	// only that front-matter precedence is honored end-to-end.
	res, _ := harness.Load(path, config.LoadOptions{})
	require.Equal(t, 1234, res.Config.Polling.IntervalMS,
		"front matter values must flow through into the typed Config (SPEC §6.1)")
}
