package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/conductor-sh/conductor/internal/audit"
	"github.com/stretchr/testify/require"
)

// recordingAudit captures audit events the dispatcher writes.
type recordingAudit struct{ events []audit.AuditEvent }

func (r *recordingAudit) Write(_ context.Context, evt audit.AuditEvent) error {
	r.events = append(r.events, evt)
	return nil
}

func (r *recordingAudit) ofType(t audit.EventType) []audit.AuditEvent {
	var out []audit.AuditEvent
	for _, e := range r.events {
		if e.EventType == t {
			out = append(out, e)
		}
	}
	return out
}

func newDispatcherWithStub(stub *stubTool, rec *recordingAudit) *Dispatcher {
	r := NewRegistry()
	r.MustRegister(stub)
	return NewDispatcher(r, WithAudit(rec))
}

func TestDispatch_RoutesAndReturnsResult(t *testing.T) {
	stub := newStub("conductor_x")
	stub.result = ToolResult{Content: map[string]any{"answer": 42}}
	rec := &recordingAudit{}
	d := newDispatcherWithStub(stub, rec)

	res := d.Dispatch(context.Background(),
		Call{ID: "c1", Name: "conductor_x", Arguments: json.RawMessage(`{"q":"hi"}`)},
		PolicyAuto, ExecutionContext{ProjectID: "p"})

	require.False(t, res.IsError)
	require.Equal(t, 42, res.Content["answer"])
	require.Equal(t, 1, stub.calls)
	require.Len(t, rec.ofType(audit.EventToolCalled), 1)
	require.Len(t, rec.ofType(audit.EventToolResult), 1)
}

func TestDispatch_UnknownToolReturnsErrorResult(t *testing.T) {
	rec := &recordingAudit{}
	d := NewDispatcher(NewRegistry(), WithAudit(rec))

	res := d.Dispatch(context.Background(),
		Call{ID: "c1", Name: "conductor_missing", Arguments: json.RawMessage(`{}`)},
		PolicyAuto, ExecutionContext{})

	require.True(t, res.IsError)
	require.Contains(t, res.Content["error"], ErrUnknownTool.Error())
	// Still records both events.
	require.Len(t, rec.ofType(audit.EventToolResult), 1)
}

func TestDispatch_ToolErrorBecomesErrorResultNotAbort(t *testing.T) {
	stub := newStub("conductor_x")
	stub.err = errEngine
	d := newDispatcherWithStub(stub, &recordingAudit{})

	res := d.Dispatch(context.Background(),
		Call{Name: "conductor_x", Arguments: json.RawMessage(`{}`)},
		PolicyAuto, ExecutionContext{})

	require.True(t, res.IsError)
	require.Equal(t, errEngine.Error(), res.Content["error"])
}

func TestDispatch_InvalidArgumentsReturnsErrorResult(t *testing.T) {
	stub := newStub("conductor_x")
	d := newDispatcherWithStub(stub, &recordingAudit{})

	res := d.Dispatch(context.Background(),
		Call{Name: "conductor_x", Arguments: json.RawMessage(`{not json`)},
		PolicyAuto, ExecutionContext{})

	require.True(t, res.IsError)
	require.Contains(t, res.Content["error"], ErrInvalidParams.Error())
	require.Equal(t, 0, stub.calls)
}

func TestDispatch_NullAndEmptyArguments(t *testing.T) {
	stub := newStub("conductor_x")
	d := newDispatcherWithStub(stub, &recordingAudit{})

	res := d.Dispatch(context.Background(),
		Call{Name: "conductor_x", Arguments: json.RawMessage(`null`)},
		PolicyAuto, ExecutionContext{})
	require.False(t, res.IsError)

	res = d.Dispatch(context.Background(),
		Call{Name: "conductor_x", Arguments: nil},
		PolicyAuto, ExecutionContext{})
	require.False(t, res.IsError)
	require.Equal(t, 2, stub.calls)
}

func TestMarshalResult(t *testing.T) {
	raw := MarshalResult(ToolResult{Content: map[string]any{"k": "v"}})
	require.JSONEq(t, `{"k":"v"}`, string(raw))
	raw = MarshalResult(ToolResult{})
	require.JSONEq(t, `{}`, string(raw))
}
