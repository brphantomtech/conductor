package cmd

import (
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
)

// rootFlags holds values bound to flags on the root command. They are
// resolved in initLogger (PersistentPreRunE) so every subcommand sees the
// configured logger.
type rootFlags struct {
	logLevel  string
	logFormat string
}

// rootContext carries the bits of state every subcommand needs but should
// not have to re-parse from flags. Currently just the configured logger;
// it grows as later phases add the Config and DB.
type rootContext struct {
	log zerolog.Logger
}

// NewRootCommand builds the top-level Cobra command tree. It is exported
// so the main package and tests can both construct independent trees;
// global state in init() makes parallel testing harder than it needs to be.
func NewRootCommand() *cobra.Command {
	flags := &rootFlags{}
	rctx := &rootContext{}

	root := &cobra.Command{
		Use:           "conductor",
		Short:         "Conductor — provider-agnostic AI agent orchestration service",
		SilenceUsage:  true,
		SilenceErrors: false,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			log, err := NewLogger(LogOptions{
				Level:  flags.logLevel,
				Format: flags.logFormat,
				Out:    cmd.ErrOrStderr(),
			})
			if err != nil {
				return err
			}
			rctx.log = log
			return nil
		},
	}

	root.PersistentFlags().StringVar(&flags.logLevel, "log-level", "info",
		"log level (debug, info, warn, error)")
	root.PersistentFlags().StringVar(&flags.logFormat, "log-format", "json",
		"log format (json, text)")

	root.AddCommand(newVersionCommand())
	root.AddCommand(newHarnessCommand())
	root.AddCommand(newStartCommand(rctx))

	return root
}
