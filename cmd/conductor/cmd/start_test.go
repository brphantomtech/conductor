package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// goodHarness is a minimal, valid HARNESS.md: complete enough to pass
// config validation and to extract three prompt-template roles.
const goodHarness = `---
project:
  id: demo
  name: Demo
tracker:
  kind: linear
  project_slug: team
  api_key: lin_test
providers:
  default:
    provider: anthropic
    model: claude-sonnet-4-5
    api_key: ank_test
routing:
  pipeline: [planner, coder, verifier]
---

## planner

plan

## coder

code

## verifier

verify
`

// withCwd temporarily switches the working directory to dir for the
// duration of fn. Restores the previous cwd on return.
func withCwd(t *testing.T, dir string, fn func()) {
	t.Helper()
	orig, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() {
		_ = os.Chdir(orig)
	})
	fn()
}

func TestRunStart_DryRunReportsTemplateRoles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "HARNESS.md")
	require.NoError(t, os.WriteFile(path, []byte(goodHarness), 0o644))

	withCwd(t, dir, func() {
		flags := &startFlags{harness: path, dryRun: true}
		rctx := &rootContext{log: zerolog.Nop()}

		cmd := &cobra.Command{}
		var stdout bytes.Buffer
		cmd.SetOut(&stdout)
		// runStart reads cmd.Flags() for cli flag bindings; build a
		// matching flag set so config.Load does not panic.
		cmd.Flags().Int("polling-interval", 0, "")

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		err := runStart(ctx, cmd, rctx, flags)
		require.NoError(t, err)

		out := stdout.String()
		require.True(t, strings.HasPrefix(out, "dry run complete:"), "got %q", out)
		require.Contains(t, out, "templates=[coder planner verifier]")
		require.Contains(t, out, "harness="+path)
	})
}

func TestRunStart_DryRunWithoutHarnessWarnsAndContinues(t *testing.T) {
	dir := t.TempDir()
	withCwd(t, dir, func() {
		flags := &startFlags{harness: "", dryRun: true}
		rctx := &rootContext{log: zerolog.Nop()}

		cmd := &cobra.Command{}
		var stdout bytes.Buffer
		cmd.SetOut(&stdout)
		cmd.Flags().Int("polling-interval", 0, "")

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		err := runStart(ctx, cmd, rctx, flags)
		require.NoError(t, err)
		require.Contains(t, stdout.String(), "dry run complete:")
	})
}

func TestRunStart_NonDryRunFailsOnInvalidHarness(t *testing.T) {
	dir := t.TempDir()
	// HARNESS.md missing project.id and api_key → fails validation.
	body := `---
tracker:
  kind: linear
  project_slug: team
providers:
  default:
    provider: anthropic
    api_key: ank
routing:
  pipeline: [planner, coder, verifier]
---

## planner

p

## coder

c

## verifier

v
`
	path := filepath.Join(dir, "HARNESS.md")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))

	withCwd(t, dir, func() {
		flags := &startFlags{harness: path, dryRun: false}
		rctx := &rootContext{log: zerolog.Nop()}

		cmd := &cobra.Command{}
		cmd.SetOut(new(bytes.Buffer))
		cmd.Flags().Int("polling-interval", 0, "")

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		err := runStart(ctx, cmd, rctx, flags)
		require.Error(t, err)
	})
}
