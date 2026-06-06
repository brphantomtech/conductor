package memory

import "context"

// LayerCounts is the per-layer entry tally surfaced by `conductor memory list`.
type LayerCounts struct {
	Episodic   int
	Semantic   int
	Procedural int
}

// Total returns the sum across all layers.
func (c LayerCounts) Total() int { return c.Episodic + c.Semantic + c.Procedural }

// Counts returns the number of entries per layer for a project. It is the
// data behind `conductor memory list`. A disabled manager returns zeros.
func (m *Manager) Counts(ctx context.Context, projectID string) (LayerCounts, error) {
	if !m.Enabled() {
		return LayerCounts{}, nil
	}
	projectID = m.resolveProject(projectID)
	var out LayerCounts
	for _, l := range []Layer{LayerEpisodic, LayerSemantic, LayerProcedural} {
		entries, err := m.store.Query(ctx, QueryFilter{ProjectID: projectID, Layer: l})
		if err != nil {
			return LayerCounts{}, wrapRead(err)
		}
		switch l {
		case LayerEpisodic:
			out.Episodic = len(entries)
		case LayerSemantic:
			out.Semantic = len(entries)
		case LayerProcedural:
			out.Procedural = len(entries)
		}
	}
	return out, nil
}

// List returns all entries for a project, most-recent first. It backs the
// detailed view of `conductor memory list`. A disabled manager returns nil.
func (m *Manager) List(ctx context.Context, projectID string) ([]MemoryEntry, error) {
	if !m.Enabled() {
		return nil, nil
	}
	projectID = m.resolveProject(projectID)
	entries, err := m.store.Query(ctx, QueryFilter{ProjectID: projectID})
	if err != nil {
		return nil, wrapRead(err)
	}
	return entries, nil
}
