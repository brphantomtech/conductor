package validation

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPersistResultWritesPerTurnPath(t *testing.T) {
	dir := t.TempDir()
	vdir := filepath.Join(dir, ".conductor", "validation")

	res := ValidationPipelineResult{
		TurnIndex: 7,
		Checks: []ValidationResult{
			{CheckID: "lint", Name: "Lint", Severity: "error", Status: StatusFailed, ExitCode: 1, Output: "boom", DurationMS: 5},
		},
	}
	require.NoError(t, persistResult(vdir, res))

	path := filepath.Join(vdir, "7.json")
	require.FileExists(t, path)

	// Round-trips back to the result type.
	got, err := LoadResult(vdir, 7)
	require.NoError(t, err)
	require.Equal(t, res, got)
}

func TestPersistResultAtomicNoTempLeftBehind(t *testing.T) {
	vdir := t.TempDir()
	require.NoError(t, persistResult(vdir, ValidationPipelineResult{TurnIndex: 0, Checks: []ValidationResult{}}))

	entries, err := os.ReadDir(vdir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "0.json", entries[0].Name())
}

func TestPersistResultOverwritesSameTurn(t *testing.T) {
	vdir := t.TempDir()
	require.NoError(t, persistResult(vdir, ValidationPipelineResult{TurnIndex: 1,
		Checks: []ValidationResult{{CheckID: "a", Status: StatusPassed}}}))
	require.NoError(t, persistResult(vdir, ValidationPipelineResult{TurnIndex: 1,
		Checks: []ValidationResult{{CheckID: "b", Status: StatusFailed}}}))

	got, err := LoadResult(vdir, 1)
	require.NoError(t, err)
	require.Len(t, got.Checks, 1)
	require.Equal(t, "b", got.Checks[0].CheckID)

	entries, err := os.ReadDir(vdir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestPersistResultEmptyDirErrors(t *testing.T) {
	err := persistResult("", ValidationPipelineResult{})
	require.ErrorIs(t, err, ErrPersist)
}

func TestLoadResultMissingFile(t *testing.T) {
	_, err := LoadResult(t.TempDir(), 99)
	require.Error(t, err)
}
