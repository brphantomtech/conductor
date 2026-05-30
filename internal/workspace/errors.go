package workspace

import (
	"errors"
	"fmt"
)

// SPEC §23.4 — Run Attempt errors owned by the workspace layer. The sentinel
// string values match the SPEC identifiers verbatim so they appear unchanged
// in audit logs and API responses (SPEC §23 registry convention).
var (
	// ErrWorkspaceCreationFailed classifies any failure while creating a
	// workspace directory, writing its skeleton, materializing its repos, or
	// running its after_create hook.
	ErrWorkspaceCreationFailed = errors.New("workspace_creation_failed")

	// ErrHookFailed classifies a lifecycle hook that exits non-zero, times
	// out, or cannot be started.
	ErrHookFailed = errors.New("hook_failed")

	// ErrUnsafePath classifies a SPEC §14.2 safety-invariant violation: a
	// resolved path that escapes the workspace root, an empty/again-traversal
	// key, or a removal target outside the root. It is an internal safety
	// classification, distinct from the §23.4 run-attempt taxonomy.
	ErrUnsafePath = errors.New("workspace_unsafe_path")
)

// joinErr attaches a classification sentinel to an underlying cause so callers
// can both read the message and match with errors.Is. A nil cause yields the
// bare sentinel.
func joinErr(cause, sentinel error) error {
	if cause == nil {
		return sentinel
	}
	return errors.Join(cause, sentinel)
}

// wrapCreate classifies a workspace-creation failure as
// ErrWorkspaceCreationFailed while keeping the operation readable in logs.
func wrapCreate(op string, cause error) error {
	return fmt.Errorf("workspace: %s: %w", op, joinErr(cause, ErrWorkspaceCreationFailed))
}
