package tracker

import "time"

// Issue is the normalized SPEC §4.1.1 issue record returned by every
// TrackerAdapter. Adapters apply the SPEC §4.2 normalization rules at
// the boundary so downstream consumers (the orchestrator in Phase 6,
// the router in Phase 7, the tool dispatcher in Phase 13) see a single
// shape regardless of source tracker.
//
// Pointer fields are nullable per the SPEC. KnowledgeContextIDs,
// MemorySessionIDs, TaskType, and EstimatedComplexity are zero-valued
// here and populated later by the router on first dispatch.
type Issue struct {
	// ID is the tracker-internal stable identifier (Linear node id,
	// GitHub issue numeric id stringified). Used as the map key in
	// FetchIssueStatesByIDs.
	ID string

	// Identifier is the human-readable ticket key (Linear "ABC-123",
	// GitHub "gh-42"). Used in logs and workspace naming.
	Identifier string

	Title       string
	Description *string

	// Priority is tracker-defined; lower values are higher priority by
	// SPEC §4.1.1 convention.
	Priority *int

	// State is preserved verbatim from the tracker. Consumers compare
	// after strings.ToLower per SPEC §4.2.
	State string

	// BranchName carries the tracker's suggested branch name when
	// available (Linear `branchName` field). GitHub leaves this nil
	// and the workspace layer falls back to its branch_template.
	BranchName *string

	URL *string

	// Labels are lowercased, deduplicated, and sorted by the adapter.
	Labels []string

	// BlockedBy is always a non-nil slice; empty when no blockers.
	BlockedBy []BlockerRef

	CreatedAt *time.Time
	UpdatedAt *time.Time

	// TaskType is classified by the router on first dispatch — Phase 7
	// populates this; Phase 4 always leaves it nil.
	TaskType *string

	// EstimatedComplexity is estimated by the planner agent — Phase 7
	// populates this; Phase 4 always leaves it nil.
	EstimatedComplexity *string

	// KnowledgeContextIDs is populated by Phase 10 on dispatch.
	KnowledgeContextIDs []string

	// MemorySessionIDs is populated by Phase 9 on dispatch.
	MemorySessionIDs []string
}
