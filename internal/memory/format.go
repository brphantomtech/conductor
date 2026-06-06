package memory

import "strings"

// Section headings for the `## Relevant Memory` block (SPEC §9.3).
const (
	headingEpisodic   = "### From previous sessions on this issue"
	headingSemantic   = "### Project knowledge"
	headingProcedural = "### How to do things well (your task type)"
)

// Format renders retrieved entries as the `## Relevant Memory` block
// (SPEC §9.3). Entries are grouped into the episodic, project-knowledge, and
// procedural sections; empty sections are omitted. An empty input yields an
// empty string so callers can skip prompt injection when there is no memory.
func Format(entries []MemoryEntry) string {
	if len(entries) == 0 {
		return ""
	}
	var episodic, semantic, procedural []MemoryEntry
	for _, e := range entries {
		switch e.Layer {
		case LayerEpisodic:
			episodic = append(episodic, e)
		case LayerSemantic:
			semantic = append(semantic, e)
		case LayerProcedural:
			procedural = append(procedural, e)
		}
	}

	var b strings.Builder
	b.WriteString("## Relevant Memory\n")

	writeSection(&b, headingEpisodic, episodic)
	writeSection(&b, headingSemantic, semantic)
	writeSection(&b, headingProcedural, procedural)

	return strings.TrimRight(b.String(), "\n")
}

func writeSection(b *strings.Builder, heading string, entries []MemoryEntry) {
	if len(entries) == 0 {
		return
	}
	b.WriteString("\n")
	b.WriteString(heading)
	b.WriteString("\n\n")
	for _, e := range entries {
		b.WriteString("- ")
		b.WriteString(strings.TrimSpace(e.Content))
		b.WriteString("\n")
	}
}
