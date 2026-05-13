# tracker-adapters Specification

## Purpose
TBD - created by archiving change phase-4-tracker-adapters. Update Purpose after archive.
## Requirements
### Requirement: Stable TrackerAdapter interface
The `internal/tracker` package SHALL expose an `Adapter` interface that every concrete tracker implementation conforms to. The interface SHALL contain exactly five methods matching SPEC §20.1: `FetchCandidateIssues(ctx) ([]Issue, error)`, `FetchIssuesByStates(ctx, states []string) ([]Issue, error)`, `FetchIssueStatesByIDs(ctx, ids []string) (map[string]string, error)`, `ExecuteQuery(ctx, query string, variables map[string]any) (map[string]any, error)`, and `ExecuteMutation(ctx, mutation string, variables map[string]any) (map[string]any, error)`. The interface MUST be importable from any Tier 3+ package and is the only surface the orchestrator and the Phase-13 tool dispatcher are permitted to call.

#### Scenario: Adapter interface satisfied by Linear implementation
- **WHEN** code declares `var _ tracker.Adapter = (*linearAdapter)(nil)`
- **THEN** the build succeeds

#### Scenario: Adapter interface satisfied by GitHub implementation
- **WHEN** code declares `var _ tracker.Adapter = (*githubAdapter)(nil)`
- **THEN** the build succeeds

### Requirement: Adapter construction by tracker kind
The `internal/tracker` package SHALL expose `New(cfg config.Tracker, opts ...Option) (Adapter, error)` that returns the adapter implementation whose name matches `cfg.Kind`. Supported tracker kinds in Phase 4 are `linear` and `github`. Any other value in the SPEC §5.3.2 supported list (`jira`, `plane`, `shortcut`) SHALL return an error satisfying `errors.Is(err, tracker.ErrUnsupportedKind)` until those kinds land in follow-up phases. Values outside the supported list SHALL also return `ErrUnsupportedKind`. The factory SHALL accept functional options for the HTTP client, the logger, and the clock function so tests can inject deterministic dependencies.

#### Scenario: Linear kind returns Linear adapter
- **WHEN** invoking `tracker.New(config.Tracker{Kind: "linear", APIKey: "lin_api_test", ProjectSlug: "test"})`
- **THEN** the returned adapter is non-nil and its `FetchCandidateIssues` posts to `https://api.linear.app/graphql` by default

#### Scenario: GitHub kind returns GitHub adapter
- **WHEN** invoking `tracker.New(config.Tracker{Kind: "github", APIKey: "ghp_test", ProjectID: "owner/repo"})`
- **THEN** the returned adapter is non-nil and its `FetchCandidateIssues` calls `https://api.github.com/repos/owner/repo/issues` by default

#### Scenario: Unsupported-but-recognized kind is rejected
- **WHEN** invoking `tracker.New(config.Tracker{Kind: "jira", APIKey: "k", ProjectID: "PROJ"})`
- **THEN** the returned error satisfies `errors.Is(err, tracker.ErrUnsupportedKind)`

#### Scenario: Unknown kind is rejected
- **WHEN** invoking `tracker.New(config.Tracker{Kind: "made-up", APIKey: "k"})`
- **THEN** the returned error satisfies `errors.Is(err, tracker.ErrUnsupportedKind)`

### Requirement: Tracker configuration validation
The `internal/tracker` package SHALL expose `Validate(cfg config.Tracker) error` that returns `errors.Join` over every problem in the tracker block. Validation SHALL enforce: (1) `kind` is in the SPEC §5.3.2 supported list (`linear`, `github`, `jira`, `plane`, `shortcut`); (2) `api_key` is non-empty after `$VAR` substitution; (3) `project_slug` is non-empty when `kind` is `linear` or `plane`; (4) `project_id` is non-empty when `kind` is `github`, `jira`, or `shortcut`. The `internal/harness` validator SHALL invoke `tracker.Validate` so a HARNESS.md with a broken tracker block fails startup.

#### Scenario: Valid Linear config passes
- **WHEN** validating `config.Tracker{Kind: "linear", APIKey: "lin_api_test", ProjectSlug: "test", ActiveStates: []string{"Todo"}, TerminalStates: []string{"Done"}}`
- **THEN** the returned error is nil

#### Scenario: Valid GitHub config passes
- **WHEN** validating `config.Tracker{Kind: "github", APIKey: "ghp_test", ProjectID: "owner/repo", ActiveStates: []string{"open"}, TerminalStates: []string{"closed"}}`
- **THEN** the returned error is nil

#### Scenario: Missing api_key is reported
- **WHEN** validating `config.Tracker{Kind: "linear", APIKey: "", ProjectSlug: "test"}`
- **THEN** the returned error satisfies `errors.Is(err, tracker.ErrMissingAPIKey)`

#### Scenario: Unsupported kind is reported
- **WHEN** validating `config.Tracker{Kind: "made-up-tracker", APIKey: "k"}`
- **THEN** the returned error satisfies `errors.Is(err, tracker.ErrUnsupportedKind)`

#### Scenario: Linear without project_slug is reported
- **WHEN** validating `config.Tracker{Kind: "linear", APIKey: "k", ProjectSlug: ""}`
- **THEN** the returned error satisfies `errors.Is(err, tracker.ErrMissingProjectID)`

#### Scenario: GitHub without project_id is reported
- **WHEN** validating `config.Tracker{Kind: "github", APIKey: "k", ProjectID: ""}`
- **THEN** the returned error satisfies `errors.Is(err, tracker.ErrMissingProjectID)`

### Requirement: Issue normalization conforms to SPEC §4.1.1 + §4.2
Every adapter SHALL return `Issue` records that conform to SPEC §4.1.1 with the §4.2 normalization rules applied: `labels` are lowercased, deduplicated, and sorted; `state` is preserved verbatim from the tracker (consumers compare after `strings.ToLower` per §4.2); `id` is the tracker's stable internal identifier; `identifier` is the human-readable key (e.g., `ABC-123` for Linear, `gh-issue-42` or `42` for GitHub depending on adapter convention); `blocked_by` is populated when the tracker exposes blocker relations and SHALL be an empty slice (not nil) when none exist; `task_type` and `estimated_complexity` are `nil` from the adapter (they are populated later by the router).

#### Scenario: Linear labels are lowercased and sorted
- **WHEN** the Linear adapter fetches an issue whose raw labels are `["Bug", "FRONTEND", "bug", "ui"]`
- **THEN** the returned `Issue.Labels == ["bug", "frontend", "ui"]`

#### Scenario: GitHub state is preserved verbatim
- **WHEN** the GitHub adapter fetches an issue whose REST `state` field is `"open"`
- **THEN** the returned `Issue.State == "open"` (not modified, not lowercased a second time)

#### Scenario: Linear blocked_by edge populates BlockerRef
- **WHEN** the Linear adapter fetches an issue whose `blockedBy` GraphQL edges include one entry with `id: "issue-x"`, `identifier: "ABC-99"`, `state.name: "In Review"`
- **THEN** the returned `Issue.BlockedBy[0] == BlockerRef{ID: &"issue-x", Identifier: &"ABC-99", State: &"In Review"}`

#### Scenario: GitHub adapter filters pull-request rows
- **WHEN** the GitHub adapter consumes a `/repos/{owner}/{repo}/issues` response that mixes issues and pull requests
- **THEN** the returned `[]Issue` contains only the rows whose `pull_request` field was absent or null

#### Scenario: Empty blocked_by is a non-nil empty slice
- **WHEN** an adapter fetches an issue with no blocker relations
- **THEN** the returned `Issue.BlockedBy` is `[]BlockerRef{}`, not `nil`

### Requirement: Pagination consumed internally with a hard cap
`FetchCandidateIssues` and `FetchIssuesByStates` SHALL page through the upstream API until the tracker signals no more pages (Linear `pageInfo.hasNextPage == false`, GitHub no `Link` header with `rel="next"`) or until a package-level hard cap of 1000 issues is reached. On reaching the cap the adapter SHALL log a warning naming the cap and return the partially accumulated slice without an error.

#### Scenario: Linear pagination follows endCursor
- **WHEN** the Linear adapter fetches candidate issues against a fixture that returns page 1 with `hasNextPage: true, endCursor: "c1"` (20 issues) and page 2 with `hasNextPage: false, endCursor: "c2"` (5 issues)
- **THEN** the returned slice contains exactly 25 issues, and the second request body included `"after":"c1"`

#### Scenario: GitHub pagination follows Link rel="next"
- **WHEN** the GitHub adapter fetches candidate issues against a fixture whose first response carries `Link: <…?page=2>; rel="next"` (30 issues) and whose second response omits Link (10 issues)
- **THEN** the returned slice contains exactly 40 issues

#### Scenario: Cap stops infinite pagination
- **WHEN** an adapter is pointed at a fixture that always reports another page available
- **THEN** the returned slice has length 1000 and the adapter emits a warning log entry naming `maxIssuesPerCall`

### Requirement: Error classification
Adapter operations SHALL surface failures using the SPEC §23.2 sentinel set. Transport-level failures (DNS, TCP, TLS, deadline-exceeded during request) SHALL wrap `tracker.ErrRequestFailed`. HTTP non-2xx responses with a structured body SHALL wrap `tracker.ErrResponseError`. GraphQL responses whose `errors[]` field is populated SHALL wrap `tracker.ErrGraphQLErrors`. JSON bodies that fail to decode into the expected shape SHALL wrap `tracker.ErrUnknownPayload`. Missing api_key at construction time SHALL surface `tracker.ErrMissingAPIKey`. Missing project_slug / project_id at construction time SHALL surface `tracker.ErrMissingProjectID`. Unsupported kinds SHALL surface `tracker.ErrUnsupportedKind`.

#### Scenario: 401 response surfaces ErrResponseError
- **WHEN** an adapter calls a server that returns HTTP 401 with body `{"error":"unauthorized"}`
- **THEN** the error returned satisfies `errors.Is(err, tracker.ErrResponseError)` and the error message includes the HTTP status code

#### Scenario: Linear GraphQL errors surface ErrGraphQLErrors
- **WHEN** the Linear adapter receives a 200 OK response whose body is `{"data":null,"errors":[{"message":"Argument 'projectId' has invalid value"}]}`
- **THEN** the returned error satisfies `errors.Is(err, tracker.ErrGraphQLErrors)` and the error message includes the GraphQL error text

#### Scenario: Malformed JSON surfaces ErrUnknownPayload
- **WHEN** the GitHub adapter receives a 200 OK response whose body is `not-valid-json`
- **THEN** the returned error satisfies `errors.Is(err, tracker.ErrUnknownPayload)`

#### Scenario: Context deadline surfaces ErrRequestFailed
- **WHEN** a caller passes a `context.Context` already cancelled with `context.DeadlineExceeded` to `FetchCandidateIssues`
- **THEN** the returned error satisfies `errors.Is(err, tracker.ErrRequestFailed)`

#### Scenario: Empty api_key fails construction
- **WHEN** `tracker.New(config.Tracker{Kind: "linear", APIKey: "", ProjectSlug: "p"})` is invoked
- **THEN** the returned error satisfies `errors.Is(err, tracker.ErrMissingAPIKey)`

### Requirement: ExecuteQuery and ExecuteMutation pass through raw payloads
`ExecuteQuery` and `ExecuteMutation` SHALL forward the supplied query / mutation and variables to the tracker and return the decoded response body as `map[string]any`. The Linear adapter SHALL return only the `data` envelope (not the wrapping `{data, errors}` object); GraphQL `errors[]` SHALL surface as `ErrGraphQLErrors`. The GitHub adapter SHALL return the decoded REST JSON body verbatim.

#### Scenario: Linear ExecuteQuery returns the data envelope
- **WHEN** the Linear adapter calls `ExecuteQuery(ctx, "query { viewer { id } }", nil)` against a fixture returning `{"data":{"viewer":{"id":"user_123"}}}`
- **THEN** the returned map equals `map[string]any{"viewer": map[string]any{"id": "user_123"}}`

#### Scenario: Linear ExecuteMutation propagates GraphQL errors
- **WHEN** the Linear adapter calls `ExecuteMutation(ctx, "mutation { …}", nil)` against a fixture returning a populated `errors[]` array
- **THEN** the returned error satisfies `errors.Is(err, tracker.ErrGraphQLErrors)` and the returned map is nil

#### Scenario: GitHub ExecuteQuery proxies REST GET responses
- **WHEN** the GitHub adapter calls `ExecuteQuery(ctx, "/repos/owner/repo/issues/1", nil)` against a fixture returning `{"number":1,"title":"hi"}`
- **THEN** the returned map equals `map[string]any{"number": float64(1), "title": "hi"}`

### Requirement: FetchIssueStatesByIDs batches lookups efficiently
`FetchIssueStatesByIDs` SHALL accept a list of tracker IDs and return a `map[string]string` keyed on the supplied IDs whose value is the current `state` for each issue. Adapters SHALL batch the lookup so a single call uses at most a small constant number of HTTP requests (≤ 1 per `maxStateLookupBatch` IDs). IDs that the tracker does not recognize SHALL be omitted from the returned map, not represented as an empty string.

#### Scenario: Linear batches state lookup with id-in filter
- **WHEN** the Linear adapter calls `FetchIssueStatesByIDs(ctx, []string{"id-1","id-2","id-3"})` against a fixture returning their three states
- **THEN** the returned map has length 3 with the correct state strings and only one HTTP request was made

#### Scenario: GitHub batches state lookup
- **WHEN** the GitHub adapter calls `FetchIssueStatesByIDs(ctx, []string{"1","2","3"})` and uses the issues-by-number endpoint (or filtered list)
- **THEN** the returned map has length 3 with the correct state strings

#### Scenario: Unknown ID is omitted
- **WHEN** the Linear adapter calls `FetchIssueStatesByIDs(ctx, []string{"id-1","does-not-exist"})` and the upstream returns only `id-1`
- **THEN** the returned map has length 1 and the key `does-not-exist` is absent

### Requirement: Tests use recorded fixtures only
All adapter unit tests SHALL exercise their HTTP code paths against an `httptest.Server` configured with payloads stored under `internal/tracker/testdata/`. The test suite SHALL NOT make outbound network calls and SHALL pass with no network connectivity. Integration tests against live tracker APIs MAY exist under the `//go:build integration` tag and SHALL NOT run in `make test`.

#### Scenario: Test run with network disabled passes
- **WHEN** the test runner runs `go test ./internal/tracker/...` in an environment with no outbound network access
- **THEN** all tests pass

#### Scenario: Integration tag is opt-in
- **WHEN** the test runner runs `make test`
- **THEN** files tagged `//go:build integration` are not compiled or executed
