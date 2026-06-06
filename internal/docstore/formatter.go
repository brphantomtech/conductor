package docstore

import (
	"strings"

	"github.com/conductor-sh/conductor/internal/knowledge"
)

// docContextHeader is the SPEC §10.5 block header for documentation injected
// into an agent prompt.
const docContextHeader = "## Relevant Documentation"

// FormatRelevantDocs renders doc-node search results into the SPEC §10.5
// "## Relevant Documentation" block for prompt injection. The block is
// truncated to at most budget bytes (whole entries are dropped to stay under
// budget; budget <= 0 means unbounded). An empty result set yields an empty
// string so callers can skip injection entirely.
func FormatRelevantDocs(results []knowledge.ScoredNode, budget int) string {
	if len(results) == 0 {
		return ""
	}
	header := docContextHeader + "\n\n"

	entries := make([]string, 0, len(results))
	for _, r := range results {
		if r.Node.Type != knowledge.NodeDoc {
			continue
		}
		entries = append(entries, formatDocEntry(r.Node))
	}
	if len(entries) == 0 {
		return ""
	}

	out := header
	for _, entry := range entries {
		candidate := out + entry
		if budget > 0 && len(candidate) > budget {
			break
		}
		out = candidate
	}
	if out == header {
		// Not even one entry fit: emit a single truncated entry so the block is
		// never empty when results exist.
		first := header + entries[0]
		if budget > 0 && len(first) > budget {
			return truncate(first, budget)
		}
		return first
	}
	return strings.TrimRight(out, "\n") + "\n"
}

// formatDocEntry renders one doc node as a SPEC §10.5 entry: a titled bullet
// with its source path and a short body excerpt.
func formatDocEntry(n knowledge.Node) string {
	title := n.Name
	if title == "" {
		title = n.Path
	}
	var b strings.Builder
	b.WriteString("### ")
	b.WriteString(title)
	b.WriteString("\n")
	b.WriteString("Source: ")
	b.WriteString(n.Path)
	b.WriteString("\n")
	if body := strings.TrimSpace(n.Content); body != "" {
		b.WriteString(body)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	return b.String()
}
