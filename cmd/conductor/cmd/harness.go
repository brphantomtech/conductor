package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/conductor-sh/conductor/internal/harness"
)

// newHarnessCommand wires the `conductor harness ...` parent command. The
// only subcommand active in Phase 1 is `validate`; `check` and friends
// arrive in Phase 12.
func newHarnessCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "harness",
		Short: "HARNESS.md validation and runtime inspection",
	}
	cmd.AddCommand(newHarnessValidateCommand())
	return cmd
}

func newHarnessValidateCommand() *cobra.Command {
	var path string
	cmd := &cobra.Command{
		Use:   "validate [path]",
		Short: "Validate a HARNESS.md file without starting the service",
		Long: "Validate the structural integrity of a HARNESS.md file. " +
			"In Phase 1 this is a stub that only verifies the file exists; " +
			"the full schema/template validator lands in Phase 2.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := path
			if len(args) == 1 {
				target = args[0]
			}
			if target == "" {
				target = "HARNESS.md"
			}

			if _, err := os.Stat(target); err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("%w: %s", harness.ErrMissingHarnessFile, target)
				}
				return fmt.Errorf("harness validate: stat %q: %w", target, err)
			}

			out := cmd.OutOrStdout()
			if _, err := fmt.Fprintf(out,
				"OK (parser stub — full validation lands in Phase 2): %s\n",
				target,
			); err != nil {
				return fmt.Errorf("harness validate: write: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&path, "harness", "", "path to HARNESS.md (default: ./HARNESS.md)")
	return cmd
}
