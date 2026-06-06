package memory

import "time"

// Layer is the three-layer classification of a MemoryEntry (SPEC §9.2).
type Layer string

// The three memory layers. Episodic is session-scoped and TTL-bounded;
// semantic is project-wide; procedural is task-type-scoped. The string
// values are persisted verbatim in the store.
const (
	LayerEpisodic   Layer = "episodic"
	LayerSemantic   Layer = "semantic"
	LayerProcedural Layer = "procedural"
)

// Valid reports whether l is one of the three known layers.
func (l Layer) Valid() bool {
	switch l {
	case LayerEpisodic, LayerSemantic, LayerProcedural:
		return true
	default:
		return false
	}
}

// Source is the provenance enum for a MemoryEntry (SPEC §4.1.5). It records
// which write path created the entry so consolidated and auto-extracted
// memories stay distinguishable and retireable.
type Source string

// Memory entry sources. The string values are persisted verbatim.
const (
	SourceAgentWritten     Source = "agent_written"
	SourceAutoExtracted    Source = "auto_extracted"
	SourceConsolidated     Source = "consolidated"
	SourceValidationResult Source = "validation_result"
)

// Valid reports whether s is one of the four known sources.
func (s Source) Valid() bool {
	switch s {
	case SourceAgentWritten, SourceAutoExtracted, SourceConsolidated, SourceValidationResult:
		return true
	default:
		return false
	}
}

// MemoryEntry is one entry in the three-layer memory system (SPEC §4.1.5).
// RelevanceScore is computed at retrieval time and is never persisted.
//
//revive:disable-next-line:exported // SPEC §4.1.5 names this entity MemoryEntry — keep the canonical name.
type MemoryEntry struct {
	// ID is a UUID assigned at write time.
	ID string
	// Layer classifies the entry (episodic/semantic/procedural).
	Layer Layer
	// ProjectID scopes every entry to a project.
	ProjectID string
	// IssueID scopes episodic entries to a specific issue. Empty otherwise.
	IssueID string
	// TaskType scopes procedural entries to a task type. Empty otherwise.
	TaskType string
	// Content is free-text or structured JSON.
	Content string
	// Embedding is the optional similarity vector for semantic search.
	Embedding []float32
	// Tags is a free-form label list.
	Tags []string
	// Source records the write path that created the entry.
	Source Source
	// CreatedAt is the write timestamp (from the injected clock).
	CreatedAt time.Time
	// ExpiresAt is the TTL boundary for episodic entries; nil for
	// semantic and procedural entries (no TTL until retired/superseded).
	ExpiresAt *time.Time
	// RelevanceScore is computed at retrieval time and never persisted
	// (SPEC §4.1.5).
	RelevanceScore float64
}
