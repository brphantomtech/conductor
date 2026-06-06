package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/conductor-sh/conductor/internal/config"
	"github.com/conductor-sh/conductor/internal/harness"
	"github.com/conductor-sh/conductor/internal/validation"
)

// newValidationCommand wires the `conductor validation ...` parent command.
// The only subcommand is `run`, which executes the configured checks against a
// workspace ad hoc (SPEC §15.2) — the live per-turn invocation is owned by the
// Phase 7 router turn loop.
func newValidationCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validation",
		Short: "Run and inspect the Validation Pipeline",
	}
	cmd.AddCommand(newValidationRunCommand())
	return cmd
}

// validationRunFlags carries the CLI options for `conductor validation run`.
type validationRunFlags struct {
	harness   string
	workspace string
	turnIndex int
	format    string
}

func newValidationRunCommand() *cobra.Command {
	flags := &validationRunFlags{}
	cmd := &cobra.Command{
		Use:   "run [workspace]",
		Short: "Run the configured validation checks against a workspace",
		Long: "Load the validation config from HARNESS.md, execute every " +
			"configured check in the given workspace directory, classify each " +
			"as passed/failed/timeout, persist the results to " +
			".conductor/validation/<turn_index>.json, and report each result.",
		Args:          cobra.MaximumNArgs(1),
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ws := flags.workspace
			if len(args) == 1 {
				ws = args[0]
			}
			if ws == "" {
				ws = "."
			}
			return runValidationRun(cmd.Context(), cmd.OutOrStdout(), flags, ws)
		},
	}
	cmd.Flags().StringVar(&flags.harness, "harness", "",
		"path to HARNESS.md (default: $CONDUCTOR_HARNESS_PATH or ./HARNESS.md)")
	cmd.Flags().StringVar(&flags.workspace, "workspace", "",
		"workspace directory to run checks in (default: current directory or positional arg)")
	cmd.Flags().IntVar(&flags.turnIndex, "turn-index", 0,
		"turn index to record results under")
	cmd.Flags().StringVar(&flags.format, "format", outputFormatText,
		"output format (text, json)")
	return cmd
}

// validationRunReport is the structured payload `validation run` writes to
// stdout, shared by both output formats.
type validationRunReport struct {
	Workspace string                        `json:"workspace"`
	TurnIndex int                           `json:"turn_index"`
	Checks    []validation.ValidationResult `json:"checks"`
}

func runValidationRun(ctx context.Context, out io.Writer, flags *validationRunFlags, wsPath string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if flags.format != outputFormatText && flags.format != outputFormatJSON {
		return fmt.Errorf("validation run: unsupported --format %q (want text or json)", flags.format)
	}

	harnessPath := harness.ResolvePath(flags.harness, nil)
	res, _ := harness.Load(harnessPath, config.LoadOptions{})
	cfg := res.Config

	absWS, err := filepath.Abs(wsPath)
	if err != nil {
		return fmt.Errorf("validation run: resolve workspace path: %w", err)
	}
	validationDir := filepath.Join(absWS, ".conductor", "validation")

	pipeline := validation.New(cfg.Validation,
		validation.WithCommandFactory(validation.NewDirCommandFactory(absWS)),
	)

	result, err := pipeline.Run(ctx, validationDir, flags.turnIndex)
	if err != nil {
		return fmt.Errorf("validation run: %w", err)
	}

	rep := validationRunReport{
		Workspace: absWS,
		TurnIndex: result.TurnIndex,
		Checks:    result.Checks,
	}
	return writeValidationReport(out, rep, flags.format)
}

// writeValidationReport emits the report in the requested format.
func writeValidationReport(out io.Writer, rep validationRunReport, format string) error {
	if format == outputFormatJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			return fmt.Errorf("validation run: encode json: %w", err)
		}
		return nil
	}
	if len(rep.Checks) == 0 {
		if _, err := fmt.Fprintf(out, "no checks configured (workspace=%s)\n", rep.Workspace); err != nil {
			return fmt.Errorf("validation run: write: %w", err)
		}
		return nil
	}
	for _, c := range rep.Checks {
		label := c.Name
		if label == "" {
			label = c.CheckID
		}
		if _, err := fmt.Fprintf(out, "%s %s (exit=%d, %dms)\n",
			validationStatusTag(c.Status), label, c.ExitCode, c.DurationMS); err != nil {
			return fmt.Errorf("validation run: write: %w", err)
		}
	}
	return nil
}

// validationStatusTag maps a status to a greppable text tag.
func validationStatusTag(s validation.Status) string {
	switch s {
	case validation.StatusPassed:
		return "PASS"
	case validation.StatusFailed:
		return "FAIL"
	case validation.StatusTimeout:
		return "TIMEOUT"
	default:
		return "UNKNOWN"
	}
}
