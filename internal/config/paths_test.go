package config

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizePath(t *testing.T) {
	t.Parallel()

	lookup := func(name string) (string, bool) {
		switch name {
		case "WORKSPACE_ROOT":
			return "/var/conductor", true
		case "DRIVE":
			return "C:", true
		}
		return "", false
	}

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty_returns_empty", "", ""},
		{"plain_relative", "relative/path", filepath.FromSlash("relative/path")},
		{"posix_absolute", "/var/lib/conductor", filepath.FromSlash("/var/lib/conductor")},
		{"var_expansion", "$WORKSPACE_ROOT/sub", filepath.FromSlash("/var/conductor/sub")},
		{"braced_var_expansion", "${WORKSPACE_ROOT}/sub", filepath.FromSlash("/var/conductor/sub")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizePath(tt.in, lookup)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestNormalizePath_TildeExpansion(t *testing.T) {
	t.Parallel()
	got, err := NormalizePath("~/foo", nil)
	require.NoError(t, err)
	require.NotEqual(t, "~/foo", got, "tilde must be expanded")
	require.True(t, strings.HasSuffix(got, filepath.FromSlash("foo")),
		"normalized path should end with foo, got %q", got)
}

func TestNormalizePath_TildeAlone(t *testing.T) {
	t.Parallel()
	got, err := NormalizePath("~", nil)
	require.NoError(t, err)
	require.NotEqual(t, "~", got)
}

func TestNormalizePath_TildeWithBackslashOnPosixPath(t *testing.T) {
	t.Parallel()
	got, err := NormalizePath("~\\foo", nil)
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(got, filepath.FromSlash("foo")))
}

func TestNormalizePath_PreservesNonTildePath(t *testing.T) {
	t.Parallel()
	in := "~user/somewhere" // ~user form is intentionally not expanded
	got, err := NormalizePath(in, nil)
	require.NoError(t, err)
	require.Equal(t, filepath.FromSlash(in), got)
}
