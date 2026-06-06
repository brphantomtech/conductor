package tools

import (
	"context"
	"encoding/json"
)

// Tool is the contract every built-in tool and registered plugin satisfies
// (SPEC §20.5). It mirrors the ToolPlugin shape exactly so built-ins and
// plugins are the same type to the registry and dispatcher.
type Tool interface {
	// Name is the tool name advertised to the model and matched on dispatch.
	// It must be unique within a Registry.
	Name() string
	// Description is the human-readable description advertised to the model.
	Description() string
	// ParametersSchema is the JSON Schema fragment describing the tool's
	// parameters, forwarded verbatim into the provider's native tool payload.
	ParametersSchema() json.RawMessage
	// Execute runs the tool against agent-supplied params, resolving any
	// credentials/handles from the ExecutionContext (never from params). A
	// returned error is surfaced to the model as an error ToolResult by the
	// dispatcher rather than aborting the turn; tools return errors only for
	// genuine execution faults.
	Execute(ctx context.Context, params map[string]any, ec ExecutionContext) (ToolResult, error)
}

// ToolPlugin is the SPEC §20.5 third-party extension point. It is identical to
// Tool; the distinct name preserves the SPEC vocabulary and documents intent at
// registration sites. Plugins are registered at startup and advertised to all
// agent sessions alongside the built-ins (dynamic loading is deferred to
// Phase 18).
type ToolPlugin = Tool

// ToolResult is the structured outcome of a tool execution returned to the
// model. Content carries the tool's payload (engine results, confirmation, or
// an error/approval-required message); IsError marks failures and denials so
// the model can react without the turn being aborted.
//
//revive:disable-next-line:exported // the ToolResult name is canonical (SPEC §20.5).
type ToolResult struct {
	// Content is the structured result payload returned to the model.
	Content map[string]any
	// IsError is true when the result represents a failure, an unknown tool,
	// or an approval denial rather than a successful execution.
	IsError bool
}

// errorResult builds an error ToolResult carrying message under the "error"
// key. The dispatcher returns these for execution faults, unknown tools, and
// approval denials so the model always receives a structured result.
func errorResult(message string) ToolResult {
	return ToolResult{
		Content: map[string]any{"error": message},
		IsError: true,
	}
}

// ExecutionContext carries the Conductor-injected handles a tool needs at
// dispatch time (SPEC §21.1). It is populated by Conductor, never by the agent:
// credentials and engine handles flow through here so agent-supplied params can
// never smuggle or extract a token.
type ExecutionContext struct {
	// ProjectID scopes engine calls (memory, knowledge) to the project.
	ProjectID string
	// IssueID scopes episodic memory and audit correlation to the issue.
	IssueID string
	// AgentRole is the role making the call (audit correlation).
	AgentRole string
	// SessionID is the provider session id (audit correlation).
	SessionID string
	// WorkspacePath is the per-issue workspace the call runs against (used by
	// validation/harness tools).
	WorkspacePath string
	// Credential is the Conductor-resolved secret for the target service,
	// injected here rather than read from params. Tools that need auth read it
	// from the ExecutionContext; engines that hold their own credentials ignore
	// it.
	Credential string
}
