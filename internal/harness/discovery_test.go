package harness

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/config"
)

// mapEnv returns a lookup func backed by the supplied map. A missing key
// returns ok=false; a present key (even an empty value) returns ok=true so
// ResolvePath can distinguish "unset" from "set but empty".
func mapEnv(m map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		v, ok := m[name]
		return v, ok
	}
}

func TestResolvePath_FlagWinsOverEnv(t *testing.T) {
	dir := t.TempDir()
	flagFile := filepath.Join(dir, "from-flag.md")
	envFile := filepath.Join(dir, "from-env.md")

	got := ResolvePath(flagFile, mapEnv(map[string]string{HarnessPathEnvVar: envFile}))
	require.Equal(t, flagFile, got)
}

func TestResolvePath_EnvWinsOverDefault(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, "from-env.md")

	got := ResolvePath("", mapEnv(map[string]string{HarnessPathEnvVar: envFile}))
	require.Equal(t, envFile, got)
}

func TestResolvePath_DefaultFallback(t *testing.T) {
	got := ResolvePath("", mapEnv(nil))
	require.Equal(t, config.DefaultHarnessPath, got)
}

func TestResolvePath_EmptyEnvFallsThrough(t *testing.T) {
	// An env var set to the empty string must not win; resolution falls
	// through to the working-directory default.
	got := ResolvePath("", mapEnv(map[string]string{HarnessPathEnvVar: ""}))
	require.Equal(t, config.DefaultHarnessPath, got)
}

func TestResolvePath_NilLookupReturnsAPath(t *testing.T) {
	// With no flag and a nil lookup, ResolvePath consults os.LookupEnv
	// without mutating the process environment; it always returns a path.
	require.NotEmpty(t, ResolvePath("", nil))
}
