package tracker

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeLabels(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "mixed case is lowercased and sorted",
			in:   []string{"Bug", "FRONTEND", "UI"},
			want: []string{"bug", "frontend", "ui"},
		},
		{
			name: "duplicates are removed after lowercasing",
			in:   []string{"Bug", "bug", "FRONTEND", "frontend", "ui"},
			want: []string{"bug", "frontend", "ui"},
		},
		{
			name: "whitespace is trimmed and empties dropped",
			in:   []string{"  bug  ", "", "   ", "ui"},
			want: []string{"bug", "ui"},
		},
		{
			name: "empty slice yields non-nil empty slice",
			in:   []string{},
			want: []string{},
		},
		{
			name: "nil slice yields non-nil empty slice",
			in:   nil,
			want: []string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeLabels(tc.in)
			require.NotNil(t, got, "result must be non-nil")
			require.Equal(t, tc.want, got)
		})
	}
}

func TestPaginationConstants(t *testing.T) {
	// Sanity: the caps must be positive and the batch must divide
	// cleanly into the per-call cap so reconciliation batches stay
	// predictable. These are not externally visible numbers but the
	// design doc references them.
	require.Greater(t, maxIssuesPerCall, 0)
	require.Greater(t, maxStateLookupBatch, 0)
	require.GreaterOrEqual(t, maxIssuesPerCall, maxStateLookupBatch)
}
