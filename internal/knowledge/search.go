package knowledge

import (
	"context"
	"fmt"
	"sort"
)

// SearchParams is the conductor_knowledge_search parameter set (SPEC §8.4). The
// zero value (empty Query) is valid for a pure structural query.
type SearchParams struct {
	// Query is the natural-language seed for semantic/hybrid search.
	Query string
	// Types restricts results to the given node types.
	Types []NodeType
	// Layers restricts results to the given layer IDs.
	Layers []string
	// PathPattern restricts results to nodes whose path matches the glob.
	PathPattern string
	// IncludeDependencies expands results with one dependency hop.
	IncludeDependencies bool
	// TopK caps the result count (defaults to the engine's configured top_k,
	// then to the SPEC default of 10).
	TopK int
}

// filter projects the params onto a structural Filter.
func (p SearchParams) filter() Filter {
	return Filter{
		Types:       p.Types,
		Layers:      p.Layers,
		PathPattern: p.PathPattern,
	}
}

// Search runs hybrid search (SPEC §8.4): when Query is set it retrieves semantic
// candidates and re-ranks them by structural relevance; when Query is empty it
// degrades to a structural query. IncludeDependencies expands the result set by
// one dependency hop. Failures are classified as ErrSearchFailed.
func (e *Engine) Search(ctx context.Context, p SearchParams) ([]ScoredNode, error) {
	if e.store == nil {
		return nil, fmt.Errorf("%w: no store configured", ErrSearchFailed)
	}
	topK := p.TopK
	if topK <= 0 {
		topK = e.topK()
	}

	results, err := e.rankedResults(ctx, p, topK)
	if err != nil {
		return nil, err
	}

	if p.IncludeDependencies {
		results, err = e.expandDependencies(ctx, results, topK)
		if err != nil {
			return nil, err
		}
	}
	if len(results) > topK {
		results = results[:topK]
	}
	return results, nil
}

// rankedResults produces the primary ranked candidate set (semantic+structural
// hybrid, or structural-only when Query is empty).
func (e *Engine) rankedResults(ctx context.Context, p SearchParams, topK int) ([]ScoredNode, error) {
	if p.Query == "" {
		nodes, err := e.store.QueryStructural(ctx, p.filter(), topK)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrSearchFailed, err)
		}
		out := make([]ScoredNode, len(nodes))
		for i, n := range nodes {
			out[i] = ScoredNode{Node: n}
		}
		return out, nil
	}

	emb, err := e.embedder.Embed(ctx, p.Query)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSearchFailed, err)
	}
	// Over-fetch candidates so structural re-ranking has room to reorder.
	candidates, err := e.store.SearchSemantic(ctx, emb, p.filter(), topK*3)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSearchFailed, err)
	}
	e.rerank(candidates, p)
	if len(candidates) > topK {
		candidates = candidates[:topK]
	}
	return candidates, nil
}

// rerank adjusts semantic scores by a structural-relevance signal (SPEC §8.4
// "semantic results re-ranked by structural relevance"): node-type weight, an
// explicit-layer-match bonus, and a path-prefix-match bonus.
func (e *Engine) rerank(cands []ScoredNode, p SearchParams) {
	for i := range cands {
		cands[i].Score += structuralBoost(cands[i].Node, p)
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].Score > cands[j].Score })
}

// structuralBoost is the additive structural-relevance term.
func structuralBoost(n Node, p SearchParams) float64 {
	var boost float64
	switch n.Type {
	case NodeSymbol:
		boost += 0.10 // symbols are the most actionable retrieval target
	case NodeFile:
		boost += 0.05
	}
	if len(p.Layers) > 0 && containsString(p.Layers, n.LayerID) {
		boost += 0.05
	}
	if p.PathPattern != "" && MatchPath(p.PathPattern, n.Path) {
		boost += 0.05
	}
	return boost
}

// expandDependencies appends the one-hop dependency neighbors of the current
// results that are not already present (SPEC §8.4 dependency expansion).
func (e *Engine) expandDependencies(ctx context.Context, results []ScoredNode, topK int) ([]ScoredNode, error) {
	seen := make(map[string]struct{}, len(results))
	for _, r := range results {
		seen[r.Node.ID] = struct{}{}
	}
	out := append([]ScoredNode(nil), results...)
	for _, r := range results {
		for _, dir := range []Direction{Outgoing, Incoming} {
			neighbors, err := e.store.Neighbors(ctx, r.Node.ID, dir)
			if err != nil {
				return nil, fmt.Errorf("%w: %v", ErrSearchFailed, err)
			}
			for _, n := range neighbors {
				if _, dup := seen[n.ID]; dup {
					continue
				}
				seen[n.ID] = struct{}{}
				out = append(out, ScoredNode{Node: n, Score: r.Score - 0.01})
			}
		}
	}
	return out, nil
}

// QueryStructural is a convenience wrapper for a pure structural query
// (SPEC §8.4 structural). Failures are classified as ErrSearchFailed.
func (e *Engine) QueryStructural(ctx context.Context, f Filter, topK int) ([]Node, error) {
	if e.store == nil {
		return nil, fmt.Errorf("%w: no store configured", ErrSearchFailed)
	}
	nodes, err := e.store.QueryStructural(ctx, f, topK)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSearchFailed, err)
	}
	return nodes, nil
}

// Dependencies returns the one-hop dependency neighbors of id in the given
// direction (SPEC §8.4 dependency query). Failures are classified as
// ErrSearchFailed.
func (e *Engine) Dependencies(ctx context.Context, id string, dir Direction) ([]Node, error) {
	if e.store == nil {
		return nil, fmt.Errorf("%w: no store configured", ErrSearchFailed)
	}
	nodes, err := e.store.Neighbors(ctx, id, dir)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSearchFailed, err)
	}
	return nodes, nil
}
