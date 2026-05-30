package workspace

import "time"

// Hook identifies one of the SPEC §5.3.5 lifecycle hooks.
type Hook string

// The six lifecycle hooks defined by SPEC §5.3.5. The string values are the
// HARNESS.md field names and double as the audit-event payload label.
const (
	HookAfterCreate        Hook = "after_create"
	HookBeforeRun          Hook = "before_run"
	HookAfterRun           Hook = "after_run"
	HookAfterTurn          Hook = "after_turn"
	HookBeforeRemove       Hook = "before_remove"
	HookOnHarnessViolation Hook = "on_harness_violation"
)

// Workspace is the materialized layout for one issue (SPEC §14.1). Callers
// treat it as an opaque handle returned by Manager.Create and passed back to
// RunHook / Remove / AgentCommand.
type Workspace struct {
	// Key is the sanitized identifier used as the directory name.
	Key string
	// Path is the absolute workspace directory (<root>/<key>).
	Path string
	// Root is the absolute, configured workspace root.
	Root string
	// IssueID is the tracker's stable internal id (audit correlation).
	IssueID string
	// IssueIdentifier is the human-readable key the Key was derived from.
	IssueIdentifier string
	// ConductorDir is <Path>/.conductor.
	ConductorDir string
	// AuditLogPath is <ConductorDir>/audit.jsonl (wired to a sink in Phase 6).
	AuditLogPath string
	// ValidationDir is <ConductorDir>/validation (populated in Phase 8).
	ValidationDir string
	// MetaPath is <ConductorDir>/meta.json.
	MetaPath string
	// Repos lists the materialized repositories (empty for single-repo).
	Repos []RepoLayout
	// CreatedAt is the workspace creation timestamp (UTC).
	CreatedAt time.Time
}

// RepoLayout records one materialized repository inside a multi-repo
// workspace (SPEC §5.3.4).
type RepoLayout struct {
	// Name is the sanitized subdirectory name within the workspace.
	Name string
	// Path is the absolute path to the repository checkout.
	Path string
	// URL is the configured clone URL (empty when none was configured).
	URL string
}
