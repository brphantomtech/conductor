package tracker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/conductor-sh/conductor/internal/config"
)

const (
	defaultGithubBaseURL    = "https://api.github.com"
	githubAcceptHeader      = "application/vnd.github+json"
	githubAPIVersion        = "2022-11-28"
	githubPerPage           = 100
	githubRateLimitWarnUnder = 100
)

// githubAdapter is the SPEC §20.1 implementation for GitHub Issues.
type githubAdapter struct {
	cfg       config.Tracker
	http      httpDoer
	log       zerolog.Logger
	clock     func() time.Time
	baseURL   string
	repoOwner string
	repoName  string
	apiKey    string
}

// githubRawIssue mirrors the subset of /repos/{owner}/{repo}/issues
// fields the adapter consumes. Pull-request rows carry a non-nil
// `pull_request` object; the adapter filters those out.
type githubRawIssue struct {
	ID          int64  `json:"id"`
	Number      int    `json:"number"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	State       string `json:"state"`
	HTMLURL     string `json:"html_url"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
	Labels      []struct {
		Name string `json:"name"`
	} `json:"labels"`
	PullRequest *json.RawMessage `json:"pull_request,omitempty"`
	// TrackedInIssues is the SPEC §4.1.1 blocker source for GitHub.
	// Field is GitHub's "tracked by" link surfaced in the REST projection
	// when the repo has issues-tracking enabled; absent otherwise.
	TrackedInIssues []struct {
		Number int    `json:"number"`
		URL    string `json:"url"`
	} `json:"tracked_in_issues,omitempty"`
}

func newGithubAdapter(cfg config.Tracker, opts options) (*githubAdapter, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("tracker: github: api_key is required: %w", ErrMissingAPIKey)
	}
	if cfg.ProjectID == "" {
		return nil, fmt.Errorf("tracker: github: project_id is required: %w", ErrMissingProjectID)
	}
	owner, name, ok := strings.Cut(cfg.ProjectID, "/")
	if !ok || owner == "" || name == "" {
		return nil, fmt.Errorf("tracker: github: project_id must be owner/repo, got %q: %w",
			cfg.ProjectID, ErrMissingProjectID)
	}
	base := cfg.Endpoint
	if base == "" {
		base = defaultGithubBaseURL
	}
	base = strings.TrimRight(base, "/")

	return &githubAdapter{
		cfg:       cfg,
		http:      opts.httpClient,
		log:       opts.logger,
		clock:     opts.clock,
		baseURL:   base,
		repoOwner: owner,
		repoName:  name,
		apiKey:    cfg.APIKey,
	}, nil
}

func (a *githubAdapter) authHeaders() http.Header {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+a.apiKey)
	h.Set("Accept", githubAcceptHeader)
	h.Set("X-GitHub-Api-Version", githubAPIVersion)
	return h
}

// FetchCandidateIssues lists open issues in the active_states set.
// GitHub's REST `state` parameter accepts only `open|closed|all`, so
// the SPEC `active_states` slice maps as:
//   - default `["Todo", "In Progress"]` → `state=open` (typical case).
//   - explicit list of state names → `state=open` plus client-side
//     filter on the textual state per label convention. GitHub does not
//     expose Linear-style workflow states for issues; the practical
//     interpretation is "open issues, optionally filtered by labels in
//     `extra.labels`". Phase 4 keeps it simple: always request `open`.
func (a *githubAdapter) FetchCandidateIssues(ctx context.Context) ([]Issue, error) {
	return a.listIssues(ctx, "open", "fetch candidate issues")
}

func (a *githubAdapter) FetchIssuesByStates(ctx context.Context, states []string) ([]Issue, error) {
	if len(states) == 0 {
		return []Issue{}, nil
	}
	want := map[string]struct{}{}
	for _, s := range states {
		want[strings.ToLower(s)] = struct{}{}
	}

	// GitHub's REST `state` parameter is open|closed|all. If the caller
	// supplied only `open` or only `closed`, request that exactly;
	// otherwise request `all` and filter client-side. This matches the
	// SPEC §13.5 reconciliation expectation: the orchestrator passes the
	// configured terminal_states slice ("Done", "Cancelled", "Closed"...)
	// which collapses to `state=closed` for GitHub.
	state := "all"
	switch {
	case len(want) == 1:
		if _, ok := want["open"]; ok {
			state = "open"
		} else if _, ok := want["closed"]; ok {
			state = "closed"
		} else if _, ok := want["all"]; ok {
			state = "all"
		}
	}

	all, err := a.listIssues(ctx, state, "fetch issues by states")
	if err != nil {
		return nil, err
	}
	if state == "all" {
		out := make([]Issue, 0, len(all))
		for _, iss := range all {
			if _, ok := want[strings.ToLower(iss.State)]; ok {
				out = append(out, iss)
			}
		}
		return out, nil
	}
	return all, nil
}

// listIssues is the shared pagination loop for FetchCandidateIssues /
// FetchIssuesByStates. It honors the Link header's rel="next" pointer
// until exhausted or until maxIssuesPerCall is reached.
func (a *githubAdapter) listIssues(ctx context.Context, state, op string) ([]Issue, error) {
	first := fmt.Sprintf("%s/repos/%s/%s/issues?state=%s&per_page=%d&page=1",
		a.baseURL, a.repoOwner, a.repoName, url.QueryEscape(state), githubPerPage)

	out := make([]Issue, 0, githubPerPage)
	next := first
	for next != "" {
		if len(out) >= maxIssuesPerCall {
			a.log.Warn().
				Int("cap", maxIssuesPerCall).
				Str("op", op).
				Msg("tracker: github: issues_cap_reached; returning partial result")
			break
		}
		var page []githubRawIssue
		headers, err := getJSON(ctx, a.http, "github", op, next, a.authHeaders(), &page)
		if err != nil {
			return nil, err
		}
		a.checkRateLimit(headers)
		for _, raw := range page {
			if raw.PullRequest != nil {
				continue
			}
			out = append(out, a.normalizeGithubIssue(raw))
			if len(out) >= maxIssuesPerCall {
				break
			}
		}
		next = parseLinkNext(headers.Get("Link"))
	}
	return out, nil
}

func (a *githubAdapter) FetchIssueStatesByIDs(ctx context.Context, ids []string) (map[string]string, error) {
	out := make(map[string]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}

	for _, id := range ids {
		num, err := strconv.Atoi(id)
		if err != nil {
			// IDs that aren't issue numbers cannot be resolved through
			// GitHub's per-number endpoint; treat them as unknown and omit.
			continue
		}
		u := fmt.Sprintf("%s/repos/%s/%s/issues/%d", a.baseURL, a.repoOwner, a.repoName, num)
		var raw githubRawIssue
		_, err = getJSON(ctx, a.http, "github", "fetch issue states", u, a.authHeaders(), &raw)
		if err != nil {
			// 404 surfaces through ErrResponseError. Per SPEC §20.1, unknown
			// IDs are omitted from the result, not surfaced as errors.
			if isResponseStatus(err, http.StatusNotFound) {
				continue
			}
			return nil, err
		}
		out[id] = raw.State
	}
	return out, nil
}

func (a *githubAdapter) ExecuteQuery(
	ctx context.Context,
	query string,
	_ map[string]any,
) (map[string]any, error) {
	u, err := a.joinPath(query)
	if err != nil {
		return nil, fmt.Errorf("tracker: github: ExecuteQuery: %w",
			joinErrs(err, ErrRequestFailed))
	}
	var out map[string]any
	if _, err := getJSON(ctx, a.http, "github", "ExecuteQuery", u, a.authHeaders(), &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (a *githubAdapter) ExecuteMutation(
	ctx context.Context,
	mutation string,
	variables map[string]any,
) (map[string]any, error) {
	u, err := a.joinPath(mutation)
	if err != nil {
		return nil, fmt.Errorf("tracker: github: ExecuteMutation: %w",
			joinErrs(err, ErrRequestFailed))
	}
	var out map[string]any
	if err := postJSON(ctx, a.http, "github", "ExecuteMutation",
		u, a.authHeaders(), variables, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// joinPath resolves a user-supplied path against the configured base
// URL. Absolute URLs are passed through after a host-prefix sanity
// check; relative paths are joined onto the base.
func (a *githubAdapter) joinPath(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("path is empty")
	}
	if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
		return p, nil
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return a.baseURL + p, nil
}

func (a *githubAdapter) normalizeGithubIssue(raw githubRawIssue) Issue {
	identifier := fmt.Sprintf("gh-%d", raw.Number)
	labelNames := make([]string, 0, len(raw.Labels))
	for _, l := range raw.Labels {
		labelNames = append(labelNames, l.Name)
	}

	blockedBy := make([]BlockerRef, 0, len(raw.TrackedInIssues))
	for _, t := range raw.TrackedInIssues {
		ident := fmt.Sprintf("gh-%d", t.Number)
		blockedBy = append(blockedBy, BlockerRef{
			Identifier: &ident,
		})
	}

	out := Issue{
		ID:         strconv.FormatInt(raw.ID, 10),
		Identifier: identifier,
		Title:      raw.Title,
		State:      raw.State,
		Labels:     normalizeLabels(labelNames),
		BlockedBy:  blockedBy,
		// BranchName intentionally nil — GitHub provides no equivalent
		// of Linear's `branchName`; the workspace layer's branch_template
		// takes over.
	}
	if raw.Body != "" {
		body := raw.Body
		out.Description = &body
	}
	if raw.HTMLURL != "" {
		u := raw.HTMLURL
		out.URL = &u
	}
	if t, err := time.Parse(time.RFC3339, raw.CreatedAt); err == nil {
		out.CreatedAt = &t
	}
	if t, err := time.Parse(time.RFC3339, raw.UpdatedAt); err == nil {
		out.UpdatedAt = &t
	}
	return out
}

// checkRateLimit emits a structured warning when the remaining quota
// drops below githubRateLimitWarnUnder. Phase 4 only logs; Phase 6's
// orchestrator will be the right place to back off.
func (a *githubAdapter) checkRateLimit(h http.Header) {
	v := h.Get("X-RateLimit-Remaining")
	if v == "" {
		return
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return
	}
	if n < githubRateLimitWarnUnder {
		a.log.Warn().
			Int("remaining", n).
			Str("reset", h.Get("X-RateLimit-Reset")).
			Msg("tracker: github: rate_limit_low")
	}
}

// parseLinkNext returns the URL whose rel="next" the GitHub Link
// header advertises, or "" when no further pages exist. The header
// format is:
//
//	<https://api.github.com/repositories/1/issues?page=2>; rel="next",
//	<https://api.github.com/repositories/1/issues?page=10>; rel="last"
func parseLinkNext(link string) string {
	if link == "" {
		return ""
	}
	for _, part := range strings.Split(link, ",") {
		segs := strings.Split(strings.TrimSpace(part), ";")
		if len(segs) < 2 {
			continue
		}
		urlSeg := strings.TrimSpace(segs[0])
		relSeg := strings.TrimSpace(segs[1])
		if relSeg != `rel="next"` && relSeg != "rel=next" {
			continue
		}
		urlSeg = strings.TrimPrefix(urlSeg, "<")
		urlSeg = strings.TrimSuffix(urlSeg, ">")
		return urlSeg
	}
	return ""
}

// isResponseStatus reports whether err is an ErrResponseError carrying
// the specified HTTP status code. Implemented by string-matching the
// "http NNN" marker that wrapResponse embeds; the integration is
// shielded behind this helper so callers don't string-match directly.
func isResponseStatus(err error, status int) bool {
	if err == nil {
		return false
	}
	marker := fmt.Sprintf("http %d", status)
	return strings.Contains(err.Error(), marker)
}

// Compile-time interface conformance.
var _ Adapter = (*githubAdapter)(nil)
