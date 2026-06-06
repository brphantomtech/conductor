package tools

import "errors"

// Sentinel errors for the tool-injection layer. These classify dispatcher and
// registry faults distinct from the per-tool error ToolResults returned to the
// model. Higher layers detect them with errors.Is.
var (
	// ErrUnknownTool signals a tool call naming a tool not in the registry.
	// The dispatcher converts it into an error ToolResult rather than aborting.
	ErrUnknownTool = errors.New("tool_unknown")

	// ErrDuplicateTool signals an attempt to register two tools with the same
	// name. Registration is a wiring-time operation, so this is a programmer
	// error surfaced at construction.
	ErrDuplicateTool = errors.New("tool_duplicate_registration")

	// ErrInvalidParams signals that a tool call's arguments could not be
	// decoded or failed the tool's required-parameter validation.
	ErrInvalidParams = errors.New("tool_invalid_params")

	// ErrApprovalRequired signals that a tool call was gated by the role's
	// approval_policy and denied because no operator approval channel exists
	// yet (Phases 14/15). The dispatcher returns it as a structured
	// approval-required ToolResult.
	ErrApprovalRequired = errors.New("tool_approval_required")

	// ErrEngineUnavailable signals that the engine a tool depends on was not
	// wired (a nil consumer-side handle). Returned as an error ToolResult.
	ErrEngineUnavailable = errors.New("tool_engine_unavailable")
)
