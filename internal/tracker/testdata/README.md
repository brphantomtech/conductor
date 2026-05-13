# Tracker adapter fixtures

These fixtures are the only data source for `internal/tracker` unit tests
(SPEC requirement — no live API in `make test`). Each was recorded against
a specific upstream API version; when the upstream schema drifts the
adapter, refresh both the fixture and its version reference here.

## Linear (GraphQL)

Recorded against the Linear GraphQL schema as of **2025-11**. The schema is
introspectable at `https://studio.apollographql.com/public/Linear-API/home`.

- `linear_viewer.json` — `viewer { id }` response for the construction-time
  auth ping.
- `linear_candidates_page1.json` — first page of a paginated
  `CandidateIssues` response (`hasNextPage: true, endCursor: "cursor-2"`).
- `linear_candidates_page2.json` — second / terminal page
  (`hasNextPage: false`).
- `linear_blocked_by.json` — single-issue response with a populated
  `blockedBy.edges` array, exercising `BlockerRef` propagation.
- `linear_states_batch.json` — `IssueStatesByIDs` response, three nodes
  for a four-id batched lookup (the fourth id is intentionally omitted to
  exercise the "unknown id is dropped" rule).
- `linear_graphql_errors.json` — populated `errors[]` array; `data` is
  null. Exercises `ErrGraphQLErrors`.

## GitHub (REST v3)

Recorded against GitHub REST v3 with `X-GitHub-Api-Version: 2022-11-28`.

- `github_candidates_page1.json` — first page of a paginated
  `/repos/{owner}/{repo}/issues` response, three rows including one
  pull-request entry (`pull_request` field set) that the adapter must
  filter out.
- `github_candidates_page2.json` — second / terminal page; the
  `tracked_in_issues` field on row 4 exercises `BlockerRef` propagation
  for GitHub.
- `github_issue_single.json` — single-issue response for
  `FetchIssueStatesByIDs`.
- `github_401.json` — `Bad credentials` error body served alongside HTTP
  401 to exercise the `ErrResponseError` path.
