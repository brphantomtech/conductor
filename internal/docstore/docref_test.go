package docstore

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDocRefJSONRoundTrip(t *testing.T) {
	in := DocRef{
		ID:           DocRefID("specs", "a/b.md"),
		Title:        "Design",
		StoreID:      "specs",
		PathOrID:     "a/b.md",
		ContentHash:  HashContent([]byte("hello")),
		Tags:         []string{"spec", "design"},
		Embedding:    []float32{0.1, 0.2, 0.3},
		LastSyncedAt: time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC),
	}
	data, err := json.Marshal(in)
	require.NoError(t, err)

	var out DocRef
	require.NoError(t, json.Unmarshal(data, &out))
	assert.Equal(t, in, out)
}

func TestDocRefIDStable(t *testing.T) {
	a := DocRefID("specs", "a/b.md")
	b := DocRefID("specs", "a/b.md")
	c := DocRefID("specs", "a/c.md")
	assert.Equal(t, a, b)
	assert.NotEqual(t, a, c)
}

func TestHashContentDetectsChange(t *testing.T) {
	assert.Equal(t, HashContent([]byte("x")), HashContent([]byte("x")))
	assert.NotEqual(t, HashContent([]byte("x")), HashContent([]byte("y")))
}

func TestDocFilterMatches(t *testing.T) {
	ref := DocRef{PathOrID: "specs/a.md", Tags: []string{"spec"}}
	tests := []struct {
		name   string
		filter DocFilter
		want   bool
	}{
		{"zero matches all", DocFilter{}, true},
		{"prefix match", DocFilter{PathPrefix: "specs/"}, true},
		{"prefix miss", DocFilter{PathPrefix: "docs/"}, false},
		{"tag match", DocFilter{Tags: []string{"spec"}}, true},
		{"tag miss", DocFilter{Tags: []string{"adr"}}, false},
		{"prefix and tag", DocFilter{PathPrefix: "specs/", Tags: []string{"spec"}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.filter.matches(ref))
		})
	}
}

// Compile-time assertions that each backend satisfies the extension point.
var (
	_ Backend = (*localFSBackend)(nil)
	_ Backend = (*gitRepoBackend)(nil)
	_ Backend = (*s3Backend)(nil)
)
