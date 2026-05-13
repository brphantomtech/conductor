package tracker

// BlockerRef is one entry in Issue.BlockedBy per SPEC §4.1.1. All three
// fields are nullable because different trackers expose different
// subsets:
//   - Linear's `blockedBy` GraphQL edge populates all three.
//   - GitHub's `tracked_in_issues` REST projection populates Identifier
//     only; consumers that need State call FetchIssueStatesByIDs.
type BlockerRef struct {
	ID         *string
	Identifier *string
	State      *string
}
