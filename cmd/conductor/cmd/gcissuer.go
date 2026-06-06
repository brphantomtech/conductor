package cmd

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/conductor-sh/conductor/internal/config"
	"github.com/conductor-sh/conductor/internal/harness"
	"github.com/conductor-sh/conductor/internal/tracker"
)

// gcRuleMarkerPrefix mirrors the hidden body marker the harness GC writer embeds
// in each created issue (harness/gc.go gcRuleMarker) so OpenGCRuleIDs can dedup
// by rule id against existing open GC issues.
const gcRuleMarkerPrefix = "conductor-gc-rule-id:"

// githubGCIssuer implements harness.TrackerIssuer over the GitHub tracker
// adapter's ExecuteQuery/ExecuteMutation (REST). It closes the deferred Phase 12
// follow-up for GitHub; the same tracker-write surface backs the Phase 13
// conductor_tracker_mutate tool.
//
// Linear (GraphQL) is not yet implemented here — see newTrackerIssuer's
// TODO(phase-13). Wiring only a GitHub issuer means GC on a Linear tracker runs
// the rules but creates no issues (a no-op, exactly as before this phase),
// keeping the build green.
type githubGCIssuer struct {
	exec      tracker.Adapter
	ownerRepo string
}

// OpenGCRuleIDs queries open issues carrying the GC label and extracts the rule
// id embedded in each body marker, returning the set already covered so the GC
// worker skips creating duplicates.
func (g githubGCIssuer) OpenGCRuleIDs(ctx context.Context, label string) (map[string]struct{}, error) {
	q := fmt.Sprintf("repo:%s state:open", g.ownerRepo)
	if label != "" {
		q += fmt.Sprintf(" label:%q", label)
	}
	path := "/search/issues?q=" + url.QueryEscape(q) + "&per_page=100"

	out, err := g.exec.ExecuteQuery(ctx, path, nil)
	if err != nil {
		return nil, fmt.Errorf("cmd: github gc issuer: search open issues: %w", err)
	}
	ids := map[string]struct{}{}
	items, _ := out["items"].([]any)
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		body, _ := m["body"].(string)
		if ruleID := extractRuleID(body); ruleID != "" {
			ids[ruleID] = struct{}{}
		}
	}
	return ids, nil
}

// CreateGCIssue creates one GC issue via the GitHub issues REST endpoint and
// returns its number as the identifier.
func (g githubGCIssuer) CreateGCIssue(ctx context.Context, issue harness.GCIssue) (string, error) {
	path := fmt.Sprintf("/repos/%s/issues", g.ownerRepo)
	body := map[string]any{
		"title": issue.Title,
		"body":  issue.Description,
	}
	if issue.Label != "" {
		body["labels"] = []string{issue.Label}
	}
	out, err := g.exec.ExecuteMutation(ctx, path, body)
	if err != nil {
		return "", fmt.Errorf("cmd: github gc issuer: create issue: %w", err)
	}
	if num, ok := out["number"].(float64); ok {
		return fmt.Sprintf("gh-%d", int(num)), nil
	}
	if u, ok := out["html_url"].(string); ok {
		return u, nil
	}
	return "", nil
}

// extractRuleID pulls the rule id out of a GC issue body's hidden marker.
func extractRuleID(body string) string {
	idx := strings.Index(body, gcRuleMarkerPrefix)
	if idx < 0 {
		return ""
	}
	rest := body[idx+len(gcRuleMarkerPrefix):]
	// The harness GC writer formats the marker as "<!-- <prefix><rule-id> -->"
	// (gc.go), so the id runs to the next whitespace. Rule ids may contain
	// hyphens, so only whitespace terminates.
	for i, r := range rest {
		if r == ' ' || r == '\n' || r == '\t' || r == '\r' {
			return strings.TrimSpace(rest[:i])
		}
	}
	return strings.TrimSpace(rest)
}

// newTrackerIssuer returns a harness.TrackerIssuer for the configured tracker
// kind, or nil when no issuer is implemented for that kind. GitHub is fully
// implemented; Linear is left as a TODO so GC stays a no-op there rather than
// erroring.
func newTrackerIssuer(cfg config.Config, adapter tracker.Adapter) harness.TrackerIssuer {
	switch cfg.Tracker.Kind {
	case tracker.KindGitHub:
		return githubGCIssuer{exec: adapter, ownerRepo: cfg.Tracker.ProjectID}
	default:
		// TODO(phase-13): implement the Linear (GraphQL) GC issuer over
		// ExecuteQuery/ExecuteMutation (issueSearch for dedup, issueCreate for
		// creation). Until then GC on a non-GitHub tracker runs rules but
		// creates no issues — a no-op, build green.
		return nil
	}
}
