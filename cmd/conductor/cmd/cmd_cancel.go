package cmd

import (
	"github.com/spf13/cobra"
)

// newCancelCommand wires `conductor cancel <id>` (SPEC §19.1). Cancelling an
// active run targets a running orchestrator over the live control channel
// (Phase 14). This phase parses and validates the argument, then reports the
// structured "requires the running service" result rather than silently
// no-oping (it shares runLiveOp with dispatch).
func newCancelCommand() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "cancel <id>",
		Short: "Cancel an active run by issue identifier (requires the running service)",
		Long: "Cancel the in-flight run for the named issue. This is a live " +
			"operation against the running service control channel (Phase 14); " +
			"when no service is available the command reports a structured " +
			"requirement instead of silently succeeding.",
		Args:          cobra.ExactArgs(1),
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLiveOp(cmd.OutOrStdout(), "cancel", args[0], format)
		},
	}
	cmd.Flags().StringVar(&format, "format", outputFormatText,
		"output format (text, json)")
	return cmd
}
