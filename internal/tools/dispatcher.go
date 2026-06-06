package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rs/zerolog"

	"github.com/conductor-sh/conductor/internal/audit"
)

// auditWriter is the consumer-side subset of the audit Writer the dispatcher
// uses to emit ToolCalled / ToolResult events. *audit.Writer satisfies it; the
// writer applies the Phase 17 redactor to every payload before fan-out, so the
// dispatcher does not redact itself (SPEC §21.1).
type auditWriter interface {
	Write(ctx context.Context, evt audit.AuditEvent) error
}

// Dispatcher routes an agent's tool call to the matching registered tool,
// applies the approval gate, marshals params/result, and emits ToolCalled /
// ToolResult audit events (SPEC §7.3, §21.1, §21.2). Tool execution failures
// become error ToolResults — the model sees the failure and can react — rather
// than aborting the turn; only the dispatcher returns no error for a tool call.
type Dispatcher struct {
	registry *Registry
	audit    auditWriter
	log      zerolog.Logger
}

// DispatcherOption configures a Dispatcher at construction.
type DispatcherOption func(*Dispatcher)

// WithAudit wires the audit writer used for ToolCalled / ToolResult events.
// Without it, audit events are dropped (tools still execute).
func WithAudit(w auditWriter) DispatcherOption {
	return func(d *Dispatcher) {
		if w != nil {
			d.audit = w
		}
	}
}

// WithLogger sets the structured logger.
func WithLogger(l zerolog.Logger) DispatcherOption {
	return func(d *Dispatcher) { d.log = l }
}

// NewDispatcher constructs a Dispatcher over the registry.
func NewDispatcher(registry *Registry, opts ...DispatcherOption) *Dispatcher {
	d := &Dispatcher{registry: registry, log: zerolog.Nop()}
	for _, o := range opts {
		if o != nil {
			o(d)
		}
	}
	return d
}

// Call is one normalized tool invocation the dispatcher executes. It mirrors
// provider.ToolCall but is provider-neutral so internal/tools does not import
// the provider package's event types.
type Call struct {
	// ID is the provider call identifier threaded back to the model.
	ID string
	// Name is the tool name to dispatch.
	Name string
	// Arguments is the raw JSON arguments object the model emitted.
	Arguments json.RawMessage
}

// Dispatch executes one tool call under the given approval policy and execution
// context, returning the ToolResult to feed back to the model. It always
// returns a ToolResult (never a fatal error): unknown tools, invalid params,
// approval denials, and execution faults all map to error ToolResults so the
// dispatch loop can continue. It emits a ToolCalled event before execution and a
// ToolResult event after.
func (d *Dispatcher) Dispatch(
	ctx context.Context, call Call, policy ApprovalPolicy, ec ExecutionContext,
) ToolResult {
	d.emitCalled(ctx, call, ec)

	result := d.execute(ctx, call, policy, ec)
	d.emitResult(ctx, call, result, ec)
	return result
}

// execute is the gate-then-run core of Dispatch, separated so Dispatch can wrap
// it with the two audit events.
func (d *Dispatcher) execute(
	ctx context.Context, call Call, policy ApprovalPolicy, ec ExecutionContext,
) ToolResult {
	tool, ok := d.registry.Lookup(call.Name)
	if !ok {
		return errorResult(fmt.Sprintf("%s: %s", ErrUnknownTool.Error(), call.Name))
	}

	if approve(call.Name, policy) == DecisionDeny {
		return approvalRequiredResult(call.Name, policy)
	}

	params, err := decodeArguments(call.Arguments)
	if err != nil {
		return errorResult(fmt.Sprintf("%s: %v", ErrInvalidParams.Error(), err))
	}

	res, err := tool.Execute(ctx, params, ec)
	if err != nil {
		d.log.Warn().Err(err).Str("tool", call.Name).Msg("tools: dispatch execution error")
		return errorResult(err.Error())
	}
	return res
}

// decodeArguments parses the raw provider arguments JSON into a params map. An
// empty or null arguments object decodes to an empty map (tools with no required
// params still run).
func decodeArguments(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	trimmed := string(raw)
	if trimmed == "null" || trimmed == "" {
		return map[string]any{}, nil
	}
	var params map[string]any
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, fmt.Errorf("decode arguments: %w", err)
	}
	if params == nil {
		params = map[string]any{}
	}
	return params, nil
}

// emitCalled writes a ToolCalled audit event for the call. The agent-supplied
// arguments are included as the event payload's params; the writer's redactor
// scrubs any secret-named fields (SPEC §21.1).
func (d *Dispatcher) emitCalled(ctx context.Context, call Call, ec ExecutionContext) {
	if d.audit == nil {
		return
	}
	var params map[string]any
	if p, err := decodeArguments(call.Arguments); err == nil {
		params = p
	}
	d.write(ctx, audit.AuditEvent{
		ProjectID: ec.ProjectID,
		IssueID:   ec.IssueID,
		SessionID: ec.SessionID,
		AgentRole: ec.AgentRole,
		EventType: audit.EventToolCalled,
		Payload: map[string]any{
			"tool":    call.Name,
			"call_id": call.ID,
			"params":  params,
		},
	})
}

// emitResult writes a ToolResult audit event for the call's outcome.
func (d *Dispatcher) emitResult(ctx context.Context, call Call, result ToolResult, ec ExecutionContext) {
	if d.audit == nil {
		return
	}
	d.write(ctx, audit.AuditEvent{
		ProjectID: ec.ProjectID,
		IssueID:   ec.IssueID,
		SessionID: ec.SessionID,
		AgentRole: ec.AgentRole,
		EventType: audit.EventToolResult,
		Payload: map[string]any{
			"tool":     call.Name,
			"call_id":  call.ID,
			"is_error": result.IsError,
			"result":   result.Content,
		},
	})
}

func (d *Dispatcher) write(ctx context.Context, evt audit.AuditEvent) {
	if err := d.audit.Write(ctx, evt); err != nil {
		d.log.Warn().Err(err).Str("event_type", string(evt.EventType)).Msg("tools: audit write failed")
	}
}

// MarshalResult encodes a ToolResult's content as the JSON payload fed back to
// the model via the provider's tool-result continuation. It never fails: a
// marshal error (which should not happen for map[string]any) degrades to an
// error envelope so the loop always has a result to send.
func MarshalResult(result ToolResult) json.RawMessage {
	content := result.Content
	if content == nil {
		content = map[string]any{}
	}
	b, err := json.Marshal(content)
	if err != nil {
		fallback, _ := json.Marshal(map[string]any{"error": "marshal tool result: " + err.Error()})
		return fallback
	}
	return b
}
