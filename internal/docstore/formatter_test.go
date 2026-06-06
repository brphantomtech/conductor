package docstore

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/conductor-sh/conductor/internal/knowledge"
)

func docResult(name, path, content string) knowledge.ScoredNode {
	return knowledge.ScoredNode{
		Node:  knowledge.Node{Type: knowledge.NodeDoc, Name: name, Path: path, Content: content},
		Score: 1,
	}
}

func TestFormatRelevantDocsShape(t *testing.T) {
	out := FormatRelevantDocs([]knowledge.ScoredNode{
		docResult("Design", "docs/specs/design.md", "the design body"),
		docResult("ADR 1", "docs/adr/1.md", "the decision"),
	}, 0)

	assert.True(t, strings.HasPrefix(out, docContextHeader))
	assert.Contains(t, out, "### Design")
	assert.Contains(t, out, "Source: docs/specs/design.md")
	assert.Contains(t, out, "the design body")
	assert.Contains(t, out, "### ADR 1")
}

func TestFormatRelevantDocsEmpty(t *testing.T) {
	assert.Equal(t, "", FormatRelevantDocs(nil, 0))
}

func TestFormatRelevantDocsSkipsNonDocNodes(t *testing.T) {
	out := FormatRelevantDocs([]knowledge.ScoredNode{
		{Node: knowledge.Node{Type: knowledge.NodeFile, Name: "x", Path: "x.go"}},
	}, 0)
	assert.Equal(t, "", out)
}

func TestFormatRelevantDocsTruncatesToBudget(t *testing.T) {
	long := strings.Repeat("x", 500)
	out := FormatRelevantDocs([]knowledge.ScoredNode{
		docResult("First", "a.md", long),
		docResult("Second", "b.md", long),
	}, 300)
	assert.LessOrEqual(t, len(out), 300)
	assert.Contains(t, out, docContextHeader)
}
