package tracker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/config"
)

// newLinearTestAdapter builds a Linear adapter pointed at the supplied
// handler. The handler is expected to recognize at minimum the viewer
// ping that runs at construction time.
//
// Pass `withoutPing: true` to skip the constructor and return an adapter
// that hasn't validated auth — useful for tests that exercise non-ctor
// paths in isolation.
func newLinearTestAdapter(t *testing.T, handler http.Handler) (*linearAdapter, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)

	cfg := config.Tracker{
		Kind:         "linear",
		APIKey:       "lin_api_test",
		ProjectSlug:  "test-project",
		Endpoint:     srv.URL,
		ActiveStates: []string{"Todo", "In Progress"},
	}
	opts := applyOptions([]Option{WithHTTPClient(srv.Client())})
	a, err := newLinearAdapter(cfg, opts)
	require.NoError(t, err)
	return a, srv
}

// readGQLBody decodes the GraphQL body of an incoming request. Used by
// handlers to dispatch on the query name.
func readGQLBody(t *testing.T, r *http.Request) (query string, variables map[string]any) {
	t.Helper()
	buf, err := io.ReadAll(r.Body)
	require.NoError(t, err)
	var env struct {
		Query     string                 `json:"query"`
		Variables map[string]any         `json:"variables"`
	}
	require.NoError(t, json.Unmarshal(buf, &env))
	return env.Query, env.Variables
}

func TestLinear_NewAdapter_Validation(t *testing.T) {
	t.Run("missing api key", func(t *testing.T) {
		_, err := newLinearAdapter(
			config.Tracker{Kind: "linear", ProjectSlug: "p"},
			defaultOptions(),
		)
		require.True(t, errors.Is(err, ErrMissingAPIKey))
	})
	t.Run("missing project slug", func(t *testing.T) {
		_, err := newLinearAdapter(
			config.Tracker{Kind: "linear", APIKey: "k"},
			defaultOptions(),
		)
		require.True(t, errors.Is(err, ErrMissingProjectID))
	})
}

func TestLinear_NewAdapter_ViewerPingValidatesKey(t *testing.T) {
	pinged := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "lin_api_test", r.Header.Get("Authorization"),
			"Linear adapter must send raw api_key (no Bearer prefix)")
		q, _ := readGQLBody(t, r)
		require.Contains(t, q, "Viewer")
		pinged = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(readFixture(t, "linear_viewer.json"))
	})
	a, srv := newLinearTestAdapter(t, handler)
	defer srv.Close()

	require.True(t, pinged, "construction must call the viewer query")
	require.Equal(t, "viewer-test", a.viewerID,
		"viewerID must be cached after construction")
}

func TestLinear_NewAdapter_BadCredentialsFailsCtor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer srv.Close()

	cfg := config.Tracker{
		Kind:        "linear",
		APIKey:      "bad",
		ProjectSlug: "p",
		Endpoint:    srv.URL,
	}
	opts := applyOptions([]Option{WithHTTPClient(srv.Client())})
	_, err := newLinearAdapter(cfg, opts)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrResponseError))
	require.Contains(t, err.Error(), "http 401")
}

func TestLinear_FetchCandidateIssues_TwoPagePagination(t *testing.T) {
	calls := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q, vars := readGQLBody(t, r)
		w.Header().Set("Content-Type", "application/json")

		switch {
		case containsViewer(q):
			_, _ = w.Write(readFixture(t, "linear_viewer.json"))
		case containsCandidate(q):
			calls++
			switch calls {
			case 1:
				// First call must not carry the `after` variable.
				_, hasAfter := vars["after"]
				require.False(t, hasAfter, "first page must not pass after cursor")
				_, _ = w.Write(readFixture(t, "linear_candidates_page1.json"))
			case 2:
				require.Equal(t, "cursor-2", vars["after"],
					"second page must use cursor-2 from page 1")
				_, _ = w.Write(readFixture(t, "linear_candidates_page2.json"))
			default:
				t.Fatalf("unexpected candidate call #%d", calls)
			}
		default:
			t.Fatalf("unexpected query: %s", q)
		}
	})
	a, srv := newLinearTestAdapter(t, handler)
	defer srv.Close()

	issues, err := a.FetchCandidateIssues(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, calls)

	// Page1 had 2 nodes, page2 had 1 → 3 total.
	require.Len(t, issues, 3)

	first := issues[0]
	require.Equal(t, "issue-1", first.ID)
	require.Equal(t, "ABC-1", first.Identifier)
	require.Equal(t, []string{"bug", "frontend"}, first.Labels,
		"labels must be lowercased + deduped + sorted")
	require.NotNil(t, first.BranchName)
	require.Equal(t, "abc-1-add-login-form", *first.BranchName)
	require.Equal(t, "Todo", first.State, "state preserved verbatim")

	second := issues[1]
	require.Empty(t, second.Labels, "issue with no labels must yield empty slice")
	require.Nil(t, second.BranchName, "empty branchName must produce nil pointer")
	require.Len(t, second.BlockedBy, 1, "blockedBy edge must propagate")
	br := second.BlockedBy[0]
	require.NotNil(t, br.ID)
	require.Equal(t, "issue-99", *br.ID)
	require.NotNil(t, br.Identifier)
	require.Equal(t, "ABC-99", *br.Identifier)
	require.NotNil(t, br.State)
	require.Equal(t, "In Review", *br.State)
}

func TestLinear_FetchCandidateIssues_BlockedByPropagated(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q, _ := readGQLBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		if containsViewer(q) {
			_, _ = w.Write(readFixture(t, "linear_viewer.json"))
			return
		}
		_, _ = w.Write(readFixture(t, "linear_blocked_by.json"))
	})
	a, srv := newLinearTestAdapter(t, handler)
	defer srv.Close()

	issues, err := a.FetchCandidateIssues(context.Background())
	require.NoError(t, err)
	require.Len(t, issues, 1)
	require.Len(t, issues[0].BlockedBy, 1)
}

func TestLinear_FetchIssuesByStates_EmptyReturnsEmpty(t *testing.T) {
	pinged := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		pinged = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(readFixture(t, "linear_viewer.json"))
	})
	a, srv := newLinearTestAdapter(t, handler)
	defer srv.Close()
	require.True(t, pinged, "ctor must run the viewer ping")

	// Reset flag — empty-states call must not produce additional traffic.
	pinged = false
	issues, err := a.FetchIssuesByStates(context.Background(), nil)
	require.NoError(t, err)
	require.Empty(t, issues)
	require.False(t, pinged, "empty states must not contact the server")
}

func TestLinear_FetchIssueStatesByIDs_BatchAndOmitUnknown(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q, vars := readGQLBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		if containsViewer(q) {
			_, _ = w.Write(readFixture(t, "linear_viewer.json"))
			return
		}
		require.Contains(t, q, "IssueStatesByIDs")
		ids, _ := vars["ids"].([]any)
		// Expect ids = ["issue-1", "issue-2", "issue-3", "does-not-exist"]
		require.Len(t, ids, 4)
		_, _ = w.Write(readFixture(t, "linear_states_batch.json"))
	})
	a, srv := newLinearTestAdapter(t, handler)
	defer srv.Close()

	states, err := a.FetchIssueStatesByIDs(context.Background(),
		[]string{"issue-1", "issue-2", "issue-3", "does-not-exist"})
	require.NoError(t, err)

	// Fixture only returns three nodes; the fourth must be omitted.
	require.Len(t, states, 3)
	require.Equal(t, "Todo", states["issue-1"])
	require.Equal(t, "In Progress", states["issue-2"])
	require.Equal(t, "Done", states["issue-3"])
	require.NotContains(t, states, "does-not-exist")
}

func TestLinear_FetchIssueStatesByIDs_EmptyReturnsEmpty(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(readFixture(t, "linear_viewer.json"))
	})
	a, srv := newLinearTestAdapter(t, handler)
	defer srv.Close()

	states, err := a.FetchIssueStatesByIDs(context.Background(), nil)
	require.NoError(t, err)
	require.Empty(t, states)
}

func TestLinear_GraphQLErrors_Wrapped(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q, _ := readGQLBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		if containsViewer(q) {
			_, _ = w.Write(readFixture(t, "linear_viewer.json"))
			return
		}
		_, _ = w.Write(readFixture(t, "linear_graphql_errors.json"))
	})
	a, srv := newLinearTestAdapter(t, handler)
	defer srv.Close()

	_, err := a.FetchCandidateIssues(context.Background())
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrGraphQLErrors))
	require.Contains(t, err.Error(), "Argument 'projectId'")
}

func TestLinear_ContextCancelled_WrapsRequestFailed(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(readFixture(t, "linear_viewer.json"))
	})
	a, srv := newLinearTestAdapter(t, handler)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := a.FetchCandidateIssues(ctx)
	require.True(t, errors.Is(err, ErrRequestFailed))
}

func TestLinear_ExecuteQuery_PassesThroughData(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q, vars := readGQLBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		if containsViewer(q) {
			_, _ = w.Write(readFixture(t, "linear_viewer.json"))
			return
		}
		// Echo a stub response shaped like a Linear team query.
		require.Equal(t, "team-1", vars["id"])
		_, _ = w.Write([]byte(`{"data":{"team":{"id":"team-1","name":"Engineering"}}}`))
	})
	a, srv := newLinearTestAdapter(t, handler)
	defer srv.Close()

	out, err := a.ExecuteQuery(context.Background(),
		`query Team($id: ID!) { team(id: $id) { id name } }`,
		map[string]any{"id": "team-1"})
	require.NoError(t, err)
	team, ok := out["team"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "team-1", team["id"])
	require.Equal(t, "Engineering", team["name"])
}

func TestLinear_ExecuteMutation_GraphQLErrors(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q, _ := readGQLBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		if containsViewer(q) {
			_, _ = w.Write(readFixture(t, "linear_viewer.json"))
			return
		}
		_, _ = w.Write(readFixture(t, "linear_graphql_errors.json"))
	})
	a, srv := newLinearTestAdapter(t, handler)
	defer srv.Close()

	out, err := a.ExecuteMutation(context.Background(),
		`mutation { issueCreate(input: {}) { success } }`, nil)
	require.True(t, errors.Is(err, ErrGraphQLErrors))
	require.Nil(t, out)
}

// containsViewer / containsCandidate are tiny readability helpers so
// the handler dispatch logic above stays self-documenting.
func containsViewer(q string) bool       { return contains(q, "Viewer") }
func containsCandidate(q string) bool    { return contains(q, "CandidateIssues") }

// contains is a strings.Contains shim that avoids pulling the strings
// package into this test file (linting prefers explicit imports per
// file; the helper here keeps the file imports tight).
func contains(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
