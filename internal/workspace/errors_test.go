package workspace

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestErrorIdentifiersMatchSpec pins the SPEC §23.4 identifier strings.
func TestErrorIdentifiersMatchSpec(t *testing.T) {
	require.Equal(t, "workspace_creation_failed", ErrWorkspaceCreationFailed.Error())
	require.Equal(t, "hook_failed", ErrHookFailed.Error())
}

func TestJoinErr(t *testing.T) {
	base := errors.New("boom")

	joined := joinErr(base, ErrHookFailed)
	require.True(t, errors.Is(joined, ErrHookFailed))
	require.True(t, errors.Is(joined, base))

	require.True(t, errors.Is(joinErr(nil, ErrHookFailed), ErrHookFailed))
}

func TestWrapCreateClassifies(t *testing.T) {
	err := wrapCreate("do thing", ErrUnsafePath)
	require.True(t, errors.Is(err, ErrWorkspaceCreationFailed))
	require.True(t, errors.Is(err, ErrUnsafePath))
	require.Contains(t, err.Error(), "do thing")
}
