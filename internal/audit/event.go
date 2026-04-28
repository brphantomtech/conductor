package audit

import (
	"encoding/json"
	"fmt"
	"time"
)

// EventType is the typed enum for SPEC §17.2's event registry. Using a named
// string keeps type-checking honest while still letting the value flow into
// JSON, SQL, and structured logs verbatim.
type EventType string

// Event types defined by SPEC §17.2. The string values are the canonical
// identifiers persisted in the audit_events table and emitted over the
// WebSocket — do not change them without a migration.
const (
	EventIssueDispatched          EventType = "IssueDispatched"
	EventIssueCancelled           EventType = "IssueCancelled"
	EventIssueReleased            EventType = "IssueReleased"
	EventRunAttemptStarted        EventType = "RunAttemptStarted"
	EventRunAttemptEnded          EventType = "RunAttemptEnded"
	EventPipelineRoleStarted      EventType = "PipelineRoleStarted"
	EventPipelineRoleEnded        EventType = "PipelineRoleEnded"
	EventTurnStarted              EventType = "TurnStarted"
	EventTurnCompleted            EventType = "TurnCompleted"
	EventTurnFailed               EventType = "TurnFailed"
	EventTurnTimedOut             EventType = "TurnTimedOut"
	EventToolCalled               EventType = "ToolCalled"
	EventToolResult               EventType = "ToolResult"
	EventMemoryRead               EventType = "MemoryRead"
	EventMemoryWritten            EventType = "MemoryWritten"
	EventKnowledgeQueried         EventType = "KnowledgeQueried"
	EventDocSearched              EventType = "DocSearched"
	EventValidationCheckRun       EventType = "ValidationCheckRun"
	EventValidationPipelineFailed EventType = "ValidationPipelineFailed"
	EventContextCompacted         EventType = "ContextCompacted"
	EventHarnessViolationDetected EventType = "HarnessViolationDetected"
	EventHarnessEnforcerBlocked   EventType = "HarnessEnforcerBlocked"
	EventGCTaskCreated            EventType = "GCTaskCreated"
	EventKnowledgeIndexed         EventType = "KnowledgeIndexed"
	EventDocStoreSynced           EventType = "DocStoreSynced"
	EventMemoryConsolidated       EventType = "MemoryConsolidated"
	EventConfigReloaded           EventType = "ConfigReloaded"
	EventConfigReloadFailed       EventType = "ConfigReloadFailed"
	EventWorkspaceCreated         EventType = "WorkspaceCreated"
	EventWorkspaceRemoved         EventType = "WorkspaceRemoved"
	EventHookExecuted             EventType = "HookExecuted"
	EventSessionStalled           EventType = "SessionStalled"
	EventRetryScheduled           EventType = "RetryScheduled"
)

// AllEventTypes returns the SPEC §17.2 registry as a deterministic slice.
// Tests cross-check this list against external schemas; the orchestrator
// uses it to validate event_type strings on the WebSocket subscription
// boundary.
func AllEventTypes() []EventType {
	return []EventType{
		EventIssueDispatched,
		EventIssueCancelled,
		EventIssueReleased,
		EventRunAttemptStarted,
		EventRunAttemptEnded,
		EventPipelineRoleStarted,
		EventPipelineRoleEnded,
		EventTurnStarted,
		EventTurnCompleted,
		EventTurnFailed,
		EventTurnTimedOut,
		EventToolCalled,
		EventToolResult,
		EventMemoryRead,
		EventMemoryWritten,
		EventKnowledgeQueried,
		EventDocSearched,
		EventValidationCheckRun,
		EventValidationPipelineFailed,
		EventContextCompacted,
		EventHarnessViolationDetected,
		EventHarnessEnforcerBlocked,
		EventGCTaskCreated,
		EventKnowledgeIndexed,
		EventDocStoreSynced,
		EventMemoryConsolidated,
		EventConfigReloaded,
		EventConfigReloadFailed,
		EventWorkspaceCreated,
		EventWorkspaceRemoved,
		EventHookExecuted,
		EventSessionStalled,
		EventRetryScheduled,
	}
}

// IsKnownEventType reports whether t is in the SPEC §17.2 registry.
func IsKnownEventType(t EventType) bool {
	for _, k := range AllEventTypes() {
		if k == t {
			return true
		}
	}
	return false
}

// AuditEvent is one node in the provenance graph (SPEC §4.1.10). The
// payload is a free-form map that carries event-specific data; consumers
// read it via the matching event type's documented schema.
//
//revive:disable-next-line:exported // SPEC §4.1.10 names this entity AuditEvent — keep the canonical name.
type AuditEvent struct {
	ID            string         `json:"id"`
	Timestamp     time.Time      `json:"timestamp"`
	ProjectID     string         `json:"project_id"`
	IssueID       string         `json:"issue_id,omitempty"`
	SessionID     string         `json:"session_id,omitempty"`
	AgentRole     string         `json:"agent_role,omitempty"`
	EventType     EventType      `json:"event_type"`
	Payload       map[string]any `json:"payload"`
	ParentEventID string         `json:"parent_event_id,omitempty"`
	DurationMS    int64          `json:"duration_ms,omitempty"`
}

// MarshalJSON ensures the timestamp is always emitted in RFC 3339 with
// millisecond precision and UTC offset, matching the format the SQLite
// schema persists. The default time.Time encoding includes nanoseconds and
// can drift with the local zone, neither of which we want in the audit
// log.
func (e AuditEvent) MarshalJSON() ([]byte, error) {
	type alias AuditEvent
	out := struct {
		Timestamp string `json:"timestamp"`
		alias
	}{
		Timestamp: e.Timestamp.UTC().Format("2006-01-02T15:04:05.000Z07:00"),
		alias:     alias(e),
	}
	// Drop the original Timestamp field by zeroing the embedded copy.
	out.alias.Timestamp = time.Time{}

	data, err := json.Marshal(struct {
		ID            string         `json:"id"`
		Timestamp     string         `json:"timestamp"`
		ProjectID     string         `json:"project_id"`
		IssueID       string         `json:"issue_id,omitempty"`
		SessionID     string         `json:"session_id,omitempty"`
		AgentRole     string         `json:"agent_role,omitempty"`
		EventType     EventType      `json:"event_type"`
		Payload       map[string]any `json:"payload"`
		ParentEventID string         `json:"parent_event_id,omitempty"`
		DurationMS    int64          `json:"duration_ms,omitempty"`
	}{
		ID:            e.ID,
		Timestamp:     out.Timestamp,
		ProjectID:     e.ProjectID,
		IssueID:       e.IssueID,
		SessionID:     e.SessionID,
		AgentRole:     e.AgentRole,
		EventType:     e.EventType,
		Payload:       e.Payload,
		ParentEventID: e.ParentEventID,
		DurationMS:    e.DurationMS,
	})
	if err != nil {
		return nil, fmt.Errorf("audit: marshal event: %w", err)
	}
	return data, nil
}

// UnmarshalJSON reverses MarshalJSON, parsing the canonical RFC 3339
// timestamp back into a time.Time with the UTC location preserved.
func (e *AuditEvent) UnmarshalJSON(data []byte) error {
	wire := struct {
		ID            string         `json:"id"`
		Timestamp     string         `json:"timestamp"`
		ProjectID     string         `json:"project_id"`
		IssueID       string         `json:"issue_id,omitempty"`
		SessionID     string         `json:"session_id,omitempty"`
		AgentRole     string         `json:"agent_role,omitempty"`
		EventType     EventType      `json:"event_type"`
		Payload       map[string]any `json:"payload"`
		ParentEventID string         `json:"parent_event_id,omitempty"`
		DurationMS    int64          `json:"duration_ms,omitempty"`
	}{}
	if err := json.Unmarshal(data, &wire); err != nil {
		return fmt.Errorf("audit: unmarshal event: %w", err)
	}
	t, err := time.Parse("2006-01-02T15:04:05.000Z07:00", wire.Timestamp)
	if err != nil {
		// Fall back to RFC 3339 with nanoseconds for tolerance.
		t, err = time.Parse(time.RFC3339Nano, wire.Timestamp)
		if err != nil {
			return fmt.Errorf("audit: parse timestamp %q: %w", wire.Timestamp, err)
		}
	}
	e.ID = wire.ID
	e.Timestamp = t.UTC()
	e.ProjectID = wire.ProjectID
	e.IssueID = wire.IssueID
	e.SessionID = wire.SessionID
	e.AgentRole = wire.AgentRole
	e.EventType = wire.EventType
	e.Payload = wire.Payload
	e.ParentEventID = wire.ParentEventID
	e.DurationMS = wire.DurationMS
	return nil
}
