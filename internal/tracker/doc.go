// Package tracker hosts issue tracker adapters behind the
// TrackerAdapter interface (SPEC §20.1). Supported kinds: linear,
// github, jira, plane, shortcut. Adapters are responsible for fetching
// candidate issues, querying state, and exposing read/write GraphQL /
// REST primitives that the Conductor-injected agent tools use to read
// and write tracker state.
//
// Phase 4 ships:
//
//   - adapter.go        Adapter interface + Option / WithHTTPClient /
//                       WithLogger / WithClock functional options.
//   - issue.go          SPEC §4.1.1 Issue struct.
//   - blocker.go        BlockerRef nested record.
//   - normalize.go      Label normalization + pagination caps.
//   - errors.go         SPEC §23.2 sentinels (unchanged from Phase 1)
//                       + wrapRequest / wrapResponse /
//                       wrapGraphQLErrors / wrapUnknownPayload helpers.
//   - httpclient.go     Shared JSON HTTP helper (postJSON / getJSON /
//                       readErrorBody) with status-code classification.
//   - linear.go         Linear GraphQL adapter (primary).
//   - linear_query.go   GraphQL query string constants used by linear.go.
//   - github.go         GitHub Issues REST adapter (secondary).
//   - factory.go        provider.New equivalent — kind → adapter dispatch.
//   - validate.go       SPEC §6.4 startup validator for the tracker block.
//
// Tier 1 (external adapters). Imports config (T0) and audit (T1,
// cross-cutting). Does not import any other Tier 1 sibling.
package tracker
