package harness

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// mapEnv returns an EnvLookup backed by the supplied map. A missing key
// returns ok=false; a present key (even empty value) returns ok=true so
// the resolver can distinguish "unset" from "set but empty".
func mapEnv(m map[string]string) EnvLookup {
	return func(name string) (string, bool) {
		v, ok := m[name]
		return v, ok
	}
}

func TestResolve_FlagWinsOverEnv(t *testing.T) {
	dir := t.TempDir()
	flagFile := filepath.Join(dir, "from-flag.md")
	envFile := filepath.Join(dir, "from-env.md")
	require.NoError(t, os.WriteFile(flagFile, []byte("flag"), 0o644))
	require.NoError(t, os.WriteFile(envFile, []byte("env"), 0o644))

	path, src, err := Resolve(flagFile, mapEnv(map[string]string{EnvHarnessPath: envFile}), dir)
	require.NoError(t, err)
	require.Equal(t, flagFile, path)
	require.Equal(t, SourceFlag, src)
}

func TestResolve_EnvWinsOverCwd(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, "from-env.md")
	cwdFile := filepath.Join(dir, DefaultHarnessFilename)
	require.NoError(t, os.WriteFile(envFile, []byte("env"), 0o644))
	require.NoError(t, os.WriteFile(cwdFile, []byte("cwd"), 0o644))

	path, src, err := Resolve("", mapEnv(map[string]string{EnvHarnessPath: envFile}), dir)
	require.NoError(t, err)
	require.Equal(t, envFile, path)
	require.Equal(t, SourceEnv, src)
}

func TestResolve_CwdFallback(t *testing.T) {
	dir := t.TempDir()
	cwdFile := filepath.Join(dir, DefaultHarnessFilename)
	require.NoError(t, os.WriteFile(cwdFile, []byte("cwd"), 0o644))

	path, src, err := Resolve("", mapEnv(nil), dir)
	require.NoError(t, err)
	require.Equal(t, cwdFile, path)
	require.Equal(t, SourceCwd, src)
}

func TestResolve_NoFileReturnsMissing(t *testing.T) {
	dir := t.TempDir()

	_, src, err := Resolve("", mapEnv(nil), dir)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrMissingHarnessFile), "expected ErrMissingHarnessFile, got %v", err)
	require.Equal(t, SourceDocStore, src)
}

func TestResolve_FlagPointingAtMissingFileErrors(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist.md")

	_, _, err := Resolve(missing, mapEnv(nil), dir)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrMissingHarnessFile))
}

func TestResolve_EnvPointingAtMissingFileErrors(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "envmissing.md")

	_, _, err := Resolve("", mapEnv(map[string]string{EnvHarnessPath: missing}), dir)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrMissingHarnessFile))
}

func TestResolve_EmptyEnvFallsThrough(t *testing.T) {
	dir := t.TempDir()
	cwdFile := filepath.Join(dir, DefaultHarnessFilename)
	require.NoError(t, os.WriteFile(cwdFile, []byte("cwd"), 0o644))

	// Env set to empty string must fall through to cwd lookup.
	path, src, err := Resolve("", mapEnv(map[string]string{EnvHarnessPath: ""}), dir)
	require.NoError(t, err)
	require.Equal(t, cwdFile, path)
	require.Equal(t, SourceCwd, src)
}
