package knowledge

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/conductor-sh/conductor/internal/config"
)

func TestFormatContextShapeAndBudget(t *testing.T) {
	eng, _ := testEngine(t, config.Knowledge{Enabled: true})
	seedGraph(t, eng)

	block, err := eng.FormatContext(context.Background(), "Fix login", "token validation broken", 0)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(block, contextHeader), "block starts with header")
	require.Contains(t, block, "### Relevant Files and Symbols")
	require.Contains(t, block, "auth.go")
	require.Contains(t, block, "[service]")

	// Budget truncation: a tight budget yields a shorter block.
	small, err := eng.FormatContext(context.Background(), "Fix login", "token validation broken", 120)
	require.NoError(t, err)
	require.LessOrEqual(t, len(small), 120)
	require.NotEmpty(t, small)
}

func TestFormatContextEmptyWhenNoResults(t *testing.T) {
	eng, _ := testEngine(t, config.Knowledge{Enabled: true})
	block, err := eng.FormatContext(context.Background(), "anything", "", 0)
	require.NoError(t, err)
	require.Empty(t, block)
}
