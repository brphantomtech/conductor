package cmd

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/conductor-sh/conductor/internal/audit"
	"github.com/conductor-sh/conductor/internal/config"
	"github.com/conductor-sh/conductor/internal/db"
	"github.com/conductor-sh/conductor/internal/harness"
)

// startFlags captures the SPEC §19.2 surface of `conductor start`. Phase 2
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
			"and validates HARNESS.md + configuration, writes one " +
			"placeholder audit event to confirm the audit pipeline, then " +
			"exits without starting the orchestrator. Without --dry-run, " +
			"a fsnotify watcher reloads HARNESS.md on disk changes.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			return runStart(ctx, cmd, rctx, flags)
		},
	}

	cmd.Flags().StringVar(&flags.harness, "harness", "",
		"path to HARNESS.md (default: env CONDUCTOR_HARNESS_PATH or ./HARNESS.md)")
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
	res, err := harness.Load(harness.LoadOptions{
		Flag:     flags.harness,
		CLIFlags: cmd.Flags(),
	})
	if err != nil {
		// In a dry-run, surface validation errors as warnings so an
		// incomplete HARNESS.md doesn't block the audit smoke test —
		// matches the Phase 1 ergonomics operators are used to. For
		// non-dry-run we fail loud.
		if flags.dryRun && res.Definition != nil {
			rctx.log.Warn().Err(err).Str("path", res.Path).Msg("harness validation reported issues")
		} else if flags.dryRun && errors.Is(err, harness.ErrMissingHarnessFile) {
			rctx.log.Warn().Err(err).Msg("no HARNESS.md found; continuing with defaults")
		} else {
			return fmt.Errorf("start: load harness: %w", err)
		}
	}

	cfg := res.Config
	if res.Definition == nil {
		// No harness found, fall back to a defaults-only config so the
		// audit pipeline can still be exercised in dry-run.
		cfg, err = config.Load(config.LoadOptions{Flags: cmd.Flags()})
		if err != nil {
			return fmt.Errorf("start: load config (no harness): %w", err)
		}
	}

	rctx.log.Info().
		Str("harness_path", res.Path).
		Str("harness_source", string(res.Source)).
		Bool("dry_run", flags.dryRun).
		Msg("conductor starting")

	// Open the database to prove the audit pipeline works end-to-end.
	// In Phase 2 we use an in-memory database when --dry-run is set so
	// the command can run on a clean checkout without any state.
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
			"phase":          2,
			"dry_run":        flags.dryRun,
			"harness":        res.Path,
			"harness_source": string(res.Source),
			"comment":        "phase-2 placeholder; orchestrator lands in Phase 6",
		},
	}); err != nil {
		return fmt.Errorf("start: write audit event: %w", err)
	}

	if flags.dryRun {
		out := cmd.OutOrStdout()
		tmpls := sortedTemplateRoles(res.Definition)
		if _, err := fmt.Fprintf(out,
			"dry run complete: harness=%s (source=%s), templates=%v, audit pipeline verified\n",
			res.Path, res.Source, tmpls,
		); err != nil {
			return fmt.Errorf("start: write dry-run summary: %w", err)
		}
		return nil
	}

	if res.Definition != nil && res.Path != "" {
		w := harness.NewWatcher(res.Path, res.Definition, cfg, harnessAuditAdapter{writer: writer, projectID: cfg.Project.ID}, rctx.log)
		go func() {
			if err := w.Run(ctx); err != nil {
				rctx.log.Error().Err(err).Msg("harness watcher exited with error")
			}
		}()
	}

	rctx.log.Info().Msg("orchestrator not yet implemented (Phase 6) — exiting cleanly")
	return nil
}

func sortedTemplateRoles(def *harness.Definition) []string {
	if def == nil {
		return nil
	}
	out := make([]string, 0, len(def.PromptTemplates))
	for name := range def.PromptTemplates {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// harnessAuditAdapter bridges the watcher's consumer-defined AuditWriter
// interface to the real audit.Writer. The watcher cannot import
// internal/audit directly (see internal/harness/watcher.go for the
// rationale and docs/conventions.md §3.1 for the rule), so the cmd
// layer — which is allowed to depend on both — supplies the adapter.
type harnessAuditAdapter struct {
	writer    *audit.Writer
	projectID string
}

func (a harnessAuditAdapter) Write(ctx context.Context, evt harness.AuditEvent) error {
	pid := evt.ProjectID
	if pid == "" {
		pid = a.projectID
	}
	return a.writer.Write(ctx, audit.AuditEvent{
		ProjectID: pid,
		EventType: audit.EventType(evt.EventType),
		Payload:   evt.Payload,
	})
}
