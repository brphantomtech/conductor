package tracker

import (
	"sort"
	"strings"
)

// maxIssuesPerCall caps the total number of issues FetchCandidateIssues
// and FetchIssuesByStates will return from a single invocation. Hitting
// the cap stops pagination and logs a warning; it is not an error.
// SPEC §4.2 is silent on a cap — 1000 is well above any realistic
// active-state count for one project (Symphony reports its largest
// Linear projects sit around 300 active issues) and surfaces a clear
// "your tracker is misconfigured" signal before exhausting memory.
const maxIssuesPerCall = 1000

// maxStateLookupBatch bounds the per-request batch size for
// FetchIssueStatesByIDs. Linear's GraphQL `id_in` filter accepts more
// than this, but 100 keeps any single GraphQL request well below
// Linear's 10 KB query-payload soft limit.
const maxStateLookupBatch = 100

// normalizeLabels applies the SPEC §4.2 label rules: lowercase,
// deduplicate, sort. Empty strings are dropped. The input is not
// mutated; the result is always a non-nil slice (empty when input
// has no usable labels) so consumers can range over it without a
// nil check.
func normalizeLabels(raw []string) []string {
	if len(raw) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, label := range raw {
		l := strings.ToLower(strings.TrimSpace(label))
		if l == "" {
			continue
		}
		if _, dup := seen[l]; dup {
			continue
		}
		seen[l] = struct{}{}
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}
