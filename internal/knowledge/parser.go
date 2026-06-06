package knowledge

import (
	"path"
	"strings"
)

// parsedUnit is one indexable unit produced by a parser: either a whole-file
// chunk or an extracted symbol. The pipeline turns each unit into a Node.
type parsedUnit struct {
	// Kind is the node type this unit becomes (NodeChunk or NodeSymbol).
	Kind NodeType
	// Name is the symbol name (empty for whole-file chunk units).
	Name string
	// Content is the raw text snippet for this unit.
	Content string
	// LineStart / LineEnd are 1-based line bounds within the file.
	LineStart int
	LineEnd   int
}

// parsedFile is the parser output for a single file: its units plus the raw
// import targets and reference targets used to build dependency edges.
type parsedFile struct {
	// Language is the detected source language (empty when unknown).
	Language string
	// Units are the symbol/chunk units extracted from the file.
	Units []parsedUnit
	// Imports are raw import targets (module paths or relative paths) the file
	// declares; the pipeline resolves them to node IDs during graph
	// construction.
	Imports []string
}

// parser turns a file's content into a parsedFile.
type unitParser interface {
	parse(relPath string, content []byte) parsedFile
}

// languageByExt maps a file extension to the SPEC §8.2 Stage 2 language label.
// Extensions absent from this map fall through to the chunker with an empty
// language.
var languageByExt = map[string]string{
	".go":   "go",
	".ts":   "typescript",
	".tsx":  "typescript",
	".js":   "javascript",
	".jsx":  "javascript",
	".mjs":  "javascript",
	".py":   "python",
	".rs":   "rust",
	".c":    "c",
	".h":    "c",
	".cc":   "cpp",
	".cpp":  "cpp",
	".hpp":  "cpp",
	".java": "java",
	".rb":   "ruby",
	".ex":   "elixir",
	".exs":  "elixir",
}

// languageFor returns the language label for relPath, or "" when unknown.
func languageFor(relPath string) string {
	return languageByExt[strings.ToLower(path.Ext(relPath))]
}

// parserFor returns the parser for relPath. When useAST is true and the file's
// language is one of the SPEC §8.2 supported languages, an AST-style parser is
// returned; otherwise the line chunker is used.
//
// NOTE(phase-10): the SPEC names tree-sitter via go-tree-sitter for AST
// extraction. That dependency is CGo and ships native grammars per language,
// which does not build cleanly in this pure-Go, cross-platform environment. To
// keep `go build ./...` green on every platform without native toolchains, the
// AST path is implemented as a dependency-free, lexical symbol/import extractor
// (astParser) for Go — the project's own language — and the line chunker is the
// default for everything else. Swapping astParser for a real tree-sitter
// backend behind a build tag is a drop-in change: it only has to satisfy the
// unitParser interface. See design.md "Parser registry" risk note.
func parserFor(relPath string, useAST bool) unitParser {
	lang := languageFor(relPath)
	if useAST && lang != "" {
		if p := astParserFor(lang); p != nil {
			return p
		}
	}
	return newChunker(lang)
}
