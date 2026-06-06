package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// NodeType is the typed enum for SPEC §4.1.6 node types. The string value is
// the canonical identifier persisted in the store and emitted over the API.
type NodeType string

// Node types defined by SPEC §4.1.6.
const (
	// NodeFile is a source file.
	NodeFile NodeType = "file"
	// NodeSymbol is an exported function, class, type, or variable.
	NodeSymbol NodeType = "symbol"
	// NodeModule is a logical package or module.
	NodeModule NodeType = "module"
	// NodeLayer is an architectural layer from knowledge.layer_definitions.
	NodeLayer NodeType = "layer"
	// NodeDoc is a document from a Doc Store (populated by Phase 11).
	NodeDoc NodeType = "doc"
	// NodeChunk is a generic text chunk for file types not parsed by the AST
	// layer.
	NodeChunk NodeType = "chunk"
)

// AllNodeTypes returns the SPEC §4.1.6 node-type registry in a deterministic
// order. Tests cross-check the enum round-trip against this list.
func AllNodeTypes() []NodeType {
	return []NodeType{NodeFile, NodeSymbol, NodeModule, NodeLayer, NodeDoc, NodeChunk}
}

// IsKnownNodeType reports whether t is in the SPEC §4.1.6 registry.
func IsKnownNodeType(t NodeType) bool {
	for _, k := range AllNodeTypes() {
		if k == t {
			return true
		}
	}
	return false
}

// EdgeType is the typed enum for SPEC §4.1.7 edge types.
type EdgeType string

// Edge types defined by SPEC §4.1.7. The model carries every direction the
// dependency graph needs; depends_on is the generic catch-all the layer-check
// uses when a more specific kind is unavailable.
const (
	// EdgeImports is a directed import dependency from one file to another.
	EdgeImports EdgeType = "imports"
	// EdgeCalls is a call-site dependency between symbols.
	EdgeCalls EdgeType = "calls"
	// EdgeExtends is a subclass/embedding relationship.
	EdgeExtends EdgeType = "extends"
	// EdgeImplements is an interface-implementation relationship.
	EdgeImplements EdgeType = "implements"
	// EdgeReferencesDoc links a code node to a Doc node (Phase 11).
	EdgeReferencesDoc EdgeType = "references_doc"
	// EdgeDependsOn is the generic dependency edge (SPEC §4.1.7).
	EdgeDependsOn EdgeType = "depends_on"
)

// AllEdgeTypes returns the SPEC §4.1.7 edge-type registry in a deterministic
// order.
func AllEdgeTypes() []EdgeType {
	return []EdgeType{
		EdgeImports, EdgeCalls, EdgeExtends, EdgeImplements, EdgeReferencesDoc, EdgeDependsOn,
	}
}

// IsKnownEdgeType reports whether t is in the SPEC §4.1.7 registry.
func IsKnownEdgeType(t EdgeType) bool {
	for _, k := range AllEdgeTypes() {
		if k == t {
			return true
		}
	}
	return false
}

// UnknownLayer is the layer assigned to nodes that match no
// knowledge.layer_definitions pattern (SPEC §8.2 Stage 6). Nodes in this layer
// are excluded from layer-violation reporting (SPEC §8.6).
const UnknownLayer = "unknown"

// Edge is a directed edge in the codebase knowledge graph (SPEC §4.1.7).
type Edge struct {
	// FromID is the source node ID.
	FromID string `json:"from_id"`
	// ToID is the target node ID.
	ToID string `json:"to_id"`
	// EdgeType classifies the dependency.
	EdgeType EdgeType `json:"edge_type"`
	// Weight ranks edges by importance (optional; defaults to 0).
	Weight float64 `json:"weight,omitempty"`
}

// Node is a node in the codebase knowledge graph (SPEC §4.1.6). The zero value
// is not meaningful; nodes are produced by the indexing pipeline.
type Node struct {
	// ID is a stable hash of <project_id>/<relative_path>[#<symbol_name>].
	ID string `json:"id"`
	// Type is the node-type enum.
	Type NodeType `json:"type"`
	// ProjectID scopes the node to a project.
	ProjectID string `json:"project_id"`
	// Path is the relative path in the workspace or doc store.
	Path string `json:"path"`
	// Name is the symbol or file name.
	Name string `json:"name"`
	// Summary is the LLM-generated or statically extracted summary.
	Summary string `json:"summary"`
	// Content is the raw content snippet for retrieval injection.
	Content string `json:"content"`
	// Embedding is the dense vector for semantic search.
	Embedding []float32 `json:"embedding,omitempty"`
	// OutgoingEdges are the directed edges leaving this node.
	OutgoingEdges []Edge `json:"outgoing_edges,omitempty"`
	// LayerID is the architectural layer (UnknownLayer when unmatched).
	LayerID string `json:"layer_id,omitempty"`
	// Language is the source language (optional).
	Language string `json:"language,omitempty"`
	// LineStart is the first line of a symbol node (1-based; 0 when unset).
	LineStart int `json:"line_start,omitempty"`
	// LineEnd is the last line of a symbol node (0 when unset).
	LineEnd int `json:"line_end,omitempty"`
	// LastIndexedAt is when the node was last (re-)indexed.
	LastIndexedAt time.Time `json:"last_indexed_at"`
	// Checksum is the content hash used for incremental indexing.
	Checksum string `json:"checksum"`
}

// NodeID computes the stable SPEC §4.1.6 node identifier:
// hash(<project_id>/<relative_path>[#<symbol_name>]). symbolName is empty for
// file/chunk nodes. The hash is deterministic so re-indexing an unchanged
// file/symbol yields the same ID.
func NodeID(projectID, relPath, symbolName string) string {
	key := projectID + "/" + relPath
	if symbolName != "" {
		key += "#" + symbolName
	}
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// Checksum computes the content checksum used for incremental indexing
// (SPEC §4.1.6 checksum, §8.2 Stage 1).
func Checksum(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
