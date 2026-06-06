package router

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/config"
	"github.com/conductor-sh/conductor/internal/provider"
	"github.com/conductor-sh/conductor/internal/tracker"
)

func newClassifyRouter(t *testing.T, pr Provider) (*Router, *captureSink) {
	t.Helper()
	w, sink := newCaptureWriter()
	r := New(
		WithProvider(pr),
		WithAudit(w),
		WithConfig(func() config.Config { return config.Defaults() }),
		WithLogger(nopLogger()),
	)
	return r, sink
}

func TestClassify_UnclassifiedIssueClassifiedOnce(t *testing.T) {
	pr := &fakeProvider{defaultResult: provider.TurnResult{Text: "bug\n"}}
	r, sink := newClassifyRouter(t, pr)

	out, err := r.Classify(context.Background(), []tracker.Issue{issue("i1", "A-1", "fix crash")})
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.NotNil(t, out[0].TaskType)
	require.Equal(t, "bug", *out[0].TaskType)
	require.Len(t, pr.recorded(), 1, "provider invoked exactly once")
	require.Equal(t, 1, sink.classifications(), "one classification audit event")
}

func TestClassify_AlreadyClassifiedSkipped(t *testing.T) {
	pr := &fakeProvider{defaultResult: provider.TurnResult{Text: "bug"}}
	r, sink := newClassifyRouter(t, pr)

	iss := issue("i1", "A-1", "already done")
	iss.TaskType = strp("feature")

	out, err := r.Classify(context.Background(), []tracker.Issue{iss})
	require.NoError(t, err)
	require.Equal(t, "feature", *out[0].TaskType, "existing task type preserved")
	require.Empty(t, pr.recorded(), "no provider call for an already-classified issue")
	require.Equal(t, 0, sink.classifications())
}

func TestClassify_ProviderErrorDefaultsToUnknown(t *testing.T) {
	pr := &fakeProvider{startErr: errors.New("boom")}
	r, sink := newClassifyRouter(t, pr)

	out, err := r.Classify(context.Background(), []tracker.Issue{issue("i1", "A-1", "flaky")})
	require.NoError(t, err, "classification never blocks dispatch")
	require.Equal(t, taskTypeUnknown, *out[0].TaskType)
	require.Equal(t, 1, sink.classifications(), "audit event still recorded with unknown")
}

func TestClassify_TurnResultErrorDefaultsToUnknown(t *testing.T) {
	pr := &fakeProvider{defaultResult: provider.TurnResult{Err: errors.New("stream broke")}}
	r, _ := newClassifyRouter(t, pr)

	out, err := r.Classify(context.Background(), []tracker.Issue{issue("i1", "A-1", "x")})
	require.NoError(t, err)
	require.Equal(t, taskTypeUnknown, *out[0].TaskType)
}

func TestNormalizeTaskType(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"exact", "bug", "bug"},
		{"trailing newline", "feature\n", "feature"},
		{"uppercase", "REFACTOR", "refactor"},
		{"with punctuation", "docs.", "docs"},
		{"sentence first word", "investigation - because reasons", "investigation"},
		{"unrecognized", "chore", "unknown"},
		{"empty", "", "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, normalizeTaskType(tt.in))
		})
	}
}
