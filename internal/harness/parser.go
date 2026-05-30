package harness

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// frontMatterDelimiter is the SPEC §5.2 fence that opens and closes the
// optional YAML front-matter block. The fence must appear by itself on a
// line; trailing whitespace is tolerated, anything else is treated as
// regular Markdown.
const frontMatterDelimiter = "---"

// roleHeadingPrefix is the SPEC §5.2 marker for prompt-template sections.
// Only top-level (`## `) headings are recognized as role boundaries —
// `### foo` and deeper headings stay inside the surrounding section.
const roleHeadingPrefix = "## "

// ParseFile reads HARNESS.md from disk and returns a Definition. The
// returned Definition has its Source set to path; callers that load from
// other sources (memory, doc store) should use ParseBytes and populate
// Source themselves.
func ParseFile(path string) (*Definition, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is operator-supplied; reads are intentional.
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrMissingHarnessFile, path)
		}
		return nil, fmt.Errorf("harness: read %q: %w", path, err)
	}
	def, err := ParseBytes(data)
	if err != nil {
		return nil, err
	}
	def.Source = path
	return def, nil
}

// ParseBytes parses HARNESS.md content and returns a Definition. ParseBytes
// is the in-memory entry point used by tests and the dynamic-reload path
// where content arrives via fsnotify or HTTP.
//
// Errors classified per SPEC §23.1:
//
//   - ErrHarnessParse — front matter delimiter missing its closing fence
//     or YAML body cannot be decoded.
//   - ErrHarnessFrontMatterShape — YAML decoded but the root value is not
//     a mapping (e.g., a list or scalar).
func ParseBytes(data []byte) (*Definition, error) {
	frontMatter, body, err := splitFrontMatter(data)
	if err != nil {
		return nil, err
	}

	fm := map[string]any{}
	if len(frontMatter) > 0 {
		decoded, derr := decodeFrontMatter(frontMatter)
		if derr != nil {
			return nil, derr
		}
		fm = decoded
	}

	templates := extractRoleTemplates(body)
	return &Definition{
		FrontMatter:     fm,
		PromptTemplates: templates,
	}, nil
}

// splitFrontMatter separates the YAML front matter from the Markdown body.
// SPEC §5.2 specifies the file optionally starts with `---` on the first
// line; everything until the next `---` line is YAML. If the file does not
// open with the fence, the whole file is treated as body.
func splitFrontMatter(data []byte) (frontMatter, body []byte, err error) {
	br := bufio.NewReader(bytes.NewReader(data))

	first, ferr := peekFirstLine(br)
	if ferr != nil && !errors.Is(ferr, io.EOF) {
		return nil, nil, fmt.Errorf("harness: read first line: %w", ferr)
	}
	if !isFenceLine(first) {
		// No front matter: entire payload is body.
		return nil, data, nil
	}

	// Consume the opening fence line so the next ReadString starts the YAML
	// body proper. peekFirstLine left the reader at offset 0.
	if _, err := br.ReadString('\n'); err != nil && !errors.Is(err, io.EOF) {
		return nil, nil, fmt.Errorf("harness: consume opening fence: %w", err)
	}

	var yamlBuf bytes.Buffer
	closed := false
	for {
		line, rerr := br.ReadString('\n')
		if isFenceLine(line) {
			closed = true
			break
		}
		yamlBuf.WriteString(line)
		if errors.Is(rerr, io.EOF) {
			break
		}
		if rerr != nil {
			return nil, nil, fmt.Errorf("harness: read front matter: %w", rerr)
		}
	}
	if !closed {
		return nil, nil, fmt.Errorf("%w: front matter is not closed by `---`", ErrHarnessParse)
	}

	// The remainder of the reader is the Markdown body.
	rest, rerr := io.ReadAll(br)
	if rerr != nil {
		return nil, nil, fmt.Errorf("harness: read body: %w", rerr)
	}
	return yamlBuf.Bytes(), rest, nil
}

// peekFirstLine reads up to the first newline without advancing the
// reader's position. Implemented via Peek so the caller can decide whether
// to actually consume the line.
func peekFirstLine(br *bufio.Reader) (string, error) {
	const peekWindow = 256
	buf, err := br.Peek(peekWindow)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, bufio.ErrBufferFull) {
		return "", fmt.Errorf("harness: peek: %w", err)
	}
	if idx := bytes.IndexByte(buf, '\n'); idx >= 0 {
		return string(buf[:idx+1]), nil
	}
	return string(buf), nil
}

// isFenceLine reports whether s is a `---` line, optionally followed by
// whitespace and an end-of-line. SPEC §5.2 only requires the three
// hyphens; we tolerate trailing whitespace because Markdown editors
// frequently strip or add it.
func isFenceLine(s string) bool {
	trimmed := strings.TrimRight(s, " \t\r\n")
	return trimmed == frontMatterDelimiter
}

// decodeFrontMatter parses a YAML byte block into a generic map. Returns
// ErrHarnessParse on syntax errors and ErrHarnessFrontMatterShape when the
// root value is not a mapping (lists, scalars).
func decodeFrontMatter(yamlBytes []byte) (map[string]any, error) {
	var root any
	if err := yaml.Unmarshal(yamlBytes, &root); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrHarnessParse, err)
	}
	if root == nil {
		// An empty front matter block decodes to nil; treat it as an
		// empty mapping so downstream code can use range freely.
		return map[string]any{}, nil
	}
	mapped, ok := root.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: front matter root is %T, expected a YAML mapping",
			ErrHarnessFrontMatterShape, root)
	}
	return normalizeYAMLMap(mapped), nil
}

// normalizeYAMLMap walks a yaml.Unmarshal map[string]any tree and
// recursively converts any nested map[any]any nodes into map[string]any.
// gopkg.in/yaml.v3 already returns map[string]any at the top level; we
// keep this for defense in depth so downstream code only ever sees a
// uniform shape.
func normalizeYAMLMap(in map[string]any) map[string]any {
	for k, v := range in {
		in[k] = normalizeYAMLValue(v)
	}
	return in
}

func normalizeYAMLValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return normalizeYAMLMap(t)
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, vv := range t {
			out[fmt.Sprint(k)] = normalizeYAMLValue(vv)
		}
		return out
	case []any:
		for i := range t {
			t[i] = normalizeYAMLValue(t[i])
		}
		return t
	default:
		return v
	}
}

// extractRoleTemplates splits the body into role sections by `## <role>`
// headings. SPEC §5.2:
//
//   - Each `## <role>` line opens a new section; everything until the next
//     `## ` (or EOF) is the template body for that role.
//   - If no `## ` heading appears in the body, the entire body is assigned
//     to the `coder` role.
//   - Section bodies are trimmed of leading/trailing whitespace.
//
// Returns an empty (but non-nil) map when the body itself is empty so
// downstream code can range and lookup safely.
func extractRoleTemplates(body []byte) map[string]string {
	out := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	var (
		currentRole string
		buf         strings.Builder
		anyHeading  bool
	)

	flush := func() {
		if currentRole == "" {
			return
		}
		out[currentRole] = strings.TrimSpace(buf.String())
		buf.Reset()
	}

	for scanner.Scan() {
		line := scanner.Text()
		if role, ok := roleHeading(line); ok {
			flush()
			currentRole = role
			anyHeading = true
			continue
		}
		buf.WriteString(line)
		buf.WriteByte('\n')
	}
	flush()

	if !anyHeading {
		// SPEC §5.2 fallback: assign the whole body to the coder role
		// when the file contains no `## ` headings. We trim whitespace
		// but do not collapse internal blank lines.
		trimmed := strings.TrimSpace(string(body))
		if trimmed == "" {
			return out
		}
		out[DefaultRole] = trimmed
	}
	return out
}

// roleHeading reports whether line is a `## <role>` heading and returns
// the lowercased role name. The role name is taken verbatim until the
// first whitespace, so `## Coder Agent` extracts `coder`. We lowercase to
// match the role normalization SPEC §4.2 applies elsewhere.
func roleHeading(line string) (string, bool) {
	if !strings.HasPrefix(line, roleHeadingPrefix) {
		return "", false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(line, roleHeadingPrefix))
	if rest == "" {
		return "", false
	}
	// Take the first whitespace-delimited token as the role name. Any
	// trailing words (e.g., "## coder — Implementation Phase") are
	// descriptive and ignored by the parser.
	if i := strings.IndexAny(rest, " \t"); i >= 0 {
		rest = rest[:i]
	}
	return strings.ToLower(rest), true
}
