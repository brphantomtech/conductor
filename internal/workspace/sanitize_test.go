package workspace

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSanitizeKey(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"alphanumeric and dash kept", "ABC-123", "ABC-123"},
		{"slash replaced", "feature/login", "feature_login"},
		{"space replaced", "a b", "a_b"},
		{"symbols replaced", "x@#y", "x__y"},
		{"dots kept (traversal handled by confinement)", "..", ".."},
		{"allowed punctuation kept", "keep.dot_-", "keep.dot_-"},
		{"empty stays empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, SanitizeKey(tt.in))
		})
	}
}

func TestWithinRoot(t *testing.T) {
	root := filepath.Clean(t.TempDir())

	require.True(t, withinRoot(root, filepath.Join(root, "sub")))
	require.True(t, withinRoot(root, filepath.Join(root, "a", "b")))

	require.False(t, withinRoot(root, root), "root itself is not within root")
	require.False(t, withinRoot(root, filepath.Dir(root)), "parent is not within root")
	require.False(t, withinRoot(root, filepath.Join(root, "..", "evil")), "traversal escapes root")
	require.False(t, withinRoot("", filepath.Join(root, "sub")), "empty root is never satisfied")
}
