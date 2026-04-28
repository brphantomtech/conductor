package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/conductor-sh/conductor/internal/audit"
	"github.com/conductor-sh/conductor/internal/config"
	"github.com/conductor-sh/conductor/internal/db"
)

// startFlags captures the SPEC §19.2 surface of `conductor start`. Phase 1
// honors --harness, --log-level, --log-format, and --dry-run end-to-end;
// the remaining flags are accepted (so the help text matches the SPEC) but
// have no effect until later phases wire the orchestrator.
type startFlags struct {
	harness     string
	port        int
	noDashboard bool
	noAPI       bool
	dryRun      bool
}

func newStartCommand(rctx *rootContext) *cobra.Command {
	flags := &startFlags{}
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the Conductor service",
		Long: "Start the Conductor service. With --dry-run the binary loads " +
			"and validates configuration, writes one placeholder audit event " +
			"to confirm the audit pipeline, then exits without starting the " +
			"orchestrator.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			return runStart(ctx, cmd, rctx, flags)
		},
	}

	cmd.Flags().StringVar(&flags.harness, "harness", config.DefaultHarnessPath,
		"path to HARNESS.md (default: ./HARNESS.md)")
	cmd.Flags().IntVar(&flags.port, "port", 0, "override server.port")
	cmd.Flags().BoolVar(&flags.noDashboard, "no-dashboard", false,
		"disable the web dashboard")
	cmd.Flags().BoolVar(&flags.noAPI, "no-api", false,
		"disable the REST API")
	cmd.Flags().BoolVar(&flags.dryRun, "dry-run", false,
		"validate config and print what would run; do not start")
	return cmd
}

func runStart(ctx context.Context, cmd *cobra.Command, rctx *rootContext, flags *startFlags) error {
	cfg, err := config.Load(config.LoadOptions{
		Flags: cmd.Flags(),
	})
	if err != nil {
		return fmt.Errorf("start: load config: %w", err)
	}

	rctx.log.Info().
		Str("harness_path", flags.harness).
		Bool("dry_run", flags.dryRun).
		Msg("conductor starting")

	// Best-effort validation: in Phase 1 most installations will not yet
	// have a complete HARNESS.md, so we surface validation errors but do
	// not exit on them when --dry-run is set. The orchestrator (Phase 6)
	// will hard-fail on the same errors when it boots.
	if err := config.Validate(cfg); err != nil {
		if flags.dryRun {
			rctx.log.Warn().Err(err).Msg("config validation reported issues")
		} else {
			return fmt.Errorf("start: validate config: %w", err)
		}
	}

	// Open the database to prove the audit pipeline works end-to-end. In
	// Phase 1 we use an in-memory database when --dry-run is set so the
	// command can run on a clean checkout without any state.
	dsn := ""
	if !flags.dryRun {
		dsn = "conductor.db"
	}
	d, err := db.Open(ctx, db.Options{Driver: db.DriverSQLite, DSN: dsn})
	if err != nil {
		return fmt.Errorf("start: open db: %w", err)
	}
	defer func() { _ = d.Close() }()

	if _, err := db.Migrate(ctx, d); err != nil {
		return fmt.Errorf("start: migrate db: %w", err)
	}

	writer := audit.NewWriter(rctx.log)
	writer.AddSink(audit.NewDBSink(d))
	defer func() { _ = writer.Close() }()

	if err := writer.Write(ctx, audit.AuditEvent{
		ProjectID: cfg.Project.ID,
		EventType: audit.EventRunAttemptStarted,
		Payload: map[string]any{
			"phase":   1,
			"dry_run": flags.dryRun,
			"harness": flags.harness,
			"comment": "phase-1 placeholder; orchestrator lands in Phase 6",
		},
	}); err != nil {
		return fmt.Errorf("start: write audit event: %w", err)
	}

	if flags.dryRun {
		out := cmd.OutOrStdout()
		if _, err := fmt.Fprintln(out, "dry run complete: config loaded, audit pipeline verified"); err != nil {
			return fmt.Errorf("start: write dry-run summary: %w", err)
		}
		return nil
	}

	rctx.log.Info().Msg("orchestrator not yet implemented (Phase 6) — exiting cleanly")
	return nil
}
