package tracker

// GraphQL queries for the Linear adapter. The queries are kept as Go
// constants so the Linear API version a query targets lives next to
// the query itself, and so fixture authors can search for the exact
// string they need to mock.
//
// Recorded against the Linear GraphQL schema as of 2025-11.

// viewerQuery is the construction-time ping. The response shape is
// `{"data":{"viewer":{"id":"<userID>"}}}`.
const viewerQuery = `query Viewer { viewer { id } }`

// candidateIssuesQuery lists issues in the configured active_states
// for the supplied project slug. Paginated via $after.
//
// Variables: `slug` (string), `states` ([]string), `first` (int),
// `after` (string nullable).
const candidateIssuesQuery = `
query CandidateIssues($slug: String!, $states: [String!]!, $first: Int!, $after: String) {
  project(id: $slug) {
    issues(first: $first, after: $after, filter: { state: { name: { in: $states } } }) {
      pageInfo { hasNextPage endCursor }
      nodes {
        id
        identifier
        title
        description
        priority
        branchName
        url
        createdAt
        updatedAt
        state { name }
        labels { nodes { name } }
        blockedBy {
          edges {
            node { id identifier state { name } }
          }
        }
      }
    }
  }
}`

// issuesByStatesQuery is structurally identical to candidateIssuesQuery
// but accepts an explicit state list; kept as a separate constant so
// fixtures can target one or the other unambiguously.
const issuesByStatesQuery = candidateIssuesQuery

// issueStatesByIDsQuery is the batched state-only lookup used by
// FetchIssueStatesByIDs. Variables: `ids` ([]string), `first` (int).
const issueStatesByIDsQuery = `
query IssueStatesByIDs($ids: [ID!]!, $first: Int!) {
  issues(first: $first, filter: { id: { in: $ids } }) {
    nodes { id state { name } }
  }
}`
