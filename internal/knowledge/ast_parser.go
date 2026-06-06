package knowledge

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// astParserFor returns an AST-backed parser for the given language, or nil when
// no native-free AST parser is available for it.
//
// NOTE(phase-10): only Go has a pure-Go, no-CGo AST parser available in the
// standard library (go/parser), so it is the one language wired to a real AST
// path. The other SPEC §8.2 languages (TS/JS/Python/Rust/C/C++/Java/Ruby/Elixir)
// would require the go-tree-sitter CGo grammars; until those are wired behind a
// build tag, parserFor falls those languages back to the line chunker. This is
// the documented trade-off from design.md (green build prioritized over full
// AST coverage).
func astParserFor(language string) unitParser {
	if language == "go" {
		return &goASTParser{}
	}
	return nil
}

// goASTParser extracts top-level symbols (functions, types, methods) and import
// targets from a Go source file using the standard library go/parser. No CGo,
// no native grammars.
type goASTParser struct{}

func (p *goASTParser) parse(relPath string, content []byte) parsedFile {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, relPath, content, parser.SkipObjectResolution)
	if err != nil || file == nil {
		// A file that does not parse (partial edit, build-tag-excluded syntax)
		// falls back to the chunker so it is still indexed.
		return newChunker("go").parse(relPath, content)
	}

	out := parsedFile{Language: "go"}
	for _, imp := range file.Imports {
		if imp.Path != nil {
			out.Imports = append(out.Imports, strings.Trim(imp.Path.Value, `"`))
		}
	}

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			out.Units = append(out.Units, symbolUnit(fset, content, funcName(d), d.Pos(), d.End()))
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok {
					out.Units = append(out.Units,
						symbolUnit(fset, content, ts.Name.Name, ts.Pos(), ts.End()))
				}
			}
		}
	}

	if len(out.Units) == 0 {
		// No top-level symbols (e.g. a package with only var blocks): index the
		// whole file as a single chunk so it is still searchable.
		chunk := newChunker("go").parse(relPath, content)
		out.Units = chunk.Units
	}
	return out
}

// funcName returns the symbol name for a function, qualifying methods with their
// receiver type so "(*T).Method" and "(*U).Method" get distinct node IDs.
func funcName(d *ast.FuncDecl) string {
	if d.Recv != nil && len(d.Recv.List) > 0 {
		recv := exprName(d.Recv.List[0].Type)
		if recv != "" {
			return recv + "." + d.Name.Name
		}
	}
	return d.Name.Name
}

// exprName renders a receiver type expression to a short name (T or *T).
func exprName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + exprName(t.X)
	default:
		return ""
	}
}

// symbolUnit builds a parsedUnit for a symbol spanning [start, end).
func symbolUnit(fset *token.FileSet, content []byte, name string, start, end token.Pos) parsedUnit {
	sp := fset.Position(start)
	ep := fset.Position(end)
	snippet := ""
	if sp.Offset >= 0 && ep.Offset <= len(content) && sp.Offset < ep.Offset {
		snippet = string(content[sp.Offset:ep.Offset])
	}
	return parsedUnit{
		Kind:      NodeSymbol,
		Name:      name,
		Content:   snippet,
		LineStart: sp.Line,
		LineEnd:   ep.Line,
	}
}

var _ unitParser = (*goASTParser)(nil)
