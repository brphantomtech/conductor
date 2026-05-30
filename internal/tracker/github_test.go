package tracker

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/config"
)

// readFixture loads a JSON fixture file from testdata/. Fixtures are
// the only data source for adapter tests (SPEC compliance: no live API).
func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err, "load fixture %s", name)
	return data
}

// newGithubTestAdapter spins up an httptest.Server with the supplied
// handler, builds an adapter pointed at it, and returns both. The
// caller closes the server.
func newGithubTestAdapter(t *testing.T, handler http.Handler) (*githubAdapter, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)

	cfg := config.Tracker{
		Kind:      "github",
		APIKey:    "ghp_test",
		ProjectID: "owner/repo",
		Endpoint:  srv.URL,
	}
	opts := applyOptions([]Option{WithHTTPClient(srv.Client())})
	a, err := newGithubAdapter(cfg, opts)
	require.NoError(t, err)
	return a, srv
}

func TestGithub_NewAdapter_Validation(t *testing.T) {
	t.Run("missing api key", func(t *testing.T) {
		_, err := newGithubAdapter(config.Tracker{Kind: "github", ProjectID: "o/r"}, defaultOptions())
		require.True(t, errors.Is(err, ErrMissingAPIKey))
	})
	t.Run("missing project_id", func(t *testing.T) {
		_, err := newGithubAdapter(config.Tracker{Kind: "github", APIKey: "k"}, defaultOptions())
		require.True(t, errors.Is(err, ErrMissingProjectID))
	})
	t.Run("malformed project_id", func(t *testing.T) {
		_, err := newGithubAdapter(
			config.Tracker{Kind: "github", APIKey: "k", ProjectID: "no-slash"},
			defaultOptions(),
		)
		require.True(t, errors.Is(err, ErrMissingProjectID))
	})
}

func TestGithub_FetchCandidateIssues_FollowsLinkPagination(t *testing.T) {
	calls := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer ghp_test", r.Header.Get("Authorization"))
		require.Equal(t, githubAcceptHeader, r.Header.Get("Accept"))
		require.Equal(t, githubAPIVersion, r.Header.Get("X-GitHub-Api-Version"))

		page := r.URL.Query().Get("page")
		switch page {
		case "1":
			calls++
			// Set Link header pointing to page=2 on the SAME server.
			next := r.Host
			if !strings.HasPrefix(next, "http") {
				next = "http://" + next
			}
			w.Header().Set("Link",
				`<`+next+`/repos/owner/repo/issues?state=open&per_page=100&page=2>; rel="next"`)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(readFixture(t, "github_candidates_page1.json"))
		case "2":
			calls++
			// No Link header → terminates pagination.
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(readFixture(t, "github_candidates_page2.json"))
		default:
			t.Fatalf("unexpected page %q", page)
		}
	})
	a, srv := newGithubTestAdapter(t, handler)
	defer srv.Close()

	issues, err := a.FetchCandidateIssues(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, calls, "both pages must be fetched")

	// Page1 had 3 rows; one was a PR (must be filtered). Page2 had 2 rows.
	// Expected: 2 + 2 = 4 issues.
	require.Len(t, issues, 4)

	// Verify first issue's normalization details.
	first := issues[0]
	require.Equal(t, "1001", first.ID)
	require.Equal(t, "gh-1", first.Identifier)
	require.Equal(t, "open", first.State)
	require.Equal(t, []string{"bug", "frontend"}, first.Labels,
		"labels must be lowercased + deduped + sorted")
	require.NotNil(t, first.URL)
	require.Equal(t, "https://github.com/owner/repo/issues/1", *first.URL)
	require.NotNil(t, first.CreatedAt)
	require.NotNil(t, first.UpdatedAt)
	require.Nil(t, first.BranchName, "GitHub adapter must leave BranchName nil")

	// Issue 4 carries a tracked_in_issues entry.
	for _, iss := range issues {
		if iss.Identifier == "gh-4" {
			require.Len(t, iss.BlockedBy, 1)
			require.NotNil(t, iss.BlockedBy[0].Identifier)
			require.Equal(t, "gh-1", *iss.BlockedBy[0].Identifier)
			require.Nil(t, iss.BlockedBy[0].ID,
				"GitHub adapter must leave BlockerRef.ID nil")
			require.Nil(t, iss.BlockedBy[0].State,
				"GitHub adapter must leave BlockerRef.State nil")
			return
		}
	}
	t.Fatal("issue gh-4 not found")
}

func TestGithub_FetchCandidateIssues_FiltersPullRequests(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(readFixture(t, "github_candidates_page1.json"))
	})
	a, srv := newGithubTestAdapter(t, handler)
	defer srv.Close()

	issues, err := a.FetchCandidateIssues(context.Background())
	require.NoError(t, err)
	for _, iss := range issues {
		require.NotEqual(t, "gh-2", iss.Identifier,
			"PR row (number 2) must be filtered out")
	}
}

func TestGithub_FetchCandidateIssues_EmptyBlockedBy_NotNil(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(readFixture(t, "github_candidates_page1.json"))
	})
	a, srv := newGithubTestAdapter(t, handler)
	defer srv.Close()

	issues, err := a.FetchCandidateIssues(context.Background())
	require.NoError(t, err)
	for _, iss := range issues {
		require.NotNil(t, iss.BlockedBy,
			"BlockedBy must be non-nil empty slice, not nil — issue=%s", iss.Identifier)
	}
}

func TestGithub_FetchIssuesByStates_StateFiltering(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "closed", r.URL.Query().Get("state"),
			"single 'closed' state must request state=closed directly")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	})
	a, srv := newGithubTestAdapter(t, handler)
	defer srv.Close()

	_, err := a.FetchIssuesByStates(context.Background(), []string{"closed"})
	require.NoError(t, err)
}

func TestGithub_FetchIssuesByStates_EmptyReturnsEmpty(t *testing.T) {
	handler := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("handler must not be invoked when states slice is empty")
	})
	a, srv := newGithubTestAdapter(t, handler)
	defer srv.Close()

	issues, err := a.FetchIssuesByStates(context.Background(), nil)
	require.NoError(t, err)
	require.Empty(t, issues)
}

func TestGithub_FetchIssueStatesByIDs_BatchAndOmitUnknown(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/issues/1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(readFixture(t, "github_issue_single.json"))
		case "/repos/owner/repo/issues/99":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"Not Found"}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	})
	a, srv := newGithubTestAdapter(t, handler)
	defer srv.Close()

	states, err := a.FetchIssueStatesByIDs(context.Background(),
		[]string{"1", "99", "not-a-number"})
	require.NoError(t, err)

	require.Len(t, states, 1)
	require.Equal(t, "open", states["1"])
	require.NotContains(t, states, "99", "404 IDs must be omitted")
	require.NotContains(t, states, "not-a-number", "non-numeric IDs must be omitted")
}

func TestGithub_FetchIssueStatesByIDs_TransportErrorPropagates(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	a, srv := newGithubTestAdapter(t, handler)
	defer srv.Close()

	_, err := a.FetchIssueStatesByIDs(context.Background(), []string{"1"})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrResponseError))
}

func TestGithub_Unauthorized_WrapsResponseError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write(readFixture(t, "github_401.json"))
	})
	a, srv := newGithubTestAdapter(t, handler)
	defer srv.Close()

	_, err := a.FetchCandidateIssues(context.Background())
	require.True(t, errors.Is(err, ErrResponseError))
	require.Contains(t, err.Error(), "http 401")
	require.Contains(t, err.Error(), "Bad credentials")
}

func TestGithub_MalformedJSON_WrapsUnknownPayload(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not-valid-json"))
	})
	a, srv := newGithubTestAdapter(t, handler)
	defer srv.Close()

	_, err := a.FetchCandidateIssues(context.Background())
	require.True(t, errors.Is(err, ErrUnknownPayload))
}

func TestGithub_ContextCancelled_WrapsRequestFailed(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[]"))
	})
	a, srv := newGithubTestAdapter(t, handler)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := a.FetchCandidateIssues(ctx)
	require.True(t, errors.Is(err, ErrRequestFailed))
}

func TestGithub_ExecuteQuery_ProxiesRESTGet(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/repos/owner/repo/issues/1", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(readFixture(t, "github_issue_single.json"))
	})
	a, srv := newGithubTestAdapter(t, handler)
	defer srv.Close()

	out, err := a.ExecuteQuery(context.Background(), "/repos/owner/repo/issues/1", nil)
	require.NoError(t, err)
	require.Equal(t, float64(1), out["number"])
	require.Equal(t, "open", out["state"])
}

func TestGithub_ExecuteMutation_PostsBody(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/repos/owner/repo/issues/1/comments", r.URL.Path)
		// Body is the variables map verbatim.
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		require.Contains(t, string(buf[:n]), `"body":"hi"`)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":42,"body":"hi"}`))
	})
	a, srv := newGithubTestAdapter(t, handler)
	defer srv.Close()

	out, err := a.ExecuteMutation(context.Background(),
		"/repos/owner/repo/issues/1/comments",
		map[string]any{"body": "hi"})
	require.NoError(t, err)
	require.Equal(t, float64(42), out["id"])
}

func TestGithub_ParseLinkNext(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"only last", `<https://x/page=10>; rel="last"`, ""},
		{
			"next then last",
			`<https://x/page=2>; rel="next", <https://x/page=10>; rel="last"`,
			"https://x/page=2",
		},
		{
			"prev then next",
			`<https://x/page=1>; rel="prev", <https://x/page=3>; rel="next"`,
			"https://x/page=3",
		},
		{"unquoted rel", `<https://x/p=2>; rel=next`, "https://x/p=2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, parseLinkNext(tc.in))
		})
	}
}

func TestGithub_CheckRateLimit_LogsWarning(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf)

	a := &githubAdapter{log: logger}
	h := http.Header{}
	h.Set("X-RateLimit-Remaining", "10")
	h.Set("X-RateLimit-Reset", "1700000000")
	a.checkRateLimit(h)

	require.Contains(t, buf.String(), "rate_limit_low")
	require.Contains(t, buf.String(), `"remaining":10`)
}

func TestGithub_CheckRateLimit_SilentWhenHealthy(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf)

	a := &githubAdapter{log: logger}
	h := http.Header{}
	h.Set("X-RateLimit-Remaining", "4500")
	a.checkRateLimit(h)

	require.Empty(t, buf.String(),
		"healthy rate limit must not produce log output")
}
