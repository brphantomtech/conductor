## Why

Phase 3 shipped a working `ProviderAdapter` layer, but `internal/tracker` still contains only error sentinels and a `doc.go` — Conductor cannot yet read a single issue from any external system. With no tracker source the orchestrator (Phase 6) has nothing to poll, the router (Phase 7) has no `Issue` to classify, and the Conductor tools `conductor_tracker_query` / `conductor_tracker_mutate` (Phase 13) have nothing to call. Phase 4 closes that gap by exposing a stable `TrackerAdapter` interface plus two concrete adapters — Linear (GraphQL, primary because of Symphony parity) and GitHub Issues (REST, secondary) — and the `Issue` / `BlockerRef` normalization from SPEC §4.1.1 + §4.2 so every downstream phase can build against a real seam.

## What Changes

- New `internal/tracker` public surface: `Adapter` interface (five methods from SPEC §20.1 — `FetchCandidateIssues`, `FetchIssuesByStates`, `FetchIssueStatesByIDs`, `ExecuteQuery`, `ExecuteMutation`), plus the `Issue`, `BlockerRef`, and `RawResponse` types. The interface is the contract; the existing sentinels in `errors.go` are reused as classifiers.
- Two concrete adapters share a small HTTP-driven core:
  - `linearAdapter` — Linear GraphQL endpoint at `https://api.linear.app/graphql`, `Authorization: <api_key>` header, `viewer { id }` ping during construction, candidate query keyed on `project.slug` + `active_states`. Cursor-based pagination on every list query.
  - `githubAdapter` — GitHub REST v3 at `https://api.github.com`, `Authorization: Bearer <token>` + `Accept: application/vnd.github+json`, repo-scoped issue queries (`GET /repos/{owner}/{repo}/issues?state=open&labels=…&page=…`), Link-header pagination. Pull-request rows are filtered out (`pull_request` field absent on real issues).
- A shared `httpJSON` helper handles JSON encoding, header injection, status-code classification, and body capture for error messages. Adapters wrap this for their own request/response shapes (GraphQL `{ data, errors }` envelope for Linear; bare REST JSON for GitHub).
- Normalization rules from SPEC §4.2 produce a single `Issue` shape regardless of tracker kind: labels lowercased and de-duplicated, state preserved verbatim (the orchestrator's `active_states` / `terminal_states` are compared after `strings.ToLower` per spec), `blocked_by` populated when the tracker exposes relations (Linear `blockedBy` edges, GitHub `tracked_in_issues` field — degrade gracefully when absent), `branch_name` taken from the Linear `branchName` field when present and from `gh-issue-{number}` for GitHub when absent.
- Adapter construction is centralized in `tracker.New(cfg)` which maps `Tracker.Kind` → adapter constructor and returns `ErrUnsupportedKind` for non-Phase-4 kinds (`jira`, `plane`, `shortcut` are stubbed to return `ErrUnsupportedKind` until follow-up phases land). `$VAR` substitution on `api_key` is already done upstream by `internal/config`; an empty string surfaces `ErrMissingAPIKey`. Missing `project_slug` (Linear) or `project_id` (GitHub) surfaces `ErrMissingProjectID`.
- Tracker config validation at startup: `tracker.Validate(cfg config.Tracker) error` enforces supported-kind list, non-empty `api_key`, kind-specific project identifier, and warns on missing `active_states` / `terminal_states` (the defaults from `internal/config/defaults.go` already supply them, so a clean HARNESS.md passes). Called from `harness.Validate` so a bad HARNESS.md fails fast.
- Unit tests use a `httptest.Server` to play back recorded JSON / GraphQL fixtures — no live API calls in CI. Fixtures live under `internal/tracker/testdata/` and cover: candidate listing with pagination, state-filtered listing, batched state lookup, error responses (401, 404, GraphQL `errors[]`), mutation echo, mid-stream cancellation via `ctx.Done()`.
- Implementation work happens on a dedicated feature branch `phase-4-tracker-adapters` cut from `main` after the Phase 3 merge — the tasks document opens with that instruction so no one accidentally starts on `main`.

## Capabilities

### New Capabilities

- `tracker-adapters`: the stable `TrackerAdapter` surface, the two concrete adapters (Linear, GitHub Issues), the `Issue` / `BlockerRef` types and SPEC §4.2 normalization, the shared HTTP JSON helper, startup tracker-config validation, and the `tracker.New` factory.

### Modified Capabilities

None. The `harness-loader` spec already references `tracker.kind` validation as part of its startup-validation requirement; Phase 4 satisfies that contract from inside `internal/tracker` without changing the `harness-loader` requirement text. The `provider-adapter-layer` spec (Phase 3) is untouched.

## Impact

- **Code (new files):** `internal/tracker/{adapter,issue,blocker,normalize,httpclient,factory,validate,linear,github,linear_query,github_query}.go` plus matching `_test.go` files, plus `internal/tracker/testdata/*.json` fixtures.
- **Code (modified):** `internal/harness/validator.go` to call `tracker.Validate` on the merged config; `internal/tracker/doc.go` to document the Phase-4 shape.
- **Audit events:** No new event types — Phase 4 does not emit audit events directly. The orchestrator will translate `Issue`-state changes into audit events in Phase 6 at the dispatch boundary.
- **Dependencies:** No new external Go modules. Standard-library `net/http` and `encoding/json` carry the load. Provider SDKs (linear-go, go-github) are intentionally NOT pulled in — keeping the wire format under our control mirrors the Phase 3 decision (D1) and shrinks the binary.
- **Backward compatibility:** New package surface; nothing in Phases 1–3 imports `internal/tracker` yet (other than its sentinels via `internal/harness/errors_test.go`), so there is nothing to break.
- **Branch / workflow:** All implementation work for this phase MUST happen on a new git branch named `phase-4-tracker-adapters` cut from `main` after Phase 3 merge — see [tasks.md](tasks.md) §0.
- **Out of scope (deferred):**
  - Jira, Plane, Shortcut adapters → follow-up phases (the factory returns `ErrUnsupportedKind` for them, validated by test).
  - The orchestrator poll loop that consumes `FetchCandidateIssues` → Phase 6.
  - Built-in Conductor tools (`conductor_tracker_query`, `conductor_tracker_mutate`) that expose the adapter to agents → Phase 13.
  - WebSocket streaming of issue changes to the dashboard → Phase 14.
  - Webhooks-based push updates from trackers (push instead of poll) → not in v0.1 scope.
