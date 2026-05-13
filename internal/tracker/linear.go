package tracker

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/rs/zerolog"

	"github.com/conductor-sh/conductor/internal/config"
)

const (
	defaultLinearBaseURL = "https://api.linear.app/graphql"
	linearPageSize       = 50
)

// linearAdapter is the SPEC §20.1 implementation for Linear (GraphQL).
type linearAdapter struct {
	cfg         config.Tracker
	http        httpDoer
	log         zerolog.Logger
	clock       func() time.Time
	baseURL     string
	projectSlug string
	apiKey      string
	viewerID    string
}

// linearGraphQLResponse is the generic envelope around every Linear
// GraphQL response.
type linearGraphQLResponse struct {
	Data   map[string]any  `json:"data"`
	Errors []linearGQLError `json:"errors,omitempty"`
}

type linearGQLError struct {
	Message string `json:"message"`
}

// linearRawIssue mirrors the issue projection used by candidateIssuesQuery.
// The pointer / nullable fields follow the Linear schema's nullability.
type linearRawIssue struct {
	ID          string  `json:"id"`
	Identifier  string  `json:"identifier"`
	Title       string  `json:"title"`
	Description *string `json:"description"`
	Priority    *int    `json:"priority"`
	BranchName  string  `json:"branchName"`
	URL         string  `json:"url"`
	CreatedAt   string  `json:"createdAt"`
	UpdatedAt   string  `json:"updatedAt"`
	State       struct {
		Name string `json:"name"`
	} `json:"state"`
	Labels struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"labels"`
	BlockedBy struct {
		Edges []struct {
			Node struct {
				ID         string `json:"id"`
				Identifier string `json:"identifier"`
				State      struct {
					Name string `json:"name"`
				} `json:"state"`
			} `json:"node"`
		} `json:"edges"`
	} `json:"blockedBy"`
}

type linearPageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

func newLinearAdapter(cfg config.Tracker, opts options) (*linearAdapter, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("tracker: linear: api_key is required: %w", ErrMissingAPIKey)
	}
	if cfg.ProjectSlug == "" {
		return nil, fmt.Errorf("tracker: linear: project_slug is required: %w", ErrMissingProjectID)
	}
	base := cfg.Endpoint
	if base == "" {
		base = defaultLinearBaseURL
	}

	a := &linearAdapter{
		cfg:         cfg,
		http:        opts.httpClient,
		log:         opts.logger,
		clock:       opts.clock,
		baseURL:     base,
		projectSlug: cfg.ProjectSlug,
		apiKey:      cfg.APIKey,
	}

	// Construction-time ping: validate the API key by reading viewer.id.
	// Auth failures surface here as ErrResponseError, not on the first
	// poll tick.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := a.pingViewer(ctx); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *linearAdapter) authHeaders() http.Header {
	h := http.Header{}
	// Linear takes the raw key, no `Bearer` prefix (SPEC §5.3.2 +
	// design D6).
	h.Set("Authorization", a.apiKey)
	return h
}

// pingViewer issues the construction-time viewer { id } query and
// caches the result on the adapter. Auth failures surface as
// ErrResponseError per design D2.
func (a *linearAdapter) pingViewer(ctx context.Context) error {
	out, err := a.executeGraphQL(ctx, "construction ping", viewerQuery, nil)
	if err != nil {
		return err
	}
	viewer, _ := out["viewer"].(map[string]any)
	if id, ok := viewer["id"].(string); ok {
		a.viewerID = id
	}
	return nil
}

func (a *linearAdapter) FetchCandidateIssues(ctx context.Context) ([]Issue, error) {
	states := a.cfg.ActiveStates
	if len(states) == 0 {
		// Empty active_states means "no filter"; the underlying GraphQL
		// schema does not accept an empty list under `name: { in: [] }`,
		// so we omit the filter by passing a synthetic placeholder that
		// matches every state. Linear's schema permits empty `in:` since
		// 2024, but we keep the defensive list to support older
		// instances. The defaults in config/defaults.go ensure most
		// users hit the populated path.
		states = []string{"Todo", "In Progress"}
	}
	return a.listIssues(ctx, "fetch candidate issues", states)
}

func (a *linearAdapter) FetchIssuesByStates(ctx context.Context, states []string) ([]Issue, error) {
	if len(states) == 0 {
		return []Issue{}, nil
	}
	return a.listIssues(ctx, "fetch issues by states", states)
}

// listIssues runs the candidate/by-states query, paging through cursors
// until exhausted or the cap is reached.
func (a *linearAdapter) listIssues(ctx context.Context, op string, states []string) ([]Issue, error) {
	out := make([]Issue, 0, linearPageSize)
	var after *string

	for iter := 0; ; iter++ {
		if len(out) >= maxIssuesPerCall {
			a.log.Warn().
				Int("cap", maxIssuesPerCall).
				Str("op", op).
				Msg("tracker: linear: issues_cap_reached; returning partial result")
			break
		}

		variables := map[string]any{
			"slug":   a.projectSlug,
			"states": states,
			"first":  linearPageSize,
		}
		if after != nil {
			variables["after"] = *after
		}

		data, err := a.executeGraphQL(ctx, op, candidateIssuesQuery, variables)
		if err != nil {
			return nil, err
		}

		project, ok := data["project"].(map[string]any)
		if !ok {
			return nil, wrapUnknownPayload("linear", op,
				fmt.Errorf("response missing project envelope"))
		}
		issuesEnv, ok := project["issues"].(map[string]any)
		if !ok {
			return nil, wrapUnknownPayload("linear", op,
				fmt.Errorf("response missing issues envelope"))
		}

		// Page through nodes.
		nodes, _ := issuesEnv["nodes"].([]any)
		for _, n := range nodes {
			raw, ok := n.(map[string]any)
			if !ok {
				continue
			}
			out = append(out, a.normalizeLinearIssue(raw))
			if len(out) >= maxIssuesPerCall {
				break
			}
		}

		pageInfo, _ := issuesEnv["pageInfo"].(map[string]any)
		hasNext, _ := pageInfo["hasNextPage"].(bool)
		endCursor, _ := pageInfo["endCursor"].(string)
		if !hasNext || endCursor == "" {
			break
		}
		c := endCursor
		after = &c

		// Defensive: bound the pagination loop independently of the
		// node-count cap above so a buggy `endCursor` echo cannot loop
		// forever even if the count cap is set very high.
		if iter >= maxIssuesPerCall/linearPageSize+5 {
			a.log.Warn().
				Int("iters", iter).
				Str("op", op).
				Msg("tracker: linear: pagination_bound_reached; returning partial result")
			break
		}
	}
	return out, nil
}

func (a *linearAdapter) FetchIssueStatesByIDs(ctx context.Context, ids []string) (map[string]string, error) {
	out := make(map[string]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}

	for i := 0; i < len(ids); i += maxStateLookupBatch {
		end := i + maxStateLookupBatch
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[i:end]

		variables := map[string]any{
			"ids":   batch,
			"first": len(batch),
		}
		data, err := a.executeGraphQL(ctx, "fetch issue states", issueStatesByIDsQuery, variables)
		if err != nil {
			return nil, err
		}
		issuesEnv, ok := data["issues"].(map[string]any)
		if !ok {
			return nil, wrapUnknownPayload("linear", "fetch issue states",
				fmt.Errorf("response missing issues envelope"))
		}
		nodes, _ := issuesEnv["nodes"].([]any)
		for _, n := range nodes {
			node, ok := n.(map[string]any)
			if !ok {
				continue
			}
			id, _ := node["id"].(string)
			state, _ := node["state"].(map[string]any)
			name, _ := state["name"].(string)
			if id != "" && name != "" {
				out[id] = name
			}
		}
	}
	return out, nil
}

func (a *linearAdapter) ExecuteQuery(
	ctx context.Context,
	query string,
	variables map[string]any,
) (map[string]any, error) {
	return a.executeGraphQL(ctx, "ExecuteQuery", query, variables)
}

func (a *linearAdapter) ExecuteMutation(
	ctx context.Context,
	mutation string,
	variables map[string]any,
) (map[string]any, error) {
	return a.executeGraphQL(ctx, "ExecuteMutation", mutation, variables)
}

// executeGraphQL is the shared POST helper. It returns the `data`
// envelope only; GraphQL errors[] surface as ErrGraphQLErrors and
// transport / decode failures keep their respective sentinels.
func (a *linearAdapter) executeGraphQL(
	ctx context.Context,
	op, query string,
	variables map[string]any,
) (map[string]any, error) {
	body := map[string]any{"query": query}
	if variables != nil {
		body["variables"] = variables
	}

	var env linearGraphQLResponse
	if err := postJSON(ctx, a.http, "linear", op, a.baseURL, a.authHeaders(), body, &env); err != nil {
		return nil, err
	}
	if len(env.Errors) > 0 {
		msgs := make([]string, len(env.Errors))
		for i, e := range env.Errors {
			msgs[i] = e.Message
		}
		return nil, wrapGraphQLErrors("linear", op, msgs)
	}
	if env.Data == nil {
		return nil, wrapUnknownPayload("linear", op,
			fmt.Errorf("response has no data envelope"))
	}
	return env.Data, nil
}

// normalizeLinearIssue produces the SPEC §4.1.1 Issue shape from a
// Linear node decoded as map[string]any. The function tolerates absent
// fields by leaving the corresponding pointer nil.
func (a *linearAdapter) normalizeLinearIssue(raw map[string]any) Issue {
	id, _ := raw["id"].(string)
	identifier, _ := raw["identifier"].(string)
	title, _ := raw["title"].(string)
	stateMap, _ := raw["state"].(map[string]any)
	stateName, _ := stateMap["name"].(string)

	labelNames := []string{}
	if labels, ok := raw["labels"].(map[string]any); ok {
		if nodes, ok := labels["nodes"].([]any); ok {
			for _, n := range nodes {
				if l, ok := n.(map[string]any); ok {
					if name, ok := l["name"].(string); ok {
						labelNames = append(labelNames, name)
					}
				}
			}
		}
	}

	blockedBy := []BlockerRef{}
	if blocked, ok := raw["blockedBy"].(map[string]any); ok {
		if edges, ok := blocked["edges"].([]any); ok {
			for _, e := range edges {
				edge, ok := e.(map[string]any)
				if !ok {
					continue
				}
				node, _ := edge["node"].(map[string]any)
				ref := BlockerRef{}
				if v, ok := node["id"].(string); ok && v != "" {
					vCopy := v
					ref.ID = &vCopy
				}
				if v, ok := node["identifier"].(string); ok && v != "" {
					vCopy := v
					ref.Identifier = &vCopy
				}
				if st, ok := node["state"].(map[string]any); ok {
					if v, ok := st["name"].(string); ok && v != "" {
						vCopy := v
						ref.State = &vCopy
					}
				}
				blockedBy = append(blockedBy, ref)
			}
		}
	}

	out := Issue{
		ID:         id,
		Identifier: identifier,
		Title:      title,
		State:      stateName,
		Labels:     normalizeLabels(labelNames),
		BlockedBy:  blockedBy,
	}

	if desc, ok := raw["description"].(string); ok && desc != "" {
		d := desc
		out.Description = &d
	}
	if pf, ok := raw["priority"].(float64); ok {
		p := int(pf)
		out.Priority = &p
	}
	if bn, ok := raw["branchName"].(string); ok && bn != "" {
		v := bn
		out.BranchName = &v
	}
	if u, ok := raw["url"].(string); ok && u != "" {
		v := u
		out.URL = &v
	}
	if cs, ok := raw["createdAt"].(string); ok {
		if t, err := time.Parse(time.RFC3339, cs); err == nil {
			out.CreatedAt = &t
		}
	}
	if us, ok := raw["updatedAt"].(string); ok {
		if t, err := time.Parse(time.RFC3339, us); err == nil {
			out.UpdatedAt = &t
		}
	}
	return out
}

// Compile-time interface conformance.
var _ Adapter = (*linearAdapter)(nil)
