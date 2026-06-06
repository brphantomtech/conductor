package knowledge

import "strings"

// chunkSize is the SPEC §8.2 Stage 2 line-chunker window.
const chunkSize = 200

// chunkOverlap is the SPEC §8.2 Stage 2 line-chunker overlap.
const chunkOverlap = 50

// chunker is the fallback parser (SPEC §8.2 Stage 2): it splits a file into
// overlapping 200-line chunks with 50-line overlap. It extracts no symbols or
// imports — it is used for unsupported languages, config, and documentation.
type chunker struct {
	language string
}

// newChunker constructs a chunker tagging produced units with language (which
// may be empty for unknown file types).
func newChunker(language string) *chunker {
	return &chunker{language: language}
}

// parse splits content into overlapping line chunks. A file with no content
// yields no units.
func (c *chunker) parse(relPath string, content []byte) parsedFile {
	text := string(content)
	if strings.TrimSpace(text) == "" {
		return parsedFile{Language: c.language}
	}
	lines := strings.Split(text, "\n")
	stride := chunkSize - chunkOverlap // 150-line advance per chunk

	var units []parsedUnit
	for start := 0; start < len(lines); start += stride {
		end := start + chunkSize
		if end > len(lines) {
			end = len(lines)
		}
		units = append(units, parsedUnit{
			Kind:      NodeChunk,
			Content:   strings.Join(lines[start:end], "\n"),
			LineStart: start + 1,
			LineEnd:   end,
		})
		if end == len(lines) {
			break
		}
	}
	return parsedFile{Language: c.language, Units: units}
}

var _ unitParser = (*chunker)(nil)
