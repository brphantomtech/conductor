package knowledge

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const goSample = `package auth

import (
	"context"

	"github.com/conductor-sh/conductor/internal/config"
	"internal/types"
)

// Service validates tokens.
type Service struct {
	cfg config.ProviderConfig
}

// ValidateToken checks a token.
func (s *Service) ValidateToken(ctx context.Context, token string) error {
	return nil
}

func New() *Service { return &Service{} }
`

func TestGoASTParserSymbolsAndImports(t *testing.T) {
	p := parserFor("internal/auth/auth.go", true)
	pf := p.parse("internal/auth/auth.go", []byte(goSample))

	require.Equal(t, "go", pf.Language)

	names := map[string]parsedUnit{}
	for _, u := range pf.Units {
		require.Equal(t, NodeSymbol, u.Kind)
		names[u.Name] = u
	}
	require.Contains(t, names, "Service")
	require.Contains(t, names, "*Service.ValidateToken")
	require.Contains(t, names, "New")
	require.Greater(t, names["*Service.ValidateToken"].LineStart, 0)

	// External module path is dropped by edge resolution; in-project paths kept.
	require.Contains(t, pf.Imports, "github.com/conductor-sh/conductor/internal/config")
	require.Contains(t, pf.Imports, "internal/types")
}

func TestChunkerBoundariesAndOverlap(t *testing.T) {
	var sb strings.Builder
	for i := 1; i <= 360; i++ {
		sb.WriteString("line")
		sb.WriteByte('\n')
	}
	p := parserFor("data.txt", false)
	pf := p.parse("data.txt", []byte(sb.String()))

	require.Empty(t, pf.Language)
	require.GreaterOrEqual(t, len(pf.Units), 2)

	first := pf.Units[0]
	assert.Equal(t, 1, first.LineStart)
	assert.Equal(t, chunkSize, first.LineEnd)

	second := pf.Units[1]
	// stride = 200 - 50 = 150; second chunk starts at line 151.
	assert.Equal(t, chunkSize-chunkOverlap+1, second.LineStart)
}

func TestParserForFallsBackForUnsupported(t *testing.T) {
	// Python with use_ast=true still falls back to the chunker (no native AST).
	p := parserFor("script.py", true)
	_, ok := p.(*chunker)
	require.True(t, ok, "python should chunk in the native-free build")

	// Go uses the AST parser when use_ast is on.
	g := parserFor("main.go", true)
	_, isAST := g.(*goASTParser)
	require.True(t, isAST)

	// use_ast off => chunker even for Go.
	c := parserFor("main.go", false)
	_, isChunk := c.(*chunker)
	require.True(t, isChunk)
}

func TestResolveImportPath(t *testing.T) {
	require.Equal(t, "", resolveImportPath("github.com/foo/bar"))
	require.Equal(t, "internal/types", resolveImportPath("internal/types"))
	require.Equal(t, "pkg/auth", resolveImportPath("./pkg/auth"))
	require.Equal(t, "", resolveImportPath(""))
}
