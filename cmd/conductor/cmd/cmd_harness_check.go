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
)

// harnessCheckFlags carries the CLI options for `conductor harness check`.
type harnessCheckFlags struct {
	harness   string
	workspace string
	format    string
}

// newHarnessCheckCommand wires `conductor harness check` (SPEC §11.2 on-demand
// rule run). It loads harness_rules from HARNESS.md, executes each rule's check
// in the given workspace directory, and reports the resulting violations grouped
// by severity.
func newHarnessCheckCommand() *cobra.Command {
	flags := &harnessCheckFlags{}
	cmd := &cobra.Command{
		Use:   "check [workspace]",
		Short: "Run the configured harness rules on demand",
		Long: "Load harness_rules from HARNESS.md, execute every rule's check " +
			"command in the given workspace directory, classify each non-zero " +
			"exit as a violation at the rule's severity, and report the " +
			"violations grouped by severity (warning, error, blocking).",
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
			return runHarnessCheck(cmd.Context(), cmd.OutOrStdout(), flags, ws)
		},
	}
	cmd.Flags().StringVar(&flags.harness, "harness", "",
		"path to HARNESS.md (default: $CONDUCTOR_HARNESS_PATH or ./HARNESS.md)")
	cmd.Flags().StringVar(&flags.workspace, "workspace", "",
		"workspace directory to run rule checks in (default: current directory or positional arg)")
	cmd.Flags().StringVar(&flags.format, "format", outputFormatText,
		"output format (text, json)")
	return cmd
}

// harnessCheckReport is the structured payload `harness check` writes to stdout.
type harnessCheckReport struct {
	Workspace  string              `json:"workspace"`
	Violations []harness.Violation `json:"violations"`
	BySeverity map[string][]string `json:"by_severity"`
}

func runHarnessCheck(ctx context.Context, out io.Writer, flags *harnessCheckFlags, wsPath string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if flags.format != outputFormatText && flags.format != outputFormatJSON {
		return fmt.Errorf("harness check: unsupported --format %q (want text or json)", flags.format)
	}

	harnessPath := harness.ResolvePath(flags.harness, nil)
	res, _ := harness.Load(harnessPath, config.LoadOptions{})
	cfg := res.Config

	absWS, err := filepath.Abs(wsPath)
	if err != nil {
		return fmt.Errorf("harness check: resolve workspace path: %w", err)
	}

	runner := harness.NewRunner(harness.NewDirCommandFactory(absWS))
	violations := runner.Run(ctx, cfg.HarnessRules)

	rep := harnessCheckReport{
		Workspace:  absWS,
		Violations: violations,
		BySeverity: groupBySeverity(violations),
	}
	return writeHarnessCheckReport(out, rep, flags.format)
}

// groupBySeverity buckets violation rule labels by their severity string.
func groupBySeverity(violations []harness.Violation) map[string][]string {
	out := map[string][]string{}
	for _, v := range violations {
		label := v.Name
		if label == "" {
			label = v.RuleID
		}
		out[string(v.Severity)] = append(out[string(v.Severity)], label)
	}
	return out
}

// writeHarnessCheckReport emits the report in the requested format. Text format
// prints one line per violation grouped by severity so it greps cleanly.
func writeHarnessCheckReport(out io.Writer, rep harnessCheckReport, format string) error {
	if format == outputFormatJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			return fmt.Errorf("harness check: encode json: %w", err)
		}
		return nil
	}
	if len(rep.Violations) == 0 {
		if _, err := fmt.Fprintf(out, "OK no violations (workspace=%s)\n", rep.Workspace); err != nil {
			return fmt.Errorf("harness check: write: %w", err)
		}
		return nil
	}
	// Print grouped by severity in a stable order: blocking, error, warning.
	order := []harness.Severity{harness.SeverityBlocking, harness.SeverityError, harness.SeverityWarning}
	for _, sev := range order {
		group := violationsOfSeverity(rep.Violations, sev)
		if len(group) == 0 {
			continue
		}
		if _, err := fmt.Fprintf(out, "%s (%d):\n", sev, len(group)); err != nil {
			return fmt.Errorf("harness check: write: %w", err)
		}
		for _, v := range group {
			label := v.Name
			if label == "" {
				label = v.RuleID
			}
			if _, err := fmt.Fprintf(out, "  [%s] %s: %s\n", v.RuleID, label, v.Summary); err != nil {
				return fmt.Errorf("harness check: write: %w", err)
			}
		}
	}
	return nil
}

// violationsOfSeverity returns the violations matching sev in input order.
func violationsOfSeverity(violations []harness.Violation, sev harness.Severity) []harness.Violation {
	var out []harness.Violation
	for _, v := range violations {
		if v.Severity == sev {
			out = append(out, v)
		}
	}
	return out
}
