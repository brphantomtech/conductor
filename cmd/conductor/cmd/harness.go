package cmd

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/conductor-sh/conductor/internal/harness"
)

// newHarnessCommand wires the `conductor harness ...` parent command.
// `validate` is the only subcommand wired in Phase 2; `check` and
// `reload` land later (Phase 12 + Phase 14).
func newHarnessCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "harness",
		Short: "HARNESS.md validation and runtime inspection",
	}
	cmd.AddCommand(newHarnessValidateCommand())
	return cmd
}

// newHarnessValidateCommand returns the Phase 2 `harness validate` command:
// it resolves the path per SPEC §5.1, parses HARNESS.md per §5.2, merges
// config per §6.1, runs schema validation per §6.4, and prints a
// categorized error list to stderr if anything fails.
func newHarnessValidateCommand() *cobra.Command {
	var harnessPath string
	cmd := &cobra.Command{
		Use:   "validate [path]",
		Short: "Validate a HARNESS.md file without starting the service",
		Long: "Validate the structural integrity, schema, and template " +
			"syntax of a HARNESS.md file. Resolves the path using the " +
			"SPEC §5.1 precedence (flag > env > cwd > doc store), parses " +
			"the file, merges defaults + env vars, and reports every " +
			"problem at once.",
		Args: cobra.MaximumNArgs(1),
		// runHarnessValidate emits its own categorized stderr output; we
		// silence Cobra's default "Error: ..." line so each underlying
		// problem is only printed once.
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			target := harnessPath
			if len(args) == 1 {
				target = args[0]
			}
			return runHarnessValidate(cmd.OutOrStdout(), cmd.ErrOrStderr(), target)
		},
	}
	cmd.Flags().StringVar(&harnessPath, "harness", "",
		"path to HARNESS.md (overrides --harness on the parent command)")
	return cmd
}

// runHarnessValidate is the testable core of `harness validate`. It
// returns the same error the Cobra RunE would, so tests can assert on
// the returned error type *and* on the bytes written to stderr.
func runHarnessValidate(stdout, stderr io.Writer, target string) error {
	res, err := harness.Load(harness.LoadOptions{Flag: target})
	if err != nil {
		printValidationErrors(stderr, res.Path, err)
		return errSilentExit{err: err}
	}

	tmpls := make([]string, 0, len(res.Definition.PromptTemplates))
	for name := range res.Definition.PromptTemplates {
		tmpls = append(tmpls, name)
	}
	sort.Strings(tmpls)

	_, werr := fmt.Fprintf(stdout, "OK: %s (front matter %d keys, %d templates [%s])\n",
		res.Path, len(res.Definition.FrontMatter), len(tmpls), strings.Join(tmpls, ", "),
	)
	if werr != nil {
		return fmt.Errorf("harness validate: write: %w", werr)
	}
	return nil
}

// ErrSilent is the sentinel returned by subcommands that have already
// printed their own structured error output. main.go uses errors.Is to
// detect it and skip the default "Error: <msg>" line; Cobra is
// SilenceErrors-on at the same time so the only side effect is a
// non-zero exit code.
var ErrSilent = errors.New("conductor: silent error already reported")

// errSilentExit wraps a real error with ErrSilent so callers can still
// inspect the underlying problem (via errors.Is / errors.As) while the
// runtime treats the failure as already-reported.
type errSilentExit struct{ err error }

func (e errSilentExit) Error() string { return e.err.Error() }
func (e errSilentExit) Unwrap() []error {
	return []error{e.err, ErrSilent}
}

// printValidationErrors expands an `errors.Join`-style error into one
// classified line per underlying problem. Categories are sorted so the
// operator always sees the same ordering for the same input.
func printValidationErrors(w io.Writer, path string, err error) {
	if path != "" {
		fmt.Fprintf(w, "harness validate: %s\n", path)
	}

	type classifiedErr struct {
		code     string
		category int
		message  string
	}
	var rows []classifiedErr

	for _, sub := range flatten(err) {
		code, cat := classifyError(sub)
		rows = append(rows, classifiedErr{
			code:     code,
			category: cat,
			message:  sub.Error(),
		})
	}

	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].category != rows[j].category {
			return rows[i].category < rows[j].category
		}
		return rows[i].code < rows[j].code
	})

	for _, r := range rows {
		fmt.Fprintf(w, "  [%s] %s\n", r.code, r.message)
	}
}

// flatten walks an arbitrarily nested errors.Join tree and returns the
// leaf errors in their original order.
func flatten(err error) []error {
	if err == nil {
		return nil
	}
	type unwrapMany interface{ Unwrap() []error }
	if u, ok := err.(unwrapMany); ok {
		var out []error
		for _, sub := range u.Unwrap() {
			out = append(out, flatten(sub)...)
		}
		if len(out) == 0 {
			return []error{err}
		}
		return out
	}
	return []error{err}
}

// classifyError maps an error to (code, category) where category orders
// output: file (1) → parse (2) → schema (3) → templates (4) → other (5).
func classifyError(err error) (string, int) {
	switch {
	case errors.Is(err, harness.ErrMissingHarnessFile):
		return harness.ErrMissingHarnessFile.Error(), 1
	case errors.Is(err, harness.ErrHarnessFrontMatterShape):
		return harness.ErrHarnessFrontMatterShape.Error(), 2
	case errors.Is(err, harness.ErrHarnessParse):
		return harness.ErrHarnessParse.Error(), 2
	case errors.Is(err, harness.ErrTemplateParse):
		return harness.ErrTemplateParse.Error(), 4
	case errors.Is(err, harness.ErrTemplateRender):
		return harness.ErrTemplateRender.Error(), 4
	case errors.Is(err, harness.ErrPipelineRoleMissingTemplate):
		return harness.ErrPipelineRoleMissingTemplate.Error(), 4
	case errors.Is(err, harness.ErrProjectIDMissing):
		return harness.ErrProjectIDMissing.Error(), 3
	case errors.Is(err, harness.ErrTrackerKindMissing):
		return harness.ErrTrackerKindMissing.Error(), 3
	case errors.Is(err, harness.ErrTrackerKindUnsupported):
		return harness.ErrTrackerKindUnsupported.Error(), 3
	case errors.Is(err, harness.ErrTrackerAPIKeyMissing):
		return harness.ErrTrackerAPIKeyMissing.Error(), 3
	case errors.Is(err, harness.ErrTrackerProjectRefMissing):
		return harness.ErrTrackerProjectRefMissing.Error(), 3
	case errors.Is(err, harness.ErrProviderUnsupported):
		return harness.ErrProviderUnsupported.Error(), 3
	case errors.Is(err, harness.ErrProviderAPIKeyMissing):
		return harness.ErrProviderAPIKeyMissing.Error(), 3
	case errors.Is(err, harness.ErrKnowledgeBackendUnsupported):
		return harness.ErrKnowledgeBackendUnsupported.Error(), 3
	case errors.Is(err, harness.ErrMemoryBackendUnsupported):
		return harness.ErrMemoryBackendUnsupported.Error(), 3
	case errors.Is(err, harness.ErrDocStoreBackendUnsupported):
		return harness.ErrDocStoreBackendUnsupported.Error(), 3
	}
	return "unclassified", 5
}
