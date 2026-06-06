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
	"github.com/conductor-sh/conductor/internal/orchestrator"
	"github.com/conductor-sh/conductor/internal/provider"
	"github.com/conductor-sh/conductor/internal/router"
	"github.com/conductor-sh/conductor/internal/tracker"
	"github.com/conductor-sh/conductor/internal/workspace"
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
		"path to HARNESS.md (default: $CONDUCTOR_HARNESS_PATH or ./HARNESS.md)")
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
	// Resolve the HARNESS.md location per SPEC §5.1: --harness, then
	// $CONDUCTOR_HARNESS_PATH, then the cwd default.
	harnessPath := harness.ResolvePath(flags.harness, nil)
	loadOpts := config.LoadOptions{Flags: cmd.Flags()}

	res, loadErr := harness.Load(harnessPath, loadOpts)

	// Resolve the effective config. When no HARNESS.md could be parsed (a
	// fresh checkout often has none), fall back to a defaults-only config so
	// the audit pipeline can still be exercised in dry-run.
	cfg := res.Config
	if res.Definition == nil {
		var cErr error
		cfg, cErr = config.Load(loadOpts)
		if cErr != nil {
			return fmt.Errorf("start: load config (no harness): %w", cErr)
		}
		if loadErr != nil {
			rctx.log.Warn().Err(loadErr).Str("harness_path", harnessPath).
				Msg("HARNESS.md not loaded; continuing with defaults")
		}
	}

	rctx.log.Info().
		Str("harness_path", harnessPath).
		Bool("dry_run", flags.dryRun).
		Msg("conductor starting")

	// Best-effort startup validation (SPEC §6.4). In dry-run we surface
	// issues but continue; otherwise a bad config or harness is fatal. The
	// orchestrator (Phase 6) will hard-fail on the same checks when it boots.
	if err := config.Validate(cfg); err != nil {
		if flags.dryRun {
			rctx.log.Warn().Err(err).Msg("config validation reported issues")
		} else {
			return fmt.Errorf("start: validate config: %w", err)
		}
	}
	if loadErr != nil && res.Definition != nil {
		if flags.dryRun {
			rctx.log.Warn().Err(loadErr).Msg("harness validation reported issues")
		} else {
			return fmt.Errorf("start: validate harness: %w", loadErr)
		}
	}

	// Open the database to prove the audit pipeline works end-to-end. dry-run
	// uses an in-memory database so the command runs on a clean checkout
	// without leaving state behind.
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
			"phase":   2,
			"dry_run": flags.dryRun,
			"harness": harnessPath,
			"comment": "placeholder; orchestrator lands in Phase 6",
		},
	}); err != nil {
		return fmt.Errorf("start: write audit event: %w", err)
	}

	if flags.dryRun {
		out := cmd.OutOrStdout()
		tmpls := sortedTemplateRoles(res.Definition)
		if _, err := fmt.Fprintf(out,
			"dry run complete: harness=%s, templates=%v, audit pipeline verified\n",
			harnessPath, tmpls,
		); err != nil {
			return fmt.Errorf("start: write dry-run summary: %w", err)
		}
		return nil
	}

	// Hot-reload HARNESS.md on disk changes (SPEC §6.3). The watcher emits
	// ConfigReloaded / ConfigReloadFailed audit events through the same writer.
	if res.Definition != nil {
		w, err := harness.NewWatcher(harness.WatchOptions{
			Path:     harnessPath,
			LoadOpts: loadOpts,
			Audit:    writer,
			Logger:   rctx.log,
		})
		if err != nil {
			return fmt.Errorf("start: create harness watcher: %w", err)
		}
		go func() {
			if err := w.Start(ctx); err != nil {
				rctx.log.Error().Err(err).Msg("harness watcher exited with error")
			}
		}()
	}

	// Construct and run the orchestrator (SPEC §13). It owns the poll loop
	// until the context is cancelled (SIGINT/SIGTERM, wired by the root cmd).
	return runOrchestrator(ctx, rctx, cfg, res.Definition, writer)
}

// runOrchestrator wires the Phase-6 collaborators and runs the poll loop until
// ctx is cancelled. A clean context cancellation (graceful shutdown) is not an
// error.
func runOrchestrator(
	ctx context.Context, rctx *rootContext, cfg config.Config,
	def *harness.Definition, writer *audit.Writer,
) error {
	trackerAdapter, err := tracker.New(cfg.Tracker, tracker.WithLogger(rctx.log))
	if err != nil {
		return fmt.Errorf("start: construct tracker: %w", err)
	}

	coderCfg := resolveProviderConfig(cfg, "coder")
	providerAdapter, err := provider.New(coderCfg, provider.WithLogger(rctx.log))
	if err != nil {
		return fmt.Errorf("start: construct provider: %w", err)
	}

	wsManager := workspace.New(cfg.Workspace, cfg.Hooks,
		workspace.WithLogger(rctx.log),
		workspace.WithAudit(writer),
		workspace.WithProjectID(cfg.Project.ID),
	)

	templates := map[string]string{}
	if def != nil {
		templates = def.PromptTemplates
	}

	configFn := func() config.Config { return cfg }
	templatesFn := func() map[string]string { return templates }

	// Construct the Phase 7 Agent Router (SPEC §12). It implements the
	// orchestrator's classification seam and drives router-selected pipelines.
	// Per-role provider resolution is config-driven; the single adapter serves
	// every role's ProviderConfig (multi-kind adapter routing lands later).
	agentRouter := router.New(
		router.WithProvider(providerAdapter),
		router.WithTracker(trackerAdapter),
		router.WithConfig(configFn),
		router.WithTemplates(templatesFn),
		router.WithAudit(writer),
		router.WithLogger(rctx.log),
	)

	o := orchestrator.New(
		orchestrator.WithTracker(trackerAdapter),
		orchestrator.WithWorkspaces(wsManager),
		orchestrator.WithProvider(providerAdapter, coderCfg),
		orchestrator.WithAudit(writer),
		orchestrator.WithConfig(configFn),
		orchestrator.WithTemplates(templatesFn),
		orchestrator.WithClassifier(agentRouter),
		orchestrator.WithPipelineRouter(agentRouter),
		orchestrator.WithLogger(rctx.log),
	)

	rctx.log.Info().Msg("orchestrator started")
	if err := o.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("start: orchestrator: %w", err)
	}
	rctx.log.Info().Msg("orchestrator stopped")
	return nil
}

// resolveProviderConfig returns the provider config for a role, falling back
// to the default provider when the role has no override (SPEC §5.3.6).
func resolveProviderConfig(cfg config.Config, role string) config.ProviderConfig {
	if rc, ok := cfg.Providers.Roles[role]; ok && rc.Provider != "" {
		return rc
	}
	return cfg.Providers.Default
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
