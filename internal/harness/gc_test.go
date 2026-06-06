package harness

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/audit"
	"github.com/conductor-sh/conductor/internal/config"
)

// fakeTracker records created GC issues and reports pre-existing open ones for
// dedup.
type fakeTracker struct {
	mu       sync.Mutex
	existing map[string]struct{}
	created  []GCIssue
	nextID   int
}

func (f *fakeTracker) OpenGCRuleIDs(context.Context, string) (map[string]struct{}, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[string]struct{}, len(f.existing))
	for k := range f.existing {
		out[k] = struct{}{}
	}
	return out, nil
}

func (f *fakeTracker) CreateGCIssue(_ context.Context, issue GCIssue) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, issue)
	f.nextID++
	return "ISSUE-" + string(rune('0'+f.nextID)), nil
}

func (f *fakeTracker) createdCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.created)
}

func gcConfig() config.Config {
	return config.Config{
		HarnessRules: []config.HarnessRule{
			{ID: "auto", Name: "auto rule", Severity: "warning", Check: "auto", AutoFix: true, FixHint: "fix me"},
			{ID: "manual", Name: "manual rule", Severity: "error", Check: "manual", AutoFix: false},
		},
		Enforcement: config.Enforcement{
			GCIssueLabel: "gc_task",
			GCIssueState: "backlog",
		},
	}
}

func newGCEnforcer(t *testing.T, cfg config.Config, tracker TrackerIssuer) (*Enforcer, *recordingSink) {
	t.Helper()
	sink := &recordingSink{}
	w := audit.NewWriter(zerolog.Nop())
	w.AddSink(sink)
	f := fakeFactory{scripts: map[string]string{"auto": failScript(), "manual": failScript()}}
	r := NewRunner(f)
	e := NewEnforcer(r, staticConfig(cfg), WithEnforcerAudit(w), WithEnforcerTracker(tracker))
	return e, sink
}

func TestRunGC_CreatesIssueForAutoFixViolation(t *testing.T) {
	tr := &fakeTracker{existing: map[string]struct{}{}}
	e, sink := newGCEnforcer(t, gcConfig(), tr)

	created, err := e.RunGC(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, created)
	require.Len(t, tr.created, 1)
	require.Equal(t, "auto", tr.created[0].RuleID)
	require.Contains(t, tr.created[0].Title, "[GC] auto rule:")
	require.Equal(t, "gc_task", tr.created[0].Label)
	require.Equal(t, "backlog", tr.created[0].State)
	require.Contains(t, tr.created[0].Description, gcRuleMarker+"auto")

	var gcEvents int
	for _, ev := range sink.events {
		if ev.EventType == audit.EventGCTaskCreated {
			gcEvents++
		}
	}
	require.Equal(t, 1, gcEvents)
}

func TestRunGC_NonAutoFixCreatesNothing(t *testing.T) {
	cfg := config.Config{
		HarnessRules: []config.HarnessRule{
			{ID: "manual", Severity: "error", Check: "manual", AutoFix: false},
		},
		Enforcement: config.Enforcement{GCIssueLabel: "gc_task"},
	}
	tr := &fakeTracker{existing: map[string]struct{}{}}
	sink := &recordingSink{}
	w := audit.NewWriter(zerolog.Nop())
	w.AddSink(sink)
	f := fakeFactory{scripts: map[string]string{"manual": failScript()}}
	e := NewEnforcer(NewRunner(f), staticConfig(cfg), WithEnforcerAudit(w), WithEnforcerTracker(tr))

	created, err := e.RunGC(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, created)
	require.Empty(t, tr.created)
}

func TestRunGC_DedupPreventsDuplicate(t *testing.T) {
	tr := &fakeTracker{existing: map[string]struct{}{"auto": {}}}
	e, _ := newGCEnforcer(t, gcConfig(), tr)

	created, err := e.RunGC(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, created, "an existing open GC issue must suppress a duplicate")
	require.Empty(t, tr.created)
}

func TestStartGC_EmptyCronIsNoOp(t *testing.T) {
	e, _ := newGCEnforcer(t, gcConfig(), &fakeTracker{existing: map[string]struct{}{}})
	require.NoError(t, e.StartGC(context.Background(), cron.New()))
}

func TestStartGC_SchedulesAndRuns(t *testing.T) {
	cfg := gcConfig()
	cfg.Enforcement.GCScheduleCron = "@every 100ms"
	tr := &fakeTracker{existing: map[string]struct{}{}}
	e, _ := newGCEnforcer(t, cfg, tr)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := cron.New()
	require.NoError(t, e.StartGC(ctx, c))

	require.Eventually(t, func() bool {
		return tr.createdCount() > 0
	}, 2*time.Second, 20*time.Millisecond, "scheduled GC must create the auto-fix issue")
}

func TestStartGC_InvalidCronErrors(t *testing.T) {
	cfg := gcConfig()
	cfg.Enforcement.GCScheduleCron = "not-a-cron"
	e, _ := newGCEnforcer(t, cfg, &fakeTracker{existing: map[string]struct{}{}})
	require.Error(t, e.StartGC(context.Background(), cron.New()))
}

func TestRunGC_NoTrackerIsNoOp(t *testing.T) {
	f := fakeFactory{scripts: map[string]string{"auto": failScript()}}
	e := NewEnforcer(NewRunner(f), staticConfig(gcConfig()))
	created, err := e.RunGC(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, created)
}
