package memory

import (
	"context"
	"fmt"
	"time"

	"github.com/conductor-sh/conductor/internal/audit"
)

// minClusterSize is the SPEC §9.5 threshold: only clusters of 3+ episodic
// members are synthesized into a semantic memory.
const minClusterSize = 3

// similarityThreshold is the cosine cutoff for grouping episodic memories
// into a cluster. Tunable; kept conservative so unrelated memories do not
// merge.
const similarityThreshold = 0.75

// Start runs the consolidation worker until ctx is cancelled (SPEC §9.5). It
// ticks every consolidation_interval_hours, re-reading the interval each
// cycle, and runs one Consolidate pass per tick. It is a no-op when memory or
// consolidation is disabled. Start owns its goroutine lifecycle; callers run
// it in a goroutine.
func (m *Manager) Start(ctx context.Context) error {
	if !m.Enabled() || !m.cfg.ConsolidationEnabled {
		<-ctx.Done()
		return fmt.Errorf("memory: consolidation worker stopped: %w", ctx.Err())
	}
	interval := time.Duration(m.cfg.ConsolidationIntervalHours) * time.Hour
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run one pass immediately at startup so a freshly-booted service
	// consolidates without waiting a full interval; this also makes the
	// worker manually drivable in tests (start, observe one pass, cancel).
	if err := m.Consolidate(ctx, m.projID); err != nil {
		m.log.Error().Err(err).Msg("consolidation pass failed")
	}

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("memory: consolidation worker stopped: %w", ctx.Err())
		case <-ticker.C:
			if err := m.Consolidate(ctx, m.projID); err != nil {
				m.log.Error().Err(err).Msg("consolidation pass failed")
			}
		}
	}
}

// ConsolidateResult reports what one Consolidate pass produced.
type ConsolidateResult struct {
	// SemanticWritten is the number of semantic entries synthesized from
	// episodic clusters.
	SemanticWritten int
	// ProceduralWritten is the number of procedural entries synthesized.
	ProceduralWritten int
	// ExpiredDeleted is the number of episodic entries past their TTL that
	// were deleted.
	ExpiredDeleted int
}

// Consolidate runs one consolidation pass for a project (SPEC §9.5):
//
//  1. cluster episodic memories by embedding similarity,
//  2. synthesize each cluster of 3+ into a semantic entry,
//  4. synthesize procedural memories from procedural episodic clusters by
//     task type,
//  5. delete expired episodic entries,
//
// then emit one MemoryConsolidated event. It is manually drivable so tests
// can step the clock and assert the outcome. A disabled manager is a no-op.
func (m *Manager) Consolidate(ctx context.Context, projectID string) error {
	if !m.Enabled() {
		return nil
	}
	projectID = m.resolveProject(projectID)
	res := ConsolidateResult{}

	episodic, err := m.store.Query(ctx, QueryFilter{ProjectID: projectID, Layer: LayerEpisodic})
	if err != nil {
		return wrapRead(err)
	}

	if m.synth != nil && m.embed != nil {
		clusters := clusterBySimilarity(episodic)
		for _, cluster := range clusters {
			if len(cluster) < minClusterSize {
				continue
			}
			contents := make([]string, len(cluster))
			for i, e := range cluster {
				contents[i] = e.Content
			}
			synth, serr := m.synth.Synthesize(ctx, contents)
			if serr != nil {
				return wrapWrite(serr)
			}
			if _, werr := m.Write(ctx, WriteInput{
				Layer:     LayerSemantic,
				ProjectID: projectID,
				Content:   synth,
				Tags:      []string{"consolidated"},
				Source:    SourceConsolidated,
			}); werr != nil {
				return werr
			}
			res.SemanticWritten++
		}

		// Procedural synthesis: group successful turn sequences by task type.
		byTask := groupByTaskType(episodic)
		for taskType, group := range byTask {
			if taskType == "" || len(group) < minClusterSize {
				continue
			}
			contents := make([]string, len(group))
			for i, e := range group {
				contents[i] = e.Content
			}
			synth, serr := m.synth.Synthesize(ctx, contents)
			if serr != nil {
				return wrapWrite(serr)
			}
			if _, werr := m.Write(ctx, WriteInput{
				Layer:     LayerProcedural,
				ProjectID: projectID,
				TaskType:  taskType,
				Content:   synth,
				Tags:      []string{"consolidated"},
				Source:    SourceConsolidated,
			}); werr != nil {
				return werr
			}
			res.ProceduralWritten++
		}
	}

	deleted, err := m.store.DeleteExpired(ctx, projectID, m.clock().UTC())
	if err != nil {
		return wrapWrite(err)
	}
	res.ExpiredDeleted = deleted

	m.emit(ctx, audit.AuditEvent{
		ProjectID: projectID,
		EventType: audit.EventMemoryConsolidated,
		Payload: map[string]any{
			"semantic_written":   res.SemanticWritten,
			"procedural_written": res.ProceduralWritten,
			"expired_deleted":    res.ExpiredDeleted,
		},
	})
	return nil
}

// clusterBySimilarity greedily groups entries whose embeddings are within
// similarityThreshold cosine of a cluster seed (SPEC §9.5 step 1). Entries
// without an embedding are skipped. The algorithm is O(n²) which is fine for
// the small per-project memory volume (see design.md).
func clusterBySimilarity(entries []MemoryEntry) [][]MemoryEntry {
	var clusters [][]MemoryEntry
	assigned := make([]bool, len(entries))
	for i := range entries {
		if assigned[i] || len(entries[i].Embedding) == 0 {
			continue
		}
		cluster := []MemoryEntry{entries[i]}
		assigned[i] = true
		for j := i + 1; j < len(entries); j++ {
			if assigned[j] || len(entries[j].Embedding) == 0 {
				continue
			}
			if cosine(entries[i].Embedding, entries[j].Embedding) >= similarityThreshold {
				cluster = append(cluster, entries[j])
				assigned[j] = true
			}
		}
		clusters = append(clusters, cluster)
	}
	return clusters
}

// groupByTaskType buckets entries by their TaskType tag. Episodic entries do
// not carry a task type natively, so callers that want procedural synthesis
// tag episodic memories accordingly; entries with an empty task type land in
// the "" bucket and are ignored by Consolidate.
func groupByTaskType(entries []MemoryEntry) map[string][]MemoryEntry {
	out := map[string][]MemoryEntry{}
	for _, e := range entries {
		out[e.TaskType] = append(out[e.TaskType], e)
	}
	return out
}
