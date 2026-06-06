package knowledge

import (
	"context"
	"fmt"
	"strings"
)

// contextHeader is the SPEC §8.5 block header.
const contextHeader = "## Codebase Context"

// FormatContext runs hybrid search with the issue title + description as the
// seed and formats the top-k results into the SPEC §8.5 "## Codebase Context"
// block. The block is truncated to at most budget bytes (the agent's
// context_budget); a budget <= 0 means unbounded. An empty result set yields an
// empty string so callers can skip injection entirely.
func (e *Engine) FormatContext(ctx context.Context, title, description string, budget int) (string, error) {
	seed := strings.TrimSpace(title + "\n" + description)
	results, err := e.Search(ctx, SearchParams{Query: seed, TopK: e.topK()})
	if err != nil {
		return "", err
	}
	return formatContextBlock(results, budget), nil
}

// formatContextBlock renders the results into the markdown block, truncating to
// budget bytes (whole entries are dropped to stay under budget; the header is
// always emitted when at least one entry fits).
func formatContextBlock(results []ScoredNode, budget int) string {
	if len(results) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(contextHeader)
	b.WriteString("\n\n### Relevant Files and Symbols\n\n")
	header := b.String()

	entries := make([]string, 0, len(results))
	for _, r := range results {
		entries = append(entries, formatEntry(r.Node))
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
		// Not even one entry fit the budget: emit a single truncated entry so
		// the block is never empty when results exist.
		first := header + entries[0]
		if budget > 0 && len(first) > budget {
			return truncate(first, budget)
		}
		return first
	}
	return strings.TrimRight(out, "\n") + "\n"
}

// formatEntry renders a single node as a SPEC §8.5 entry block.
func formatEntry(n Node) string {
	layer := n.LayerID
	if layer == "" {
		layer = UnknownLayer
	}
	var b strings.Builder
	fmt.Fprintf(&b, "**[%s] %s**", layer, n.Path)
	if n.Name != "" && n.Name != baseName(n.Path) {
		fmt.Fprintf(&b, " (%s)", n.Name)
	}
	b.WriteString("\n")
	if s := strings.TrimSpace(n.Summary); s != "" {
		b.WriteString(s)
		b.WriteString("\n")
	}
	if deps := dependencyLine(n); deps != "" {
		b.WriteString(deps)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	return b.String()
}

// dependencyLine renders the "Dependencies:" line from a node's outgoing import
// edges, naming the target paths.
func dependencyLine(n Node) string {
	if len(n.OutgoingEdges) == 0 {
		return ""
	}
	targets := make([]string, 0, len(n.OutgoingEdges))
	for _, e := range n.OutgoingEdges {
		if e.EdgeType == EdgeImports {
			targets = append(targets, baseName(e.ToID))
		}
	}
	if len(targets) == 0 {
		return ""
	}
	return "Dependencies: " + strings.Join(targets, ", ")
}
