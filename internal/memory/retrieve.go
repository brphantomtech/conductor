package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/conductor-sh/conductor/internal/audit"
)

// Per-layer retrieval quotas from SPEC §9.3.
const (
	episodicQuota   = 3
	semanticQuota   = 3
	proceduralQuota = 1
)

// RetrieveRequest selects the context for one turn's retrieval (SPEC §9.3).
type RetrieveRequest struct {
	// ProjectID scopes every layer; required.
	ProjectID string
	// IssueID scopes the episodic query to one issue.
	IssueID string
	// TaskType scopes the procedural query to one task type.
	TaskType string
	// Intent is the turn's seed text, embedded for semantic similarity.
	Intent string
}

// Retrieve returns the SPEC §9.3 memory mix for a turn: the 3 most recent
// episodic entries for the issue, the top 3 semantic entries by similarity to
// the intent, and 1 procedural entry for the task type — merged, deduped by
// content, ranked by relevance, and capped by max_context_memories. It emits
// a MemoryRead event. A disabled manager returns no entries.
func (m *Manager) Retrieve(ctx context.Context, req RetrieveRequest) ([]MemoryEntry, error) {
	if !m.Enabled() {
		return nil, nil
	}
	projectID := m.resolveProject(req.ProjectID)

	episodic, err := m.store.Query(ctx, QueryFilter{
		ProjectID: projectID, Layer: LayerEpisodic, IssueID: req.IssueID, Limit: episodicQuota,
	})
	if err != nil {
		return nil, wrapRead(err)
	}
	// Query orders by recency; episodic relevance is recency-based.
	for i := range episodic {
		episodic[i].RelevanceScore = recencyScore(i, len(episodic))
	}

	semantic, err := m.store.Query(ctx, QueryFilter{ProjectID: projectID, Layer: LayerSemantic})
	if err != nil {
		return nil, wrapRead(err)
	}
	semantic, err = m.rankBySimilarity(ctx, req.Intent, semantic, semanticQuota)
	if err != nil {
		return nil, wrapRead(err)
	}

	procedural, err := m.store.Query(ctx, QueryFilter{
		ProjectID: projectID, Layer: LayerProcedural, TaskType: req.TaskType, Limit: proceduralQuota,
	})
	if err != nil {
		return nil, wrapRead(err)
	}
	for i := range procedural {
		procedural[i].RelevanceScore = 0.5
	}

	merged := dedupe(episodic, semantic, procedural)
	sort.SliceStable(merged, func(i, j int) bool {
		return merged[i].RelevanceScore > merged[j].RelevanceScore
	})
	if m.cfg.MaxContextMemories > 0 && len(merged) > m.cfg.MaxContextMemories {
		merged = merged[:m.cfg.MaxContextMemories]
	}

	m.emit(ctx, audit.AuditEvent{
		ProjectID: projectID,
		IssueID:   req.IssueID,
		EventType: audit.EventMemoryRead,
		Payload: map[string]any{
			"episodic":   len(episodic),
			"semantic":   len(semantic),
			"procedural": len(procedural),
			"returned":   len(merged),
		},
	})
	return merged, nil
}

// rankBySimilarity scores candidates by cosine similarity to the intent and
// returns the topK. When no Embedder is wired (or the intent is empty), it
// falls back to recency order with a flat score.
func (m *Manager) rankBySimilarity(
	ctx context.Context, intent string, candidates []MemoryEntry, topK int,
) ([]MemoryEntry, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	if m.embed == nil || strings.TrimSpace(intent) == "" {
		out := candidates
		if len(out) > topK {
			out = out[:topK]
		}
		for i := range out {
			out[i].RelevanceScore = recencyScore(i, len(out))
		}
		return out, nil
	}
	seed, err := m.embed.Embed(ctx, intent)
	if err != nil {
		return nil, fmt.Errorf("memory: embed intent: %w", err)
	}
	scored := make([]MemoryEntry, len(candidates))
	copy(scored, candidates)
	for i := range scored {
		sim := cosine(seed, scored[i].Embedding)
		// Map [-1,1] cosine into [0,1] relevance.
		scored[i].RelevanceScore = (sim + 1) / 2
	}
	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].RelevanceScore > scored[j].RelevanceScore
	})
	if len(scored) > topK {
		scored = scored[:topK]
	}
	return scored, nil
}

// dedupe merges layer slices and removes duplicate content (SPEC §9.3 step
// 4). The first occurrence wins; later duplicates are dropped.
func dedupe(groups ...[]MemoryEntry) []MemoryEntry {
	seen := map[string]struct{}{}
	var out []MemoryEntry
	for _, g := range groups {
		for _, e := range g {
			h := contentHash(e.Content)
			if _, dup := seen[h]; dup {
				continue
			}
			seen[h] = struct{}{}
			out = append(out, e)
		}
	}
	return out
}

// recencyScore maps a position in a recency-ordered slice to a [0,1]
// relevance, with the most recent entry scoring highest.
func recencyScore(index, total int) float64 {
	if total <= 1 {
		return 1.0
	}
	return 1.0 - float64(index)/float64(total)
}

func contentHash(s string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(s)))
	return hex.EncodeToString(sum[:])
}
