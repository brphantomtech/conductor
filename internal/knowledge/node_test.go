package knowledge

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNodeIDStability(t *testing.T) {
	a := NodeID("proj", "internal/auth/auth.go", "ValidateToken")
	b := NodeID("proj", "internal/auth/auth.go", "ValidateToken")
	require.Equal(t, a, b, "same path+symbol must yield same ID")

	file := NodeID("proj", "internal/auth/auth.go", "")
	require.NotEqual(t, a, file, "symbol and file IDs differ")

	other := NodeID("proj", "internal/auth/auth.go", "VerifyToken")
	require.NotEqual(t, a, other, "different symbols differ")

	diffProj := NodeID("other", "internal/auth/auth.go", "ValidateToken")
	require.NotEqual(t, a, diffProj, "project scopes the ID")
}

func TestChecksumStability(t *testing.T) {
	require.Equal(t, Checksum([]byte("hello")), Checksum([]byte("hello")))
	require.NotEqual(t, Checksum([]byte("hello")), Checksum([]byte("world")))
}

func TestNodeTypeEnumRoundTrip(t *testing.T) {
	for _, nt := range AllNodeTypes() {
		assert.True(t, IsKnownNodeType(nt), "type %q should be known", nt)
		assert.Equal(t, nt, NodeType(string(nt)), "round-trip")
	}
	assert.False(t, IsKnownNodeType(NodeType("bogus")))
}

func TestEdgeTypeEnumRoundTrip(t *testing.T) {
	for _, et := range AllEdgeTypes() {
		assert.True(t, IsKnownEdgeType(et))
	}
	assert.False(t, IsKnownEdgeType(EdgeType("bogus")))
}

func TestSentinelStrings(t *testing.T) {
	assert.Equal(t, "knowledge_index_failed", ErrIndexFailed.Error())
	assert.Equal(t, "knowledge_search_failed", ErrSearchFailed.Error())
	assert.Equal(t, "embedding_request_failed", ErrEmbeddingRequestFailed.Error())
}
