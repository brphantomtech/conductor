## Context

`internal/tracker` currently contains only error sentinels (Phase 1) and a package doc. SPEC §20.1 specifies a five-method `TrackerAdapter` interface; SPEC §4.1.1 specifies the normalized `Issue` shape; SPEC §4.2 specifies the normalization rules (label lowercasing, state-comparison policy, workspace-key sanitization); SPEC §23.2 specifies the sentinel error set already in place. Phase 4's scope (per `docs/phases.md`) is everything in §20.1 + §4.1.1 + §4.2 for two adapter kinds — `linear` (GraphQL) and `github` (REST) — with `jira` / `plane` / `shortcut` deferred to follow-up phases.

Linear and GitHub were picked deliberately. Linear has Symphony parity (so anyone migrating from Symphony has a working path), supports a clean GraphQL schema with cursor pagination, and exposes `blockedBy` edges natively — the richest source of `BlockerRef` data Phase 6 will consume. GitHub Issues is the most common open-source tracker; its REST API is well-known and its Link-header pagination is a different enough wire-protocol shape that landing both adapters now exercises the abstraction properly.

Downstream consumers in Phases 6, 7, and 13 will hold a `tracker.Adapter` constructed by `tracker.New` keyed off `Tracker.Kind`. The orchestrator (Phase 6) calls `FetchCandidateIssues` on every poll tick and `FetchIssueStatesByIDs` during reconciliation Part B. The router (Phase 7) does not call the tracker directly. Phase 13's Conductor tools `conductor_tracker_query` / `conductor_tracker_mutate` wrap `ExecuteQuery` / `ExecuteMutation` for agent-facing access. Keeping the interface stable in Phase 4 lets all later phases build against a real seam.

Tracker APIs differ in three places the adapter layer must normalize: (1) auth header (`Authorization: <raw_key>` for Linear, `Authorization: Bearer <token>` for GitHub), (2) request shape (GraphQL POST with `{query, variables}` body for Linear, repo-scoped REST GETs with query-string filters for GitHub), and (3) pagination (cursor-based `pageInfo { endCursor, hasNextPage }` for Linear, Link-header `rel="next"` for GitHub). The adapter layer normalizes all of these into a single `Issue` stream + raw map-shaped responses for `ExecuteQuery` / `ExecuteMutation`.

## Goals / Non-Goals

**Goals:**
- Implement SPEC §20.1 verbatim as the `Adapter` interface in `internal/tracker`. Five methods: `FetchCandidateIssues`, `FetchIssuesByStates`, `FetchIssueStatesByIDs`, `ExecuteQuery`, `ExecuteMutation`.
- Implement two concrete adapters: `linear`, `github`. They share an `httpJSON` helper plus the `normalize` package-private helpers; everything else is per-adapter.
- Implement the `Issue` and `BlockerRef` shapes from SPEC §4.1.1 with the §4.2 normalization rules applied at the adapter boundary so consumers never see raw tracker output.
- Add `tracker.Validate(cfg config.Tracker) error` that the harness validator calls. Surface missing api_key, unsupported kind, kind-specific project-id absence as `ErrMissingAPIKey` / `ErrUnsupportedKind` / `ErrMissingProjectID`.
- Drive unit tests entirely from `httptest.Server` instances that replay recorded JSON fixtures from `testdata/`. CI never makes a live API call.
- Cut a dedicated feature branch `phase-4-tracker-adapters` from `main` after Phase 3 merge for all of the above. Implementation work MUST NOT land on `main` directly.

**Non-Goals:**
- Jira, Plane, Shortcut adapters. The factory returns `ErrUnsupportedKind` for them; one test pins that behavior so a future PR cannot land a stub without flipping the test green. Each lands with its own phase.
- The orchestrator poll loop, candidate selection, dispatch sort, retry/backoff, reconciliation — Phase 6 finishes that and is the first caller of `FetchCandidateIssues`.
- The built-in Conductor tools (`conductor_tracker_query`, `conductor_tracker_mutate`) — Phase 13. `ExecuteQuery` / `ExecuteMutation` are shaped to be wrapped by those tools without leaking adapter internals.
- Plugin loading for third-party adapters — Phase 18.
- Webhook ingestion (push instead of poll) — not in v0.1 scope per `docs/phases.md`.
- Audit-event emission from inside the adapter. Phase 4 returns errors and Issue records; Phase 6 translates dispatched issues into `IssueClaimed` / `IssueReleased` audit events at the orchestrator boundary.

## Decisions

### D1. Speak the wire format directly; no tracker SDKs

**Choice.** Implement Linear / GitHub as plain `net/http` clients with hand-rolled JSON request bodies. No `github.com/google/go-github/v68/github` or `github.com/sashabaranov/linear-go` imports.

**Why.** Tracker SDKs lag the upstream schema (Linear ships breaking GraphQL changes quarterly), balloon the dependency graph (go-github is ~140 files of generated code we only need 6 endpoints from), and surface their own retry / pagination quirks. The wire format we need is small — six GraphQL queries for Linear, five REST endpoints for GitHub — and stable enough that maintaining our own request structs is cheaper than tracking two SDK release trains. This mirrors the Phase 3 D1 decision for the provider adapters and keeps the cgo-free single-binary contract intact (SPEC §3).

**Alternatives considered.** (a) Pull in `go-github` for GitHub — adds ~3 MB to the binary, brings transitive deps for OAuth flows we do not use, and forces us to translate SDK errors into our sentinel set anyway. (b) Generate a typed Linear client from the GraphQL introspection schema — adds a build-time codegen step, and the six queries we need are short enough that hand-written is shorter than the generator config.

### D2. Adapter interface lives in `internal/tracker`, not at a consumer

**Choice.** The public `Adapter` interface is exported from `internal/tracker` directly, mirroring the Phase 3 decision for `provider.Adapter`.

**Why.** SPEC §20.1 lists `TrackerAdapter` as one of the five public extension points third parties can implement (`docs/conventions.md §3.1` carves out exactly this exception: "cross-cutting public extension points are defined in the implementer's home package because they are the contract third parties implement"). Phase 4 follows that exception. Phase 18 will add a plugin loader without changing the interface location.

### D3. Pagination is internal; consumers see a fully materialized `[]Issue`

**Choice.** `FetchCandidateIssues` and `FetchIssuesByStates` page internally until the upstream signals "no more pages" (Linear `pageInfo.hasNextPage == false`, GitHub no Link `rel="next"`), accumulate into a single slice, and return. A hard cap of 1000 issues per call protects against runaway accumulators; hitting the cap returns the partial slice plus a logged warning, not an error.

**Why.** SPEC §13.2 says the orchestrator's candidate selection picks the top-N by priority — it needs the full slice in memory to sort, so a streaming iterator buys nothing. The 1000-issue cap is well above any realistic active-state count for a single project (Symphony reports its largest Linear projects sit around 300 active issues) and surfaces a clear "your tracker is misconfigured" signal before we exhaust process memory.

**Alternatives considered.** A `iter.Seq2[Issue, error]` iterator (Go 1.23+) — useful for Phase 13's `conductor_tracker_query`, but the orchestrator's sort step requires `len(issues)` to size the heap. We can add an iterator later without breaking the slice-returning method.

### D4. Normalization happens at the adapter boundary, not the consumer

**Choice.** Each adapter calls a private `normalizeIssue(raw)` helper that produces the `Issue` shape required by SPEC §4.1.1 + §4.2: labels lowercased + deduplicated + sorted, state preserved verbatim (consumer lowercases for comparison per §4.2), `blocked_by` populated when available, `branch_name` filled from tracker-specific source. The consumer (Phase 6) sees a homogeneous `[]Issue` and does not know which tracker it came from.

**Why.** Without this, every consumer needs to know "Linear gives me `lowercase` labels but GitHub gives me `Mixed-Case`" — every consumer becomes a leak of the wire format. Putting normalization at the boundary is the entire reason the adapter pattern exists.

**Alternatives considered.** Return raw `map[string]any` and let the consumer normalize — defeats the abstraction; the SPEC explicitly says "normalized issue record" in §4.1.1.

### D5. `ExecuteQuery` / `ExecuteMutation` return `map[string]any`, not typed responses

**Choice.** Both methods return `(map[string]any, error)` per SPEC §20.1. The Linear adapter decodes the GraphQL `data` envelope and returns it; the GitHub adapter decodes the JSON body and returns it. GraphQL `errors[]` from Linear surfaces as `ErrGraphQLErrors`. HTTP non-2xx surfaces as `ErrResponseError`.

**Why.** SPEC §20.1 mandates the signature. Typed responses would also need a code-generation step (see D1's "Alternatives considered"). The Phase 13 tool dispatcher passes the raw map through to the agent as JSON-encoded tool-call output, so a typed intermediate would just be JSON-decoded and re-encoded.

### D6. `Issue.BlockerRef` degrades gracefully across trackers

**Choice.** `BlockerRef` has three optional fields (`ID`, `Identifier`, `State` — all `*string`). Linear populates all three from the `blockedBy` GraphQL edge. GitHub populates `Identifier` (e.g., `#42`) from the `tracked_in_issues` REST field when present; `ID` and `State` may be `nil` because the REST projection does not include them. Consumers that need `State` make a follow-up `FetchIssueStatesByIDs` call.

**Why.** SPEC §4.1.1 declares all three BlockerRef fields nullable. Forcing a synthetic `State == "unknown"` would be lying to callers; Phase 6's blocker-rule evaluation already checks for nil.

**Alternatives considered.** Always make a second round-trip to fill `State` — adds 1–N extra HTTP calls per poll tick for GitHub. The orchestrator's reconciliation tick is the right place to do that, not the adapter.

### D7. Adapter factory is a single `tracker.New` function with a switch

**Choice.** `func New(cfg config.Tracker, opts ...Option) (Adapter, error)` dispatches on `cfg.Kind`. Options carry the `*http.Client`, logger, and clock. Unknown kinds return `ErrUnsupportedKind`. `jira` / `plane` / `shortcut` return `ErrUnsupportedKind` explicitly (with a wrapped message naming the kind) until their phases land.

**Why.** Two concrete kinds is small enough that a registry / plugin layer is overkill. Phase 18 will introduce a plugin loader for third-party adapters; until then, the explicit switch is the most discoverable and is what the SPEC §23.2 `unsupported_tracker_kind` error already expects. Mirrors Phase 3 D7.

### D8. HTTP client, clock, and logger are injected via functional options

**Choice.** `tracker.New(cfg, WithHTTPClient(c), WithClock(now), WithLogger(log))`. Defaults: `http.DefaultClient` cloned with `Timeout: 30 * time.Second`, `time.Now`, and a `zerolog.Nop()` logger.

**Why.** Tests inject `httptest.Server.Client()` to point the adapter at the local fixture server, a deterministic clock for `last_synced_at`-style timestamps in mutation responses, and a log-capture buffer for assertions. Production callers (Phase 6 orchestrator) inject a tuned `http.Client` with connection pooling. Functional options are the project's established convention (Phase 1 audit Writer, Phase 3 provider adapters).

### D9. Timeouts: 30 s overall on every call

**Choice.** Unlike `internal/provider` where streaming required `Timeout: 0` + ctx-deadline, `internal/tracker` calls are unary request/response. Set `http.Client.Timeout = 30 * time.Second` directly and rely on the caller's `ctx` for cancellation under that ceiling.

**Why.** No streaming means no late-arriving body bytes to worry about. 30 s comfortably covers Linear's slowest project-wide queries at p99 (Symphony's observability dashboard reports p99 ≈ 4 s for project-scoped issue listings) while bounding a runaway query.

### D10. Pagination caps live in `normalize.go`, not per adapter

**Choice.** A package-level constant `maxIssuesPerCall = 1000` lives in `normalize.go` and is consulted by both adapters' pagination loops. A package-level `maxStateLookupBatch = 100` (Linear) / `maxStateLookupBatch = 100` (GitHub via `gh-issue-XX,gh-issue-YY` filtered list) bounds the `FetchIssueStatesByIDs` payload.

**Why.** Identical caps across adapters keep the consumer's behavior predictable. Adapter-private caps would let one tracker silently return more than another for the same call.

### D11. Tracker config validation is in `internal/tracker`, called from `internal/harness`

**Choice.** `tracker.Validate(cfg config.Tracker) error` lives in the tracker package and returns an `errors.Join` of every problem found. `internal/harness/validator.go` adds a one-line call so a bad tracker block in HARNESS.md fails startup. Mirrors Phase 3 D10.

**Why.** Putting validation in the package that owns the supported-kind list keeps the rules close to the code that enforces them. The harness validator already aggregates errors via `errors.Join`, so adding one more sub-error is mechanical.

### D12. Branch hint resolution is kind-specific

**Choice.** Linear: use the `branchName` field from the issue payload when present (Linear computes it server-side from the issue identifier and project workflow); fall back to `nil` when the project disables branch names. GitHub: there is no native branch hint, so `branch_name` is left `nil` and the `BranchTemplate` in `WorkspaceRepo` config (default `conductor/{{ issue.identifier | downcase }}`) takes over.

**Why.** SPEC §4.1.1 declares `branch_name` nullable. Linear's `branchName` field is the closest analog to Symphony's tracker-provided branch hint. Synthesizing a hint inside the adapter for GitHub would just duplicate the workspace-layer template logic.

### D13. Single feature branch for the whole phase

**Choice.** All implementation work for Phase 4 lands on a single feature branch named `phase-4-tracker-adapters`, cut from `main` after the Phase 3 merge commit. The branch is opened in PR form against `main` and merged after the full task checklist is green. Sub-PRs are not used.

**Why.** The Phase 3 archive (`openspec/changes/archive/2026-05-13-phase-3-provider-adapter-layer/design.md` §"Migration Plan") established this single-branch-per-phase pattern, and `docs/phases.md` describes each phase as "independently testable and shippable." Splitting Phase 4 into multiple PRs would land a half-wired adapter on `main` (e.g., Linear without normalization) — pointless churn. The single-branch model also keeps the merge-conflict surface with parallel work small: any other phase work happens on its own branch.

## Risks / Trade-offs

- **Risk: GraphQL schema drift breaks the Linear queries.** Linear ships breaking changes about once a quarter; a renamed field silently breaks pagination or normalization. → Mitigation: every Linear fixture carries a leading comment with the Linear API version it was recorded against; a Phase-9 / Phase-13 follow-up task will add an `integration`-tagged live-call test (build tag `integration`, not run in CI) to rotate fixtures.
- **Risk: GitHub rate limits (5 000 req/h authenticated) under aggressive polling.** A 30-second poll interval against a large repo with `FetchIssueStatesByIDs` batching could approach the limit. → Mitigation: log a warning when the `X-RateLimit-Remaining` header drops below 100; the orchestrator's reconciliation tick (Phase 6) is the right place to back off. Phase 4 only logs.
- **Risk: GitHub Issues API returns pull requests as issues.** The REST endpoint mixes both in the same list. → Mitigation: the adapter filters out rows where `pull_request != nil` before normalization. A test fixture exercises this.
- **Risk: Linear `blockedBy` returns archived issues that the active-state filter would otherwise exclude.** → Mitigation: `BlockerRef.State` carries the raw Linear state name; consumers compare after `strings.ToLower`. Phase 6's blocker rule will use this.
- **Risk: pagination loop bug eats infinite memory.** A buggy `endCursor` echo could loop forever. → Mitigation: the loop counter is bounded by `maxIssuesPerCall / pageSize` (max 20 iterations for `pageSize=50`); reaching the bound logs a warning and returns the partial slice. A test fixture exercises the bound.
- **Risk: empty `active_states` slice means "fetch everything."** SPEC §5.3.2 says the default is `["Todo", "In Progress"]`; the config defaults already populate it, but a user could clear it. → Mitigation: `tracker.Validate` returns a warning (not an error) when `active_states` is empty; the adapter treats empty as "no filter" so power users can opt out.
- **Risk: fixture-based tests drift from real tracker responses.** → Mitigation: each fixture has a comment naming the tracker's API version it was recorded against (Linear API as of 2025-11, GitHub REST v3 as of 2025-11), and a follow-up task rotates them alongside the integration tests.
- **Risk: `wrapcheck` will flag every `fmt.Errorf("tracker: …")` inside helper functions.** → Mitigation: helpers either return raw sentinels (allowed) or use `fmt.Errorf` (already in `wrapcheck` allowlist for sibling packages). No new lint exceptions needed.
- **Risk: depguard prevents `internal/tracker` from importing other Tier-1 siblings.** → Mitigation: by design we don't need them. Tracker talks to `internal/config` (T0) and `internal/audit` (T1, cross-cutting allowed). The Linear adapter does not call into the workspace, provider, or harness packages — anything that needs cross-package coordination happens above us in Phase 6+.
- **Trade-off: hand-rolled HTTP vs SDKs adds maintenance burden.** Two integrations × two API surfaces = four wire formats we own. Mirrors the Phase 3 trade-off and follows the same logic; the alternative (SDK churn) is worse.

## Migration Plan

- No data migration. New package surface.
- **Branch: `phase-4-tracker-adapters` cut from `main` after Phase 3 merge.** Implementation work MUST start with the branch-creation step in [tasks.md](tasks.md) §0 — do not edit any files in `internal/tracker/` on `main`. All commits land on the feature branch; the merge to `main` happens after the full task checklist is green.
- Land order inside the phase: errors-helpers → Issue/BlockerRef types + normalization → httpJSON helper → GitHub adapter (simpler wire format, more reusable) → Linear adapter → factory → validator → harness wiring → fixture tests → docs.
- Rollback: revert the merge commit. Nothing outside the package depends on it yet (Phases 6+ have not landed); the harness validator gains a sub-error that disappears on revert.

## Open Questions

1. Should we ship a `tracker.Mock` exported helper for downstream packages to use in their own tests? Tentative answer: no — matching Phase 3 Open Question 1. Each consumer (Phase 6 orchestrator, Phase 13 tools) will need different fake behavior; a one-size-fits-all mock encourages over-coupling. Hand-rolled fakes per `docs/conventions.md §6`.
2. Should the Linear adapter prefetch the `viewer { id }` once at construction and cache it? Tentative answer: yes — it doubles as auth validation (a bad api_key surfaces `ErrRequestFailed` at startup instead of on the first poll). Cost is one HTTP round-trip during `tracker.New`; cached value is not used elsewhere yet but is read-cheap.
3. Should `FetchIssueStatesByIDs` retry on `tracker_request_failed`? Tentative answer: no, matching Phase 3 Open Question 2. The orchestrator (Phase 6) owns retry/backoff per SPEC §13.4; doubling it here would conflict with the run-attempt retry policy.
4. Should `Issue.UpdatedAt` come from `updatedAt` or `lastSyncedAt`? Tentative answer: `updatedAt` (the tracker's view of last issue change), so the orchestrator's stale-issue detection compares apples to apples across trackers.
