package cmd

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// ErrRequiresRunningService is returned by the live control-channel commands
// (dispatch, cancel) when no running service is available. The live control
// channel — the HTTP API / IPC of SPEC §18 — is delivered in Phase 14; until
// then these commands report this structured requirement rather than silently
// doing nothing.
var ErrRequiresRunningService = errors.New("requires the running service (Phase 14)")

// liveOpReport is the structured payload the live-operation commands emit so a
// caller (a script, a CI gate) can detect the "needs the service" outcome in
// both text and JSON without scraping prose.
type liveOpReport struct {
	Operation string `json:"operation"`
	IssueID   string `json:"issue_id"`
	Performed bool   `json:"performed"`
	Notice    string `json:"notice"`
}

// newDispatchCommand wires `conductor dispatch <id>` (SPEC §19.1). Force-dispatch
// targets a running orchestrator over the live control channel (Phase 14). This
// phase parses and validates the argument, then reports the structured
// "requires the running service" result rather than silently no-oping.
func newDispatchCommand() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "dispatch <id>",
		Short: "Force-dispatch an issue by identifier (requires the running service)",
		Long: "Force the orchestrator to dispatch the named issue immediately. " +
			"This is a live operation against the running service control " +
			"channel (Phase 14); when no service is available the command " +
			"reports a structured requirement instead of silently succeeding.",
		Args:          cobra.ExactArgs(1),
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLiveOp(cmd.OutOrStdout(), "dispatch", args[0], format)
		},
	}
	cmd.Flags().StringVar(&format, "format", outputFormatText,
		"output format (text, json)")
	return cmd
}

// runLiveOp emits the structured report for a live control-channel operation and
// returns ErrRequiresRunningService so the process exits non-zero — the
// operation was not performed. It is shared by dispatch and cancel.
func runLiveOp(out io.Writer, operation, issueID, format string) error {
	if format != outputFormatText && format != outputFormatJSON {
		return fmt.Errorf("%s: unsupported --format %q (want text or json)", operation, format)
	}
	if issueID == "" {
		return fmt.Errorf("%s: issue id is required", operation)
	}

	rep := liveOpReport{
		Operation: operation,
		IssueID:   issueID,
		Performed: false,
		Notice:    ErrRequiresRunningService.Error(),
	}

	if format == outputFormatJSON {
		if err := writeJSON(out, rep); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintf(out, "%s %s: not performed — %s\n",
			operation, issueID, rep.Notice); err != nil {
			return err
		}
	}
	return fmt.Errorf("%s %s: %w", operation, issueID, ErrRequiresRunningService)
}
