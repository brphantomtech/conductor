package harness

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Definition is the parsed representation of a HARNESS.md file. SPEC §5.2
// returns exactly these three fields: the front-matter map, the per-role
// prompt templates keyed by section heading, and the path the parser read
// from (preserved for log + audit context).
type Definition struct {
	// FrontMatter is the YAML front-matter map decoded with yaml.v3. An
	// absent front matter produces an empty (non-nil) map so callers can
	// safely index without nil-checks.
	FrontMatter map[string]any

	// PromptTemplates maps role name (the literal text after `## `) to
	// the trimmed body of that section. If the body had no `## <role>`
	// headings, SPEC §5.2 says the entire body lands under the "coder"
	// key, which Parse implements.
	PromptTemplates map[string]string

	// SourcePath is the on-disk path the Definition was loaded from. It
	// is empty for parseBytes calls that did not come through Parse.
	SourcePath string
}

// frontMatterDelim is the SPEC §5.2 front-matter delimiter line. Three
// dashes, no leading whitespace.
const frontMatterDelim = "---"

// DefaultRole is the role key SPEC §5.2 assigns to a body that has no
// `## <role>` headings — "the entire body is assigned to the coder
// role." Exported so callers / tests can refer to the constant rather
// than hardcoding the literal.
const DefaultRole = "coder"

// Parse reads HARNESS.md from disk and returns its parsed Definition.
// I/O failures wrap ErrHarnessParse so callers can match on the spec's
// error class without inspecting wrapped strings.
func Parse(path string) (*Definition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: read %s: %w", ErrHarnessParse, path, err)
	}
	def, err := parseBytes(data, path)
	if err != nil {
		return nil, err
	}
	return def, nil
}

// parseBytes is Parse's pure core. It accepts raw file bytes and returns
// the same Definition Parse would have. Used directly by tests that feed
// in-memory fixtures and by the watcher's reload path.
func parseBytes(data []byte, sourcePath string) (*Definition, error) {
	frontMatter, body, err := splitFrontMatter(data)
	if err != nil {
		return nil, err
	}

	templates, err := splitTemplates(body)
	if err != nil {
		return nil, err
	}

	return &Definition{
		FrontMatter:     frontMatter,
		PromptTemplates: templates,
		SourcePath:      sourcePath,
	}, nil
}

// splitFrontMatter looks for the SPEC §5.2 front-matter block: lines
// between a leading `---` and the next `---`, both on lines by
// themselves. Returns the decoded map, the remaining body bytes, and any
// error. A file with no leading `---` is treated as having empty front
// matter and the entire input as the body.
func splitFrontMatter(data []byte) (map[string]any, []byte, error) {
	// Tolerate a UTF-8 BOM that editors and templated HARNESS.md files
	// occasionally inject before the delimiter line.
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})

	lines := bytes.SplitN(data, []byte("\n"), 2)
	first := bytes.TrimRight(lines[0], "\r")
	if string(first) != frontMatterDelim {
		return map[string]any{}, data, nil
	}
	if len(lines) < 2 {
		// "---" with nothing after it: malformed.
		return nil, nil, fmt.Errorf("%w: front matter has opening delimiter but no body", ErrHarnessParse)
	}

	rest := lines[1]
	// Find the closing "---" on a line by itself.
	closeIdx, closeLen := findCloseDelim(rest)
	if closeIdx < 0 {
		return nil, nil, fmt.Errorf("%w: front matter has no closing %q", ErrHarnessParse, frontMatterDelim)
	}

	yamlBytes := rest[:closeIdx]
	bodyStart := closeIdx + closeLen
	body := []byte{}
	if bodyStart < len(rest) {
		body = rest[bodyStart:]
	}

	// Empty front matter ("---\n---\n") is legal and decodes to {}.
	var node yaml.Node
	if len(bytes.TrimSpace(yamlBytes)) == 0 {
		return map[string]any{}, body, nil
	}
	if err := yaml.Unmarshal(yamlBytes, &node); err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrHarnessParse, err)
	}

	root := unwrapDocument(&node)
	if root == nil {
		return map[string]any{}, body, nil
	}
	if root.Kind != yaml.MappingNode {
		return nil, nil, fmt.Errorf("%w: front matter root is %s, expected mapping", ErrHarnessFrontMatterShape, kindName(root.Kind))
	}

	out := map[string]any{}
	if err := root.Decode(&out); err != nil {
		return nil, nil, fmt.Errorf("%w: decode front matter map: %w", ErrHarnessParse, err)
	}
	return out, body, nil
}

// findCloseDelim returns the byte offset of the next line whose content
// (after CR stripping) equals "---" and the length of that line including
// its terminator, so the caller can skip past it. Returns -1 if not
// found.
func findCloseDelim(data []byte) (int, int) {
	start := 0
	for start < len(data) {
		end := bytes.IndexByte(data[start:], '\n')
		var line []byte
		var lineLen int
		if end < 0 {
			line = data[start:]
			lineLen = len(line)
		} else {
			line = data[start : start+end]
			lineLen = end + 1
		}
		if string(bytes.TrimRight(line, "\r")) == frontMatterDelim {
			return start, lineLen
		}
		start += lineLen
	}
	return -1, 0
}

// splitTemplates implements SPEC §5.2's body-section rule: split by
// top-level `## <role>` headings; each section's trimmed body becomes the
// value under PromptTemplates[role]. If no `##` heading is present, the
// entire trimmed body becomes the "coder" template.
func splitTemplates(body []byte) (map[string]string, error) {
	out := map[string]string{}
	if len(bytes.TrimSpace(body)) == 0 {
		return out, nil
	}

	text := string(body)
	lines := strings.Split(text, "\n")

	currentRole := ""
	var currentLines []string
	flush := func() {
		if currentRole == "" {
			return
		}
		out[currentRole] = strings.TrimSpace(strings.Join(currentLines, "\n"))
	}

	sawHeading := false
	var preludeLines []string
	for _, raw := range lines {
		stripped := strings.TrimRight(raw, "\r")
		if role, ok := parseRoleHeading(stripped); ok {
			if !sawHeading {
				sawHeading = true
			}
			flush()
			currentRole = role
			currentLines = currentLines[:0]
			continue
		}
		if !sawHeading {
			preludeLines = append(preludeLines, stripped)
			continue
		}
		currentLines = append(currentLines, stripped)
	}
	flush()

	if !sawHeading {
		// SPEC §5.2: "If no ## sections are present, the entire body is
		// assigned to the coder role."
		trimmed := strings.TrimSpace(strings.Join(preludeLines, "\n"))
		if trimmed != "" {
			out[DefaultRole] = trimmed
		}
	}

	return out, nil
}

// parseRoleHeading recognizes a top-level `## <role>` heading. It is
// intentionally narrow: it must start with exactly two `#` characters
// followed by a single space and a non-empty role name, mirroring SPEC
// §5.2 which only carves on the level-2 heading.
func parseRoleHeading(line string) (string, bool) {
	if !strings.HasPrefix(line, "## ") {
		return "", false
	}
	// Reject level-3+ headings ("### " has prefix "## " — block them).
	if strings.HasPrefix(line, "### ") {
		return "", false
	}
	name := strings.TrimSpace(line[3:])
	if name == "" {
		return "", false
	}
	// Extract just the first token so "## planner (notes)" still maps to
	// "planner". The trailing chunk is treated as a comment.
	if idx := strings.IndexAny(name, " \t"); idx > 0 {
		name = name[:idx]
	}
	return name, true
}

// unwrapDocument peels a top-level yaml.DocumentNode down to its single
// content child, returning nil if the document is empty.
func unwrapDocument(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.DocumentNode {
		if len(n.Content) == 0 {
			return nil
		}
		return n.Content[0]
	}
	return n
}

func kindName(k yaml.Kind) string {
	switch k {
	case yaml.DocumentNode:
		return "document"
	case yaml.MappingNode:
		return "mapping"
	case yaml.SequenceNode:
		return "sequence"
	case yaml.ScalarNode:
		return "scalar"
	case yaml.AliasNode:
		return "alias"
	}
	return "unknown"
}
