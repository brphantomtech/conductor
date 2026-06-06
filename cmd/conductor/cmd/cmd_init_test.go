package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/config"
	"github.com/conductor-sh/conductor/internal/harness"
)

// loadScaffold loads a scaffolded HARNESS.md with the placeholder secrets
// supplied via env, mirroring the "once secrets are supplied" flow.
func loadScaffold(t *testing.T, dir string) config.Config {
	t.Helper()
	t.Setenv("LINEAR_API_KEY", "lin_test")
	t.Setenv("OPENROUTER_API_KEY", "sk-or-test")
	res, err := harness.Load(filepath.Join(dir, "HARNESS.md"), config.LoadOptions{})
	require.NoError(t, err)
	return res.Config
}

func TestInitProfilesScaffoldAndValidate(t *testing.T) {
	cases := []struct {
		profile   initProfile
		wantFiles []string
	}{
		{profileLocal, []string{"HARNESS.md"}},
		{profileTeam, []string{"HARNESS.md", "docker-compose.yml", "Dockerfile"}},
		{profileCloud, []string{"HARNESS.md", "docker-compose.yml", "Dockerfile"}},
	}
	for _, tc := range cases {
		t.Run(string(tc.profile), func(t *testing.T) {
			dir := t.TempDir()
			var out bytes.Buffer
			require.NoError(t, runInit(&out, dir, tc.profile, false))

			for _, f := range tc.wantFiles {
				require.FileExists(t, filepath.Join(dir, f))
			}

			cfg := loadScaffold(t, dir)
			require.NoError(t, config.Validate(cfg),
				"scaffolded %s HARNESS.md must pass Validate with placeholder secrets", tc.profile)
		})
	}
}

func TestInitNoOverwriteWithoutForce(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "HARNESS.md")
	require.NoError(t, os.WriteFile(existing, []byte("original"), 0o644))

	err := runInit(&bytes.Buffer{}, dir, profileLocal, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "--force")

	data, rerr := os.ReadFile(existing)
	require.NoError(t, rerr)
	require.Equal(t, "original", string(data))
}

func TestInitForceOverwrites(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "HARNESS.md"), []byte("original"), 0o644))

	require.NoError(t, runInit(&bytes.Buffer{}, dir, profileLocal, true))
	data, rerr := os.ReadFile(filepath.Join(dir, "HARNESS.md"))
	require.NoError(t, rerr)
	require.NotEqual(t, "original", string(data))
}

func TestInitUnknownProfile(t *testing.T) {
	err := runInit(&bytes.Buffer{}, t.TempDir(), initProfile("bogus"), false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported")
}

func TestInitCommandRegistered(t *testing.T) {
	cmd := newInitCommand()
	require.Equal(t, "init", cmd.Name())
	require.NotNil(t, cmd.Flags().Lookup("profile"))
	require.NotNil(t, cmd.Flags().Lookup("force"))
}
