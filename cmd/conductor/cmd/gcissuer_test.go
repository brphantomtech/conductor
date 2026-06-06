package cmd

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/config"
	"github.com/conductor-sh/conductor/internal/harness"
	"github.com/conductor-sh/conductor/internal/tracker"
)

// fakeTrackerAdapter is a minimal tracker.Adapter for GC issuer tests. Only
// ExecuteQuery/ExecuteMutation are exercised; the rest return zero values.
type fakeTrackerAdapter struct {
	queryPath   string
	queryOut    map[string]any
	mutatePath  string
	mutateBody  map[string]any
	mutateOut   map[string]any
	mutateCalls int
}

func (f *fakeTrackerAdapter) FetchCandidateIssues(context.Context) ([]tracker.Issue, error) {
	return nil, nil
}
func (f *fakeTrackerAdapter) FetchIssuesByStates(context.Context, []string) ([]tracker.Issue, error) {
	return nil, nil
}
func (f *fakeTrackerAdapter) FetchIssueStatesByIDs(context.Context, []string) (map[string]string, error) {
	return nil, nil
}
func (f *fakeTrackerAdapter) ExecuteQuery(_ context.Context, q string, _ map[string]any) (map[string]any, error) {
	f.queryPath = q
	return f.queryOut, nil
}
func (f *fakeTrackerAdapter) ExecuteMutation(_ context.Context, m string, vars map[string]any) (map[string]any, error) {
	f.mutatePath = m
	f.mutateBody = vars
	f.mutateCalls++
	return f.mutateOut, nil
}

func TestExtractRuleID(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"marker in html comment", "text\n<!-- conductor-gc-rule-id:no-todos -->", "no-todos"},
		{"marker with trailing newline", "conductor-gc-rule-id:rule_1\nmore", "rule_1"},
		{"no marker", "just a body", ""},
		{"marker at end", "x conductor-gc-rule-id:r9", "r9"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, extractRuleID(tt.body))
		})
	}
}

func TestGithubGCIssuer_OpenGCRuleIDs(t *testing.T) {
	fk := &fakeTrackerAdapter{queryOut: map[string]any{
		"items": []any{
			map[string]any{"body": "blah <!-- conductor-gc-rule-id:r1 -->"},
			map[string]any{"body": "no marker here"},
			map[string]any{"body": "x conductor-gc-rule-id:r2 y"},
		},
	}}
	issuer := githubGCIssuer{exec: fk, ownerRepo: "acme/widgets"}

	ids, err := issuer.OpenGCRuleIDs(context.Background(), "gc")
	require.NoError(t, err)
	require.Contains(t, ids, "r1")
	require.Contains(t, ids, "r2")
	require.Len(t, ids, 2)
	require.Contains(t, fk.queryPath, "/search/issues")
	require.Contains(t, fk.queryPath, "repo%3Aacme%2Fwidgets")
}

func TestGithubGCIssuer_CreateGCIssue(t *testing.T) {
	fk := &fakeTrackerAdapter{mutateOut: map[string]any{"number": float64(42)}}
	issuer := githubGCIssuer{exec: fk, ownerRepo: "acme/widgets"}

	id, err := issuer.CreateGCIssue(context.Background(), harness.GCIssue{
		Title:       "[GC] x",
		Description: "body",
		Label:       "gc",
	})
	require.NoError(t, err)
	require.Equal(t, "gh-42", id)
	require.Equal(t, "/repos/acme/widgets/issues", fk.mutatePath)
	require.Equal(t, "[GC] x", fk.mutateBody["title"])
	require.Equal(t, []string{"gc"}, fk.mutateBody["labels"])
}

func TestNewTrackerIssuer_KindSelection(t *testing.T) {
	gh := config.Config{Tracker: config.Tracker{Kind: tracker.KindGitHub, ProjectID: "a/b"}}
	require.NotNil(t, newTrackerIssuer(gh, &fakeTrackerAdapter{}))

	lin := config.Config{Tracker: config.Tracker{Kind: tracker.KindLinear}}
	require.Nil(t, newTrackerIssuer(lin, &fakeTrackerAdapter{}), "linear issuer is a TODO; returns nil")
}
