// Package workspace owns the lifecycle of per-issue workspaces (SPEC §14):
// sanitized key derivation, layout creation with the .conductor/ skeleton,
// multi-repo checkout, lifecycle hook execution with timeouts, removal, and
// the subprocess agent-isolation seam. It enforces the four SPEC §14.2 safety
// invariants — the agent process runs within workspace_path; workspace_path
// stays under workspace_root; keys are sanitized to [A-Za-z0-9._-]; and
// credentials are never written to the workspace.
//
// Files:
//   - manager.go    — Manager, functional options, Create / Remove, repo
//     materialization, the AgentCommand isolation seam, and audit emission.
//   - hooks.go      — RunHook plus the OS-aware script runner (bash -lc /
//     cmd /C) with per-hook timeout and output capture (SPEC §5.3.5).
//   - sanitize.go   — SanitizeKey (SPEC §4.2 / §14.2 Invariant 3) and the
//     withinRoot confinement check (Invariant 2).
//   - errors.go     — SPEC §23.4 sentinels (workspace_creation_failed,
//     hook_failed) and the internal §14.2 unsafe-path sentinel.
//   - proc_unix.go / proc_windows.go — OS-specific process-group setup and
//     teardown so a timed-out hook is fully reaped.
//
// Tier 1 (external adapters / infra). Imports config and audit. Does not
// import any other Tier 1 sibling.
package workspace
