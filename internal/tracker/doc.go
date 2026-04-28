// Package tracker hosts issue tracker adapters behind the TrackerAdapter
// interface (SPEC §20.1). Supported kinds: linear, github, jira, plane,
// shortcut. Adapters are responsible for fetching candidate issues,
// querying state, and exposing read/write GraphQL/REST primitives that the
// Conductor-injected agent tools use to read and write tracker state.
//
// Tier 1 (external adapters). Imports config, db, audit. Does not import
// any other Tier 1 sibling.
package tracker
