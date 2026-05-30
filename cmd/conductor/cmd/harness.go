package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/spf13/cobra"

	"github.com/conductor-sh/conductor/internal/config"
	"github.com/conductor-sh/conductor/internal/harness"
)

// outputFormatText emits human-readable lines; outputFormatJSON emits a
// single JSON object so downstream tooling (CI gates, IDE extensions) can
// parse the report without scraping text.
const (
	outputFormatText = "text"
	outputFormatJSON = "json"
)

// newHarnessCommand wires the `conductor harness ...` parent command. The
// only subcommand active in Phase 2 is `validate`; `check` arrives in
// Phase 12.
func newHarnessCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "harness",
		Short: "HARNESS.md validation and runtime inspection",
	}
	cmd.AddCommand(newHarnessValidateCommand())
	return cmd
}

// validateFlags carries the CLI options for `conductor harness validate`.
// Path is positional or `--harness`; Format selects text vs JSON output.
type validateFlags struct {
	path   string
	format string
}

func newHarnessValidateCommand() *cobra.Command {
	flags := &validateFlags{}
	cmd := &cobra.Command{
		Use:   "validate [path]",
		Short: "Validate a HARNESS.md file without starting the service",
		Long: "Parse HARNESS.md, decode its YAML front matter into the typed " +
			"config, and dry-render every prompt template against the SPEC §16.2 " +
			"mock bindings. Exits non-zero when the parser, config decoder, or " +
			"template validator reports an issue.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// A positional path overrides --harness; either then takes
			// precedence over $CONDUCTOR_HARNESS_PATH and the cwd default
			// per SPEC §5.1 discovery order.
			explicit := flags.path
			if len(args) == 1 {
				explicit = args[0]
			}
			target := harness.ResolvePath(explicit, nil)
			return runHarnessValidate(cmd.OutOrStdout(), target, flags.format)
		},
	}
	cmd.Flags().StringVar(&flags.path, "harness", "",
		"path to HARNESS.md (default: ./HARNESS.md)")
	cmd.Flags().StringVar(&flags.format, "format", outputFormatText,
		"output format (text, json)")
	return cmd
}

// validateReport is the structured payload `harness validate` writes to
// stdout. It is shared by both output formats so JSON consumers and humans
// see the same fields.
type validateReport struct {
	Path       string                `json:"path"`
	OK         bool                  `json:"ok"`
	ParseError string                `json:"parse_error,omitempty"`
	Roles      []string              `json:"roles,omitempty"`
	Issues     []validateReportIssue `json:"issues,omitempty"`
}

// validateReportIssue mirrors harness.ValidationIssue but with a stable
// JSON shape (Sentinel emitted as the SPEC string identifier rather than
// the Go error pointer).
type validateReportIssue struct {
	Sentinel string `json:"sentinel"`
	Role     string `json:"role,omitempty"`
	Message  string `json:"message"`
}

// runHarnessValidate is the top-level executor for the subcommand. It loads
// the harness, builds a structured report, prints it, and returns an error
// when the load surfaced one (so cobra propagates a non-zero exit code).
func runHarnessValidate(out io.Writer, path, format string) error {
	if format != outputFormatText && format != outputFormatJSON {
		return fmt.Errorf("harness validate: unsupported --format %q (want text or json)", format)
	}

	res, loadErr := harness.Load(path, config.LoadOptions{})

	rep := buildReport(path, res, loadErr)

	if err := writeReport(out, rep, format); err != nil {
		return err
	}
	if loadErr != nil {
		// Wrap so cobra exits non-zero with context; %w keeps the SPEC
		// sentinel recoverable via errors.Is for callers and tests.
		return fmt.Errorf("harness validate: %w", loadErr)
	}
	return nil
}

// buildReport translates a (LoadResult, error) pair into the wire-shaped
// report. Parse-time failures populate ParseError; validation findings fill
// Issues. Success populates Roles so the operator can confirm at a glance
// which `## <role>` sections were extracted.
func buildReport(path string, res harness.LoadResult, loadErr error) validateReport {
	rep := validateReport{Path: path}

	switch {
	case loadErr != nil && res.Definition == nil:
		rep.ParseError = loadErr.Error()
	case loadErr != nil:
		rep.Issues = collectIssues(res.Validation.Issues)
		rep.Roles = sortedRoles(res.Definition)
	default:
		rep.OK = true
		rep.Roles = sortedRoles(res.Definition)
	}
	return rep
}

// collectIssues converts harness.ValidationIssues to the wire shape. The
// sentinel field carries the SPEC §23.1 string so the output matches the
// audit-event identifiers operators are already familiar with.
func collectIssues(in []harness.ValidationIssue) []validateReportIssue {
	if len(in) == 0 {
		return nil
	}
	out := make([]validateReportIssue, 0, len(in))
	for _, iss := range in {
		out = append(out, validateReportIssue{
			Sentinel: sentinelString(iss.Sentinel),
			Role:     iss.Role,
			Message:  iss.Message,
		})
	}
	return out
}

// sentinelString recovers the SPEC identifier from an error. Validation
// issues always carry one of the harness sentinels, but defensive code
// returns an empty string when the chain has been replaced.
func sentinelString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// sortedRoles returns the prompt-template role names in alphabetical order
// so output is deterministic across runs (Go map iteration is randomized).
func sortedRoles(def *harness.Definition) []string {
	if def == nil {
		return nil
	}
	roles := make([]string, 0, len(def.PromptTemplates))
	for r := range def.PromptTemplates {
		roles = append(roles, r)
	}
	sort.Strings(roles)
	return roles
}

// writeReport emits the report in the requested format. Text format prints
// one line per finding so it greps cleanly; JSON format prints a single
// indented object.
func writeReport(out io.Writer, rep validateReport, format string) error {
	if format == outputFormatJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			return fmt.Errorf("harness validate: encode json: %w", err)
		}
		return nil
	}
	return writeTextReport(out, rep)
}

// writeTextReport renders the human-readable form. Each line begins with a
// status tag (`OK`, `FAIL`, `PARSE`) so log-grep filters stay simple.
func writeTextReport(out io.Writer, rep validateReport) error {
	if rep.ParseError != "" {
		_, err := fmt.Fprintf(out, "PARSE %s\n  %s\n", rep.Path, rep.ParseError)
		if err != nil {
			return fmt.Errorf("harness validate: write: %w", err)
		}
		return nil
	}
	if rep.OK {
		_, err := fmt.Fprintf(out, "OK %s (%d roles: %s)\n",
			rep.Path, len(rep.Roles), commaJoin(rep.Roles))
		if err != nil {
			return fmt.Errorf("harness validate: write: %w", err)
		}
		return nil
	}
	if _, err := fmt.Fprintf(out, "FAIL %s (%d issue(s))\n", rep.Path, len(rep.Issues)); err != nil {
		return fmt.Errorf("harness validate: write: %w", err)
	}
	for _, iss := range rep.Issues {
		role := iss.Role
		if role == "" {
			role = "<document>"
		}
		if _, err := fmt.Fprintf(out, "  [%s] role=%s: %s\n", iss.Sentinel, role, iss.Message); err != nil {
			return fmt.Errorf("harness validate: write: %w", err)
		}
	}
	return nil
}

// commaJoin is a tiny helper that returns "<empty>" for an empty slice so
// the OK line stays grammatical when a harness has no `## <role>` headings.
func commaJoin(in []string) string {
	if len(in) == 0 {
		return "<empty>"
	}
	out := in[0]
	for _, s := range in[1:] {
		out += ", " + s
	}
	return out
}
