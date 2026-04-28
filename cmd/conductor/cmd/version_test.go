package cmd

import (
	"bytes"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVersionCommandPrintsBuildMetadata(t *testing.T) {
	t.Parallel()

	root := NewRootCommand()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"version"})

	require.NoError(t, root.Execute())

	out := stdout.String()
	require.Contains(t, out, "conductor ")
	require.Contains(t, out, runtime.Version())
	require.Contains(t, out, runtime.GOOS)
	require.Contains(t, out, runtime.GOARCH)
}

func TestReadVersionInfoHasGoAndPlatform(t *testing.T) {
	t.Parallel()
	v := ReadVersionInfo()
	require.Equal(t, runtime.Version(), v.GoVersion)
	require.Equal(t, runtime.GOOS, v.OS)
	require.Equal(t, runtime.GOARCH, v.Arch)
	require.NotEmpty(t, v.Version)
}
