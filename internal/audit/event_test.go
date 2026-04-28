package audit_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/audit"
)

func TestEventTypeRegistry_Complete(t *testing.T) {
	t.Parallel()

	// SPEC §17.2 declares 33 event types — every entry of the SPEC table
	// must round-trip through the typed enum.
	all := audit.AllEventTypes()
	require.Len(t, all, 33,
		"SPEC §17.2 lists 33 event types; AllEventTypes returned %d", len(all))

	seen := map[audit.EventType]bool{}
	for _, et := range all {
		require.False(t, seen[et], "duplicate event type %q", et)
		seen[et] = true
		require.True(t, audit.IsKnownEventType(et))
		require.NotEmpty(t, string(et))
	}

	require.False(t, audit.IsKnownEventType("NotARealEvent"))
}

func TestAuditEvent_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	original := audit.AuditEvent{
		ID:            "evt-123",
		Timestamp:     time.Date(2026, 4, 28, 12, 34, 56, 789_000_000, time.UTC),
		ProjectID:     "proj-A",
		IssueID:       "ABC-1",
		SessionID:     "run-9-turn-2",
		AgentRole:     "coder",
		EventType:     audit.EventTurnStarted,
		Payload:       map[string]any{"prompt_tokens": float64(123)},
		ParentEventID: "evt-100",
		DurationMS:    250,
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	// Sanity check: the canonical timestamp format is RFC 3339 with
	// millisecond precision and a UTC offset indicator.
	require.Contains(t, string(data), `"timestamp":"2026-04-28T12:34:56.789Z"`)

	var decoded audit.AuditEvent
	require.NoError(t, json.Unmarshal(data, &decoded))

	require.Equal(t, original.ID, decoded.ID)
	require.Equal(t, original.ProjectID, decoded.ProjectID)
	require.Equal(t, original.EventType, decoded.EventType)
	require.Equal(t, original.IssueID, decoded.IssueID)
	require.Equal(t, original.ParentEventID, decoded.ParentEventID)
	require.Equal(t, original.DurationMS, decoded.DurationMS)
	require.True(t, original.Timestamp.Equal(decoded.Timestamp),
		"round-tripped timestamp must equal original (got %v vs %v)",
		decoded.Timestamp, original.Timestamp)
	require.Equal(t, original.Payload, decoded.Payload)
}

func TestAuditEvent_OmitsZeroOptionalFields(t *testing.T) {
	t.Parallel()
	evt := audit.AuditEvent{
		ID:        "evt-1",
		Timestamp: time.Now().UTC(),
		ProjectID: "proj",
		EventType: audit.EventConfigReloaded,
	}
	data, err := json.Marshal(evt)
	require.NoError(t, err)

	// Optional fields with zero values should be elided so the JSONL
	// log stays compact; consumers tolerate missing fields per §17.3.
	for _, omitted := range []string{"issue_id", "session_id", "agent_role", "parent_event_id", "duration_ms"} {
		require.NotContains(t, string(data), omitted,
			"%q must be omitted when empty", omitted)
	}
}
