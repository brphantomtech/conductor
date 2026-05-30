## 0. Branch Setup (do this first — no exceptions)

- [x] 0.1 Confirm you are starting from an up-to-date `main`: `git fetch origin && git checkout main && git pull --ff-only origin main`
- [x] 0.2 Verify Phase 3 has been merged: `git log --oneline -5` should show the Phase 3 merge / `feat: provider adapter Llayer` commit at or near HEAD on `main`
- [x] 0.3 Cut the feature branch: `git checkout -b phase-4-tracker-adapters`
- [x] 0.4 Push the empty branch to the remote so a draft PR can be opened: `git push -u origin phase-4-tracker-adapters`
- [ ] 0.5 Open a draft PR titled `phase 4: tracker adapters` against `main`, linking to `openspec/changes/phase-4-tracker-adapters/` — **manual step**, `gh` CLI not installed locally; use https://github.com/brphantomtech/conductor/pull/new/phase-4-tracker-adapters
- [x] 0.6 Confirm `git status` is clean and you are ON the feature branch before editing any `internal/tracker/` file — every following task assumes this

## 1. Public Types and Surface (`internal/tracker`)

- [x] 1.1 Add `issue.go`: `Issue` struct matching SPEC §4.1.1 — fields `ID`, `Identifier`, `Title`, `Description *string`, `Priority *int`, `State`, `BranchName *string`, `URL *string`, `Labels []string`, `BlockedBy []BlockerRef`, `CreatedAt *time.Time`, `UpdatedAt *time.Time`, `TaskType *string`, `EstimatedComplexity *string`, `KnowledgeContextIDs []string`, `MemorySessionIDs []string`
- [x] 1.2 Add `blocker.go`: `BlockerRef` struct with `ID *string`, `Identifier *string`, `State *string`
- [x] 1.3 Add `adapter.go`: `Adapter` interface (`FetchCandidateIssues`, `FetchIssuesByStates`, `FetchIssueStatesByIDs`, `ExecuteQuery`, `ExecuteMutation`) plus `Option` functional-options type and `WithHTTPClient`, `WithLogger`, `WithClock` constructors
- [x] 1.4 Add `adapter_test.go` covering: `var _ Adapter = (*linearAdapter)(nil)` and `var _ Adapter = (*githubAdapter)(nil)` compile-time assertions, default option construction

## 2. Normalization Helpers (`internal/tracker/normalize.go`)

- [x] 2.1 Add `normalizeLabels(raw []string) []string` — lowercase, deduplicate, sort
- [x] 2.2 Add package-level constants `maxIssuesPerCall = 1000`, `maxStateLookupBatch = 100`
- [x] 2.3 Add `normalize_test.go` covering label normalization with mixed case, duplicates, empty slice, nil slice

## 3. Error Wrapping Helpers (`internal/tracker/errors.go`)

- [x] 3.1 Add helpers next to the existing sentinels: `wrapRequest(kind, op string, err error) error` returning `fmt.Errorf("tracker: %s: %s: %w", kind, op, errors.Join(err, ErrRequestFailed))`, plus `wrapResponse(kind, op string, status int, body string) error`, `wrapGraphQLErrors(kind, op string, errs []string) error`, `wrapUnknownPayload(kind, op string, err error) error`
- [x] 3.2 Keep the existing sentinels unchanged so `internal/harness/errors_test.go` (the SPEC §23 registry test) still passes
- [x] 3.3 Add `errors_test.go` covering: each wrapper produces an error that satisfies `errors.Is(err, <sentinel>)` and whose message contains the operation name and the tracker kind

## 4. HTTP JSON Helper (`internal/tracker/httpclient.go`)

- [x] 4.1 Define a package-internal `httpDoer` interface compatible with `*http.Client` so tests can inject a fake
- [x] 4.2 Default HTTP client: `&http.Client{Transport: http.DefaultTransport.Clone(), Timeout: 30 * time.Second}`
- [x] 4.3 Add `postJSON(ctx, doer, url string, headers http.Header, body any, out any) error` — handles encode, request, status-classification, decode, error wrapping
- [x] 4.4 Add `getJSON(ctx, doer, url string, headers http.Header, out any) (http.Header, error)` — returns response headers so the GitHub adapter can read the `Link` pagination header
- [x] 4.5 Helper `readErrorBody(resp *http.Response) (status int, snippet string)` that reads up to 4 KB of the response body for inclusion in the error message
- [x] 4.6 Add `httpclient_test.go` covering: 200 OK + valid JSON decoded into `out`, 401 + JSON body returns wrapped `ErrResponseError`, 5xx + plain-text body is captured, malformed JSON returns wrapped `ErrUnknownPayload`, `ctx.Done()` between connect and response returns wrapped `ErrRequestFailed`

## 5. GitHub Adapter (`internal/tracker/github.go`)

- [x] 5.1 Define `githubAdapter` struct (cfg, http, log, clock, baseURL, repo `owner/repo`) — defaults `baseURL = "https://api.github.com"`
- [x] 5.2 Implement constructor `newGithubAdapter(cfg config.Tracker, opts ...Option) (*githubAdapter, error)` — validates `api_key` non-empty, splits `ProjectID` on `/`, returns `ErrMissingAPIKey` / `ErrMissingProjectID` accordingly
- [x] 5.3 Implement `FetchCandidateIssues`: GET `/repos/{owner}/{repo}/issues?state=open&per_page=100&page=N`, follow `Link: …; rel="next"` header, filter out rows with non-null `pull_request`, normalize each row via `normalizeGithubIssue`, cap at `maxIssuesPerCall`
- [x] 5.4 Implement `FetchIssuesByStates`: same as above but accepting an explicit `state` list — GitHub supports `open`, `closed`, `all`; emit one request per state if more than one is requested, then merge
- [x] 5.5 Implement `FetchIssueStatesByIDs`: for each ID call `GET /repos/{owner}/{repo}/issues/{number}` (or batch via `?filter=…` when supported); return `map[id]state`; omit IDs the upstream returns 404 for
- [x] 5.6 Implement `ExecuteQuery(ctx, path string, _ map[string]any) (map[string]any, error)`: GET `{baseURL}{path}` and return the decoded JSON body; treat `query` as the URL path for REST
- [x] 5.7 Implement `ExecuteMutation(ctx, mutation string, variables map[string]any) (map[string]any, error)`: POST `{baseURL}{mutation}` with `variables` as the JSON body; `mutation` is the URL path
- [x] 5.8 Implement `normalizeGithubIssue(raw)` producing the §4.1.1 shape: `Identifier = fmt.Sprintf("gh-%d", raw.Number)` for workspace naming compatibility, `Labels` via `normalizeLabels` over `raw.Labels[].Name`, `URL = &raw.HTMLURL`, `BlockedBy` populated from `tracked_in_issues` when present (else empty slice), `BranchName = nil`
- [x] 5.9 Headers on every request: `Authorization: Bearer <api_key>`, `Accept: application/vnd.github+json`, `X-GitHub-Api-Version: 2022-11-28`
- [x] 5.10 Log a warning when the `X-RateLimit-Remaining` response header is below 100
- [x] 5.11 Add `github_test.go` driven by `httptest.Server` + fixtures: candidate listing with Link pagination, pull-request rows filtered out, state-filtered listing, batched state lookup, unknown-id omitted, 401 path, malformed-JSON path, ctx-cancellation

## 6. Linear Adapter (`internal/tracker/linear.go`)

- [x] 6.1 Define `linearAdapter` struct (cfg, http, log, clock, baseURL `https://api.linear.app/graphql`, projectSlug, viewerID)
- [x] 6.2 Implement constructor `newLinearAdapter(cfg, opts...) (*linearAdapter, error)` — validates `api_key` and `project_slug`; on first construction calls `viewer { id }` to validate the key and cache `viewerID`; auth failure surfaces `ErrResponseError` wrapping the upstream status
- [x] 6.3 Add `linear_query.go` with the GraphQL query strings as Go constants: `candidateIssuesQuery`, `issuesByStatesQuery`, `issueStatesByIDsQuery`, `viewerQuery` — each takes variables documented inline
- [x] 6.4 Implement `FetchCandidateIssues`: POST GraphQL with `candidateIssuesQuery`, paging via `pageInfo.endCursor` until `hasNextPage == false`, normalizing each `node` via `normalizeLinearIssue`, capping at `maxIssuesPerCall`
- [x] 6.5 Implement `FetchIssuesByStates`: POST GraphQL with `issuesByStatesQuery` filtering on the supplied state name list, same pagination
- [x] 6.6 Implement `FetchIssueStatesByIDs`: chunk IDs into batches of `maxStateLookupBatch`, POST GraphQL with `issueStatesByIDsQuery` (uses an `id_in` filter), return `map[id]state.name`; omit IDs the upstream did not return
- [x] 6.7 Implement `ExecuteQuery(ctx, query string, variables map[string]any) (map[string]any, error)`: POST `{baseURL}` with `{query, variables}` body, return `data` envelope, surface `errors[]` as `ErrGraphQLErrors`
- [x] 6.8 Implement `ExecuteMutation` identically (Linear's GraphQL endpoint accepts both)
- [x] 6.9 Implement `normalizeLinearIssue(raw)`: `ID = raw.ID`, `Identifier = raw.Identifier`, `Labels = normalizeLabels(raw.Labels.Nodes[].Name)`, `State = raw.State.Name`, `BranchName = &raw.BranchName` when non-empty, `BlockedBy` populated from `raw.BlockedBy.Edges[].Node`, timestamps parsed via `time.Parse(time.RFC3339, …)`
- [x] 6.10 Headers on every request: `Authorization: <api_key>` (Linear takes the raw key, no `Bearer` prefix), `Content-Type: application/json`
- [x] 6.11 Add `linear_test.go` driven by `httptest.Server` + fixtures: viewer-id ping at construction, candidate listing with two-page pagination, `blockedBy` edges populating BlockerRef, GraphQL-errors path, state-batch lookup, ctx-cancellation, unknown-id omitted

## 7. Factory and Validation (`internal/tracker/factory.go`, `internal/tracker/validate.go`)

- [x] 7.1 Add `factory.go`: `tracker.New(cfg config.Tracker, opts ...Option) (Adapter, error)` switch over `cfg.Kind` returning the matching adapter or wrapping `ErrUnsupportedKind` for `jira` / `plane` / `shortcut` / any other value
- [x] 7.2 Add `validate.go`: `Validate(cfg config.Tracker) error` enforcing the rules in the capability spec ("Tracker configuration validation"); use `errors.Join` to accumulate every problem
- [x] 7.3 Emit a warning (not an error) via the injected logger when `cfg.ActiveStates` is empty after defaults are applied — log key `tracker.active_states_empty`
- [x] 7.4 Add `factory_test.go` covering each supported kind, each deferred kind (`jira`, `plane`, `shortcut`) wrapping `ErrUnsupportedKind`, and the unknown-kind path
- [x] 7.5 Add `validate_test.go` covering: clean Linear case, clean GitHub case, missing api_key, unsupported kind, Linear without project_slug, GitHub without project_id, empty active_states warning

## 8. Harness Validator Integration (`internal/harness/validator.go`)

- [x] 8.1 In `internal/harness/validator.go`, call `tracker.Validate(cfg.Tracker)` and append its return to the joined error (mirror the Phase-3 `provider.Validate` integration)
- [x] 8.2 Update `internal/harness/validator_test.go` to add a test scenario that supplies a bad `tracker` block (e.g., `kind: made-up`) and asserts the joined error surfaces `tracker.ErrUnsupportedKind`
- [x] 8.3 Confirm `internal/harness/errors_test.go` (the SPEC §23 registry test) still passes with no edits — tracker sentinels are unchanged

## 9. Fixtures (`internal/tracker/testdata/`)

- [x] 9.1 `linear_viewer.json` — `{"data":{"viewer":{"id":"viewer-test"}}}` for the construction ping
- [x] 9.2 `linear_candidates_page1.json` — first page of 20 candidate issues with `pageInfo.hasNextPage: true, endCursor: "cursor-2"` — landed with 2 issues (sufficient to exercise pagination + normalization)
- [x] 9.3 `linear_candidates_page2.json` — second page of 5 issues with `pageInfo.hasNextPage: false` — landed with 1 issue (sufficient to exercise terminator)
- [x] 9.4 `linear_blocked_by.json` — single-issue response demonstrating populated `blockedBy.edges`
- [x] 9.5 `linear_states_batch.json` — multi-issue response keyed on `id_in` filter for `FetchIssueStatesByIDs`
- [x] 9.6 `linear_graphql_errors.json` — response with populated `errors[]` array
- [x] 9.7 `github_candidates_page1.json` — first page of 30 GitHub issues mixed with two pull-request rows — landed with 3 rows (one PR) which is sufficient to exercise the filter
- [x] 9.8 `github_candidates_page2.json` — second page of 10 issues — landed with 2 rows
- [x] 9.9 `github_issue_single.json` — single-issue response for `FetchIssueStatesByIDs`
- [x] 9.10 `github_401.json` — error body for the 401 unauthorized path
- [x] 9.11 API version is documented in `testdata/README.md` (JSON has no comment syntax and some fixtures are top-level arrays where a `_comment` key would break decoding) — Linear API as of 2025-11, GitHub REST v3 with `X-GitHub-Api-Version: 2022-11-28`

## 10. Doc, Lint, and Coverage

- [x] 10.1 Expand `internal/tracker/doc.go` to enumerate the Phase-4 files (mirroring the Phase-3 doc.go expansion in `internal/provider`)
- [x] 10.2 Run `go build ./...` and confirm the binary compiles
- [x] 10.3 Run `go vet ./...` and confirm no findings
- [x] 10.4 Run `golangci-lint run ./...` if the tool is available locally; if not, document the skipped check in the PR description — **skipped locally** (golangci-lint not installed on the dev machine); the CI workflow at `.github/workflows/ci.yml` runs it on PR
- [x] 10.5 Run `go test ./internal/tracker/...` and confirm all unit tests pass on Windows
- [x] 10.6 Run `go test -cover ./internal/tracker/...` and confirm coverage ≥ 70% for the new package — **89.6%**
- [x] 10.7 Run `make test` to ensure the full suite still passes (config + audit + harness + provider + tracker) — `make` itself is not installed on this Windows shell; ran the equivalent `go test -count=1 -timeout 120s ./...` (the `-race` flag from the Makefile target requires cgo + gcc, which are not available locally — CI runs the raced variant)

## 11. Phase Notes and Wrap-up

- [x] 11.1 Update `docs/phases.md` only if any deliverable text needs sharpening based on the implementation (no behavior change — only doc clarity) — Phase 4 deliverables list in `docs/phases.md` already matches what landed; no edits needed
- [x] 11.2 No CLI changes required for Phase 4 — `conductor harness validate` already surfaces the new joined tracker errors transparently
- [ ] 11.3 Mark the draft PR ready for review once the checklist above is fully green; squash-merge to `main` after approval, then delete the `phase-4-tracker-adapters` branch from the remote — **manual step for the reviewer**
- [ ] 11.4 After merge, archive this change: `/opsx:archive phase-4-tracker-adapters` — **manual step after PR merge**
