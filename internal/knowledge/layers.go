package knowledge

import (
	"context"
	"fmt"
	"sort"

	"github.com/conductor-sh/conductor/internal/config"
)

// assignLayer matches relPath against knowledge.layer_definitions glob patterns
// (SPEC §8.2 Stage 6) and returns the matching layer ID, or UnknownLayer when no
// pattern matches. Layer keys are tested in deterministic (key-order) sequence;
// the first matching layer wins.
func (e *Engine) assignLayer(relPath string) string {
	for _, layer := range layerOrder(e.cfg.LayerDefinitions) {
		for _, pat := range e.cfg.LayerDefinitions[layer] {
			if MatchPath(pat, relPath) {
				return layer
			}
		}
	}
	return UnknownLayer
}

// layerOrder returns the layer keys in a deterministic order. Go maps have no
// stable iteration order, but SPEC §8.6 derives layer direction from "the order
// of keys in knowledge.layer_definitions". We sort the keys so the derived
// direction is stable across runs; operators who need a specific stack order
// should encode it via harness_rules overrides (which take precedence).
func layerOrder(defs map[string][]string) []string {
	keys := make([]string, 0, len(defs))
	for k := range defs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// layerRank maps each layer ID to its index in the stack (lower index = lower
// layer). Unknown layers are absent from the map.
func layerRank(defs map[string][]string) map[string]int {
	order := layerOrder(defs)
	rank := make(map[string]int, len(order))
	for i, k := range order {
		rank[k] = i
	}
	return rank
}

// LayerViolation is one prohibited cross-layer dependency (SPEC §8.6).
type LayerViolation struct {
	// FromID / FromPath identify the depending node.
	FromID   string `json:"from_id"`
	FromPath string `json:"from_path"`
	// FromLayer is the depending node's layer.
	FromLayer string `json:"from_layer"`
	// ToID / ToPath identify the depended-upon node.
	ToID   string `json:"to_id"`
	ToPath string `json:"to_path"`
	// ToLayer is the depended-upon node's layer.
	ToLayer string `json:"to_layer"`
	// EdgeType is the dependency edge kind.
	EdgeType EdgeType `json:"edge_type"`
}

// CheckLayerViolations returns every dependency edge that crosses layer
// boundaries in a prohibited direction (SPEC §8.6). By default, a node in a
// lower layer may not depend on a node in a higher layer (higher layers may
// import lower layers but not vice versa). Custom harness_rules with category
// "dependency" override the default for a named pair (see harnessAllows). Nodes
// in the UnknownLayer are excluded from reporting on either side.
func (e *Engine) CheckLayerViolations(ctx context.Context, rules []config.HarnessRule) ([]LayerViolation, error) {
	if e.store == nil {
		return nil, fmt.Errorf("%w: no store configured", ErrSearchFailed)
	}
	nodes, err := e.store.AllForLayerCheck(ctx, e.projectID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSearchFailed, err)
	}

	rank := layerRank(e.cfg.LayerDefinitions)
	layerByID := make(map[string]string, len(nodes))
	pathByID := make(map[string]string, len(nodes))
	for i := range nodes {
		layerByID[nodes[i].ID] = nodes[i].LayerID
		pathByID[nodes[i].ID] = nodes[i].Path
	}

	var out []LayerViolation
	for i := range nodes {
		from := &nodes[i]
		fromLayer := from.LayerID
		if fromLayer == "" || fromLayer == UnknownLayer {
			continue
		}
		fromRank, ok := rank[fromLayer]
		if !ok {
			continue
		}
		for _, edge := range from.OutgoingEdges {
			toLayer, known := layerByID[edge.ToID]
			if !known || toLayer == "" || toLayer == UnknownLayer {
				continue
			}
			toRank, ok := rank[toLayer]
			if !ok {
				continue
			}
			// Default: depending "upward" (lower layer -> higher layer) is a
			// violation. A custom rule may permit it.
			if toRank > fromRank && !harnessAllows(rules, fromLayer, toLayer) {
				out = append(out, LayerViolation{
					FromID:    from.ID,
					FromPath:  from.Path,
					FromLayer: fromLayer,
					ToID:      edge.ToID,
					ToPath:    pathByID[edge.ToID],
					ToLayer:   toLayer,
					EdgeType:  edge.EdgeType,
				})
			}
		}
	}
	return out, nil
}

// harnessAllows reports whether a harness_rules override explicitly permits a
// dependency from fromLayer to toLayer. The override convention encodes the
// allowed direction in the rule's Check string as "<from> -> <to>" for any
// dependency-category rule; a matching entry suppresses the default violation.
func harnessAllows(rules []config.HarnessRule, fromLayer, toLayer string) bool {
	want := fromLayer + " -> " + toLayer
	for _, r := range rules {
		if r.Category != "dependency" {
			continue
		}
		if r.Check == want {
			return true
		}
	}
	return false
}
