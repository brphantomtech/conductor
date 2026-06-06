package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/audit"
)

func TestApprove_Decisions(t *testing.T) {
	tests := []struct {
		name   string
		tool   string
		policy ApprovalPolicy
		want   Decision
	}{
		{"auto allows query", ToolTrackerQuery, PolicyAuto, DecisionAllow},
		{"auto allows mutate", ToolTrackerMutate, PolicyAuto, DecisionAllow},
		{"review allows query", ToolTrackerQuery, PolicyReviewDestructive, DecisionAllow},
		{"review denies mutate", ToolTrackerMutate, PolicyReviewDestructive, DecisionDeny},
		{"manual denies query", ToolTrackerQuery, PolicyManual, DecisionDeny},
		{"manual denies mutate", ToolTrackerMutate, PolicyManual, DecisionDeny},
		{"unknown policy defaults to auto", ToolTrackerMutate, ApprovalPolicy("bogus"), DecisionAllow},
		{"empty policy defaults to auto", ToolTrackerMutate, ApprovalPolicy(""), DecisionAllow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, approve(tt.tool, tt.policy))
		})
	}
}

func TestNormalizePolicy(t *testing.T) {
	require.Equal(t, PolicyAuto, NormalizePolicy(""))
	require.Equal(t, PolicyAuto, NormalizePolicy("nonsense"))
	require.Equal(t, PolicyManual, NormalizePolicy("manual"))
	require.Equal(t, PolicyReviewDestructive, NormalizePolicy("review_destructive"))
}

func TestDispatch_ReviewDestructiveDeniesMutate(t *testing.T) {
	fk := &fakeTracker{}
	r := NewRegistry()
	r.MustRegister(NewTrackerMutateTool(fk))
	d := NewDispatcher(r)

	res := d.Dispatch(context.Background(),
		Call{Name: ToolTrackerMutate, Arguments: json.RawMessage(`{"mutation":"m"}`)},
		PolicyReviewDestructive, ExecutionContext{})

	require.True(t, res.IsError)
	require.Equal(t, true, res.Content["approval_required"])
	// The mutation never reached the engine.
	require.Empty(t, fk.mutationArgs)
}

func TestDispatch_ManualDeniesAll(t *testing.T) {
	stub := newStub("conductor_x")
	d := newDispatcherWithStub(stub, &recordingAudit{})

	res := d.Dispatch(context.Background(),
		Call{Name: "conductor_x", Arguments: json.RawMessage(`{}`)},
		PolicyManual, ExecutionContext{})

	require.True(t, res.IsError)
	require.Equal(t, true, res.Content["approval_required"])
	require.Equal(t, 0, stub.calls)
}

func TestDispatch_AutoAllowsMutate(t *testing.T) {
	fk := &fakeTracker{}
	r := NewRegistry()
	r.MustRegister(NewTrackerMutateTool(fk))
	d := NewDispatcher(r)

	res := d.Dispatch(context.Background(),
		Call{Name: ToolTrackerMutate, Arguments: json.RawMessage(`{"mutation":"m"}`)},
		PolicyAuto, ExecutionContext{})

	require.False(t, res.IsError)
	require.Equal(t, "m", fk.mutationArgs)
}

// TestCredentialIsolation_AuditRedactsSecrets asserts that a secret passed in
// agent params is redacted in the ToolCalled / ToolResult audit payloads when
// the writer is configured with the Phase 17 redactor, and never appears in the
// result returned to the agent.
func TestCredentialIsolation_AuditRedactsSecrets(t *testing.T) {
	const secret = "sk-super-secret-token"

	rec := &recordingAudit{}
	// Wrap the recorder behind a real audit Writer + Redactor (Phase 17), the
	// production path the dispatcher relies on.
	writer := audit.NewWriter(noopLogger(), audit.WithRedactor(audit.NewRedactor(secret)))
	writer.AddSink(recSink{rec})

	stub := newStub("conductor_x")
	stub.result = ToolResult{Content: map[string]any{"data": "clean"}}
	r := NewRegistry()
	r.MustRegister(stub)
	d := NewDispatcher(r, WithAudit(writer))

	// Agent smuggles a token in params (and a secret-named field).
	args := json.RawMessage(`{"q":"` + secret + `","api_key":"` + secret + `"}`)
	res := d.Dispatch(context.Background(),
		Call{Name: "conductor_x", Arguments: args}, PolicyAuto,
		ExecutionContext{Credential: secret})

	// Result returned to the agent contains no secret.
	resJSON := string(MarshalResult(res))
	require.NotContains(t, resJSON, secret)

	// Audit payloads have the secret redacted on both axes (field name + value).
	for _, e := range rec.events {
		b, err := json.Marshal(e.Payload)
		require.NoError(t, err)
		require.NotContains(t, string(b), secret, "secret leaked in %s payload", e.EventType)
	}
	require.Len(t, rec.ofType(audit.EventToolCalled), 1)
	require.Len(t, rec.ofType(audit.EventToolResult), 1)
}
