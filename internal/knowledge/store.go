package knowledge

import "context"

// Filter is the structural-query predicate set (SPEC §8.4 structural query and
// the conductor_knowledge_search parameter set). All fields are optional; a
// zero-value Filter matches every node. Slice fields are OR-within / AND-across:
// a node matches when its type is in Types AND its layer is in Layers AND its
// language is in Languages AND its path matches PathPattern.
type Filter struct {
	// Types restricts to the given node types (empty means any).
	Types []NodeType
	// Layers restricts to the given layer IDs (empty means any).
	Layers []string
	// Languages restricts to the given source languages (empty means any).
	Languages []string
	// PathPattern is a glob matched against the node path (empty means any).
	PathPattern string
}

// matches reports whether n satisfies the filter. PathPattern matching is the
// caller's responsibility via MatchPath; the store applies the slice filters.
func (f Filter) matches(n *Node) bool {
	if len(f.Types) > 0 && !containsNodeType(f.Types, n.Type) {
		return false
	}
	if len(f.Layers) > 0 && !containsString(f.Layers, n.LayerID) {
		return false
	}
	if len(f.Languages) > 0 && !containsString(f.Languages, n.Language) {
		return false
	}
	if f.PathPattern != "" && !MatchPath(f.PathPattern, n.Path) {
		return false
	}
	return true
}

// Direction selects which side of an edge a dependency traversal walks.
type Direction string

const (
	// Outgoing follows edges from the node ("what does X import / call").
	Outgoing Direction = "outgoing"
	// Incoming follows edges into the node ("what imports / calls X").
	Incoming Direction = "incoming"
)

// Store is the persistence abstraction for the knowledge graph (SPEC §8.2
// Stage 7). The sqlite_vec backend is the default; qdrant sits behind a build
// tag. It is a cross-cutting extension point so it lives in the implementer's
// home package per docs/conventions.md §3.
type Store interface {
	// Upsert idempotently writes nodes (and their outgoing edges) keyed by
	// node ID; a second upsert of the same ID replaces the prior record.
	Upsert(ctx context.Context, nodes []Node) error

	// DeleteByPath removes every node at the given relative path plus all
	// edges incident to those nodes.
	DeleteByPath(ctx context.Context, projectID, path string) error

	// SearchSemantic returns up to topK nodes ranked by embedding similarity
	// to query, restricted to the structural filter.
	SearchSemantic(ctx context.Context, query []float32, filter Filter, topK int) ([]ScoredNode, error)

	// QueryStructural returns every node matching the structural filter, up to
	// topK (topK <= 0 means unbounded).
	QueryStructural(ctx context.Context, filter Filter, topK int) ([]Node, error)

	// Neighbors returns the nodes reachable from id by one edge hop in the
	// given direction.
	Neighbors(ctx context.Context, id string, dir Direction) ([]Node, error)

	// AllForLayerCheck returns every node (with edges) for the project so the
	// layer-violation pass can examine the full graph.
	AllForLayerCheck(ctx context.Context, projectID string) ([]Node, error)

	// Count returns the number of persisted nodes for the project.
	Count(ctx context.Context, projectID string) (int, error)

	// Close releases backend resources.
	Close() error
}

// ScoredNode pairs a node with its semantic-similarity score (cosine, in
// [-1, 1]; higher is more relevant).
type ScoredNode struct {
	Node  Node
	Score float64
}

func containsNodeType(s []NodeType, v NodeType) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func containsString(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
