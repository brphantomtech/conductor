package memory

import (
	"context"
	"regexp"
	"strings"
)

// memoryBlockRe matches a `<memory type="...">...</memory>` block. The body
// is captured non-greedily so multiple blocks in one output are split.
var memoryBlockRe = regexp.MustCompile(`(?is)<memory\s+type="([a-z]+)"\s*>(.*?)</memory>`)

// Extracted is one parsed memory candidate from agent output. It carries
// only the layer and content; scope and source are applied by the manager
// when the candidate is written.
type Extracted struct {
	Layer   Layer
	Content string
}

// Extract is the pure auto-extractor (SPEC §9.4). It parses agent output for
// two patterns:
//
//   - lines beginning with `MEMORY:` → semantic memory, and
//   - `<memory type="...">…</memory>` blocks → the named layer.
//
// A block whose type is not a known layer is downgraded to episodic, matching
// the SPEC rule that validation failures (and otherwise un-typed memories)
// land in episodic. Extract performs no I/O and is trivially unit-testable.
func Extract(output string) []Extracted {
	var out []Extracted

	for _, mtch := range memoryBlockRe.FindAllStringSubmatch(output, -1) {
		layer := Layer(strings.ToLower(strings.TrimSpace(mtch[1])))
		content := strings.TrimSpace(mtch[2])
		if content == "" {
			continue
		}
		if !layer.Valid() {
			// Unknown type → validation/parse failure → episodic (SPEC §9.4).
			layer = LayerEpisodic
		}
		out = append(out, Extracted{Layer: layer, Content: content})
	}

	// Strip the XML blocks before scanning for MEMORY: lines so a MEMORY:
	// line inside a block is not double-counted.
	stripped := memoryBlockRe.ReplaceAllString(output, "")
	for _, line := range strings.Split(stripped, "\n") {
		trimmed := strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(trimmed, "MEMORY:"); ok {
			content := strings.TrimSpace(rest)
			if content != "" {
				out = append(out, Extracted{Layer: LayerSemantic, Content: content})
			}
		}
	}
	return out
}

// IngestAgentOutput runs the auto-extractor over one turn's output and writes
// every extracted candidate with source=auto_extracted (SPEC §9.4 path 2).
// Episodic candidates are scoped to issueID; procedural candidates to
// taskType. It returns the entries written. A disabled manager is a no-op.
func (m *Manager) IngestAgentOutput(
	ctx context.Context, projectID, issueID, taskType, output string,
) ([]MemoryEntry, error) {
	if !m.Enabled() {
		return nil, nil
	}
	var written []MemoryEntry
	for _, ex := range Extract(output) {
		in := WriteInput{
			Layer:     ex.Layer,
			ProjectID: projectID,
			Content:   ex.Content,
			Source:    SourceAutoExtracted,
		}
		switch ex.Layer {
		case LayerEpisodic:
			in.IssueID = issueID
		case LayerProcedural:
			in.TaskType = taskType
		}
		e, err := m.Write(ctx, in)
		if err != nil {
			return written, err
		}
		if e != nil {
			written = append(written, *e)
		}
	}
	return written, nil
}

// RecordValidationFailure writes a validation failure as an issue-scoped
// episodic memory with source=validation_result (SPEC §9.4). checkID and
// detail are folded into the content. A disabled manager is a no-op.
func (m *Manager) RecordValidationFailure(
	ctx context.Context, projectID, issueID, checkID, detail string,
) (*MemoryEntry, error) {
	if !m.Enabled() {
		return nil, nil
	}
	content := detail
	if checkID != "" {
		content = "validation check " + checkID + " failed: " + detail
	}
	return m.Write(ctx, WriteInput{
		Layer:     LayerEpisodic,
		ProjectID: projectID,
		IssueID:   issueID,
		Content:   content,
		Tags:      []string{"validation", checkID},
		Source:    SourceValidationResult,
	})
}
