package router

import (
	"regexp"
	"strings"

	"github.com/conductor-sh/conductor/internal/config"
	"github.com/conductor-sh/conductor/internal/tracker"
)

// SelectPipeline returns the agent-role pipeline for an issue (SPEC §12.3). It
// evaluates routing.rules in declared order; the first rule whose present
// conditions all match (a conjunction) wins. When no rule matches, it falls
// back to routing.pipeline.
func (r *Router) SelectPipeline(iss tracker.Issue) []string {
	routing := r.configFn().Routing
	for i := range routing.Rules {
		rule := routing.Rules[i]
		if matchRule(rule.When, iss) && len(rule.Pipeline) > 0 {
			return append([]string(nil), rule.Pipeline...)
		}
	}
	return append([]string(nil), routing.Pipeline...)
}

// matchRule reports whether every condition present on the match applies to the
// issue. Absent conditions (zero value) are ignored, so a rule with no
// conditions matches every issue.
func matchRule(m config.RoutingMatch, iss tracker.Issue) bool {
	if len(m.Labels) > 0 && !hasAllLabels(iss.Labels, m.Labels) {
		return false
	}
	if len(m.AnyLabel) > 0 && !hasAnyLabel(iss.Labels, m.AnyLabel) {
		return false
	}
	if m.TaskType != "" && !equalFold(deref(iss.TaskType), m.TaskType) {
		return false
	}
	if m.State != "" && !equalFold(iss.State, m.State) {
		return false
	}
	if m.Complexity != "" && !equalFold(deref(iss.EstimatedComplexity), m.Complexity) {
		return false
	}
	if m.TitleMatches != "" && !titleMatches(m.TitleMatches, iss.Title) {
		return false
	}
	return true
}

// hasAllLabels reports whether every want label is present on the issue
// (case-insensitive). Tracker labels are already lowercased by the adapter, but
// the rule label is folded too for robustness.
func hasAllLabels(have, want []string) bool {
	for _, w := range want {
		if !containsFold(have, w) {
			return false
		}
	}
	return true
}

// hasAnyLabel reports whether at least one want label is present on the issue.
func hasAnyLabel(have, want []string) bool {
	for _, w := range want {
		if containsFold(have, w) {
			return true
		}
	}
	return false
}

// titleMatches reports whether pattern matches the title. An invalid regex is
// surfaced at config validation, not at dispatch, so a non-compiling pattern
// here simply fails to match rather than panicking.
func titleMatches(pattern, title string) bool {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return false
	}
	return re.MatchString(title)
}

func containsFold(haystack []string, needle string) bool {
	for _, h := range haystack {
		if equalFold(h, needle) {
			return true
		}
	}
	return false
}

func equalFold(a, b string) bool { return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b)) }

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
