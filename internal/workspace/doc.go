// Package workspace owns the lifecycle of per-issue workspaces (SPEC §14):
// creation, sanitized key derivation, multi-repo cloning, hook execution,
// and removal. It enforces the four safety invariants — agent process must
// run within workspace_path; workspace_path must be under workspace_root;
// keys are sanitized; credentials never reach the workspace.
//
// Tier 1 (external adapters / infra). Imports config, db, audit. Does not
// import any other Tier 1 sibling.
package workspace
