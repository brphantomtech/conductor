package audit_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/audit"
	"github.com/conductor-sh/conductor/internal/db"
)

// fakeSink captures every event for assertions and (optionally) returns an
// error to exercise the multi-sink fan-out failure path.
type fakeSink struct {
	mu     sync.Mutex
	events []audit.AuditEvent
	err    error
	closed bool
}

func (f *fakeSink) Write(_ context.Context, evt audit.AuditEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, evt)
	return f.err
}

func (f *fakeSink) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func TestWriter_FillsIDAndTimestamp(t *testing.T) {
	t.Parallel()

	w := audit.NewWriter(zerolog.Nop())
	sink := &fakeSink{}
	w.AddSink(sink)

	require.NoError(t, w.Write(context.Background(), audit.AuditEvent{
		ProjectID: "proj",
		EventType: audit.EventTurnStarted,
	}))

	require.Len(t, sink.events, 1)
	got := sink.events[0]
	require.NotEmpty(t, got.ID, "Writer must populate empty IDs")
	require.False(t, got.Timestamp.IsZero(), "Writer must populate empty timestamps")
	require.NotNil(t, got.Payload, "Writer must default Payload to non-nil map")
	require.WithinDuration(t, time.Now().UTC(), got.Timestamp, 5*time.Second)
}

func TestWriter_PreservesProvidedID(t *testing.T) {
	t.Parallel()

	w := audit.NewWriter(zerolog.Nop())
	sink := &fakeSink{}
	w.AddSink(sink)

	require.NoError(t, w.Write(context.Background(), audit.AuditEvent{
		ID:        "caller-supplied-id",
		ProjectID: "proj",
		EventType: audit.EventTurnStarted,
	}))
	require.Equal(t, "caller-supplied-id", sink.events[0].ID)
}

func TestWriter_FanoutContinuesAfterSinkError(t *testing.T) {
	t.Parallel()

	w := audit.NewWriter(zerolog.Nop())
	failing := &fakeSink{err: errors.New("upstream down")}
	ok := &fakeSink{}
	w.AddSink(failing)
	w.AddSink(ok)

	err := w.Write(context.Background(), audit.AuditEvent{
		ProjectID: "proj",
		EventType: audit.EventTurnStarted,
	})

	require.Error(t, err, "fan-out failures must surface to caller")
	require.Len(t, ok.events, 1, "second sink must still receive event after first sink fails")
	require.Len(t, failing.events, 1, "failing sink still saw the event")
}

func TestWriter_ParentEventIDChaining(t *testing.T) {
	t.Parallel()

	w := audit.NewWriter(zerolog.Nop())
	sink := &fakeSink{}
	w.AddSink(sink)

	parent := audit.AuditEvent{ProjectID: "proj", EventType: audit.EventRunAttemptStarted}
	require.NoError(t, w.Write(context.Background(), parent))
	parentID := sink.events[0].ID

	child := audit.AuditEvent{
		ProjectID:     "proj",
		EventType:     audit.EventTurnStarted,
		ParentEventID: parentID,
	}
	require.NoError(t, w.Write(context.Background(), child))
	require.Equal(t, parentID, sink.events[1].ParentEventID,
		"child event must carry the parent's ID for provenance traversal (SPEC §17.1)")
}

func TestWriter_CloseClosesAllSinks(t *testing.T) {
	t.Parallel()

	w := audit.NewWriter(zerolog.Nop())
	a := &fakeSink{}
	b := &fakeSink{}
	w.AddSink(a)
	w.AddSink(b)
	require.NoError(t, w.Close())
	require.True(t, a.closed)
	require.True(t, b.closed)
}

func TestDBSink_PersistsEvent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	d, err := db.Open(ctx, db.Options{Driver: db.DriverSQLite, DSN: ":memory:"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	_, err = db.Migrate(ctx, d)
	require.NoError(t, err)

	w := audit.NewWriter(zerolog.Nop())
	w.AddSink(audit.NewDBSink(d))
	t.Cleanup(func() { _ = w.Close() })

	require.NoError(t, w.Write(ctx, audit.AuditEvent{
		ID:        "evt-1",
		ProjectID: "proj",
		EventType: audit.EventConfigReloaded,
		Payload:   map[string]any{"hash": "abc123"},
	}))

	var (
		eventType string
		payload   string
	)
	row := d.SQL().QueryRowContext(ctx,
		`SELECT event_type, payload FROM audit_events WHERE id = ?`, "evt-1")
	require.NoError(t, row.Scan(&eventType, &payload))
	require.Equal(t, string(audit.EventConfigReloaded), eventType)

	var pl map[string]any
	require.NoError(t, json.Unmarshal([]byte(payload), &pl))
	require.Equal(t, "abc123", pl["hash"])
}

func TestJSONLSink_AppendsOneRecordPerEvent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, ".conductor", "audit.jsonl")

	sink, err := audit.NewJSONLSink(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sink.Close() })

	w := audit.NewWriter(zerolog.Nop())
	w.AddSink(sink)

	for i := 0; i < 3; i++ {
		require.NoError(t, w.Write(context.Background(), audit.AuditEvent{
			ProjectID: "proj",
			EventType: audit.EventTurnStarted,
			Payload:   map[string]any{"i": float64(i)},
		}))
	}
	require.NoError(t, sink.Close())

	f, err := os.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })

	scanner := bufio.NewScanner(f)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	require.NoError(t, scanner.Err())
	require.Len(t, lines, 3, "one NDJSON record per event")

	for _, line := range lines {
		var evt audit.AuditEvent
		require.NoError(t, json.Unmarshal([]byte(line), &evt))
		require.Equal(t, "proj", evt.ProjectID)
		require.Equal(t, audit.EventTurnStarted, evt.EventType)
		require.NotEmpty(t, evt.ID)
	}
}

func TestJSONLSink_RejectsEmptyPath(t *testing.T) {
	t.Parallel()
	_, err := audit.NewJSONLSink("")
	require.Error(t, err)
}
