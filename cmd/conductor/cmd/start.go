package cmd

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/robfig/cron/v3"
	"github.com/spf13/cobra"

	"github.com/conductor-sh/conductor/internal/audit"
	"github.com/conductor-sh/conductor/internal/config"
	"github.com/conductor-sh/conductor/internal/db"
	"github.com/conductor-sh/conductor/internal/harness"
	"github.com/conductor-sh/conductor/internal/knowledge"
	"github.com/conductor-sh/conductor/internal/memory"
	"github.com/conductor-sh/conductor/internal/orchestrator"
	"github.com/conductor-sh/conductor/internal/provider"
	"github.com/conductor-sh/conductor/internal/router"
	"github.com/conductor-sh/conductor/internal/tracker"
	"github.com/conductor-sh/conductor/internal/validation"
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

	// Construct the Validation Pipeline (SPEC §15) so it is available to the
	// turn loop. The live per-turn invocation point is owned by the Phase 7
	// router (SPEC §12.4 step 5), which builds a workspace-scoped command
	// factory per issue via validation.NewWorkspaceCommandFactory and calls
	// Pipeline.Run; this phase constructs the shared, stateless pipeline.
	validationPipeline := validation.New(cfg.Validation,
		validation.WithLogger(rctx.log),
		validation.WithAudit(writer),
		validation.WithProjectID(cfg.Project.ID),
	)
	rctx.log.Info().
		Bool("validation_enabled", cfg.Validation.Enabled).
		Int("validation_checks", len(cfg.Validation.Checks)).
		Msg("validation pipeline ready")
	// validationPipeline is handed to the router turn loop in Phase 7; retained
	// here as the constructed, shared instance.
	_ = validationPipeline

	templates := map[string]string{}
	if def != nil {
		templates = def.PromptTemplates
	}

	// Construct and wire the Knowledge Engine (Phase 10). It is a standalone
	// background service: when enabled we open its store, optionally index on
	// startup, optionally start the incremental watcher, and report its
	// knowledge_index_status. The Phase 13 tool and the Phase 6 poll-loop seam
	// consume the engine later; here we own its lifecycle and log its status.
	knowledgeStatus := wireKnowledge(ctx, rctx, cfg, writer)
	rctx.log.Info().Str("knowledge_index_status", string(knowledgeStatus)).Msg("knowledge engine status")

	configFn := func() config.Config { return cfg }
	templatesFn := func() map[string]string { return templates }

	// Construct the Phase 7 Agent Router (SPEC §12). It implements the
	// orchestrator's classification seam and drives router-selected pipelines.
	// Per-role provider resolution is config-driven; the single adapter serves
	// every role's ProviderConfig (multi-kind adapter routing lands later).
	//
	// TODO(integration): wire validationPipeline into the router as the SPEC
	// §12.4 step-5 Validator. The router.Validator seam (Run(ctx, workspace,
	// role) error) and validation.Pipeline.Run(ctx, dir, turnIndex) differ; a
	// small adapter mapping role→turn-index and applying fail_on_severity is
	// needed before passing router.WithValidator(...). Until then validation is
	// available via `conductor validation run` and the router runs nil-guarded.
	agentRouter := router.New(
		router.WithProvider(providerAdapter),
		router.WithTracker(trackerAdapter),
		router.WithConfig(configFn),
		router.WithTemplates(templatesFn),
		router.WithAudit(writer),
		router.WithLogger(rctx.log),
	)

	orchOpts := []orchestrator.Option{
		orchestrator.WithTracker(trackerAdapter),
		orchestrator.WithWorkspaces(wsManager),
		orchestrator.WithProvider(providerAdapter, coderCfg),
		orchestrator.WithAudit(writer),
		orchestrator.WithConfig(configFn),
		orchestrator.WithTemplates(templatesFn),
		orchestrator.WithClassifier(agentRouter),
		orchestrator.WithPipelineRouter(agentRouter),
		orchestrator.WithLogger(rctx.log),
	}

	// Wire the Memory Manager as reconciliation Part C (SPEC §13.5): each
	// terminal run writes a session-end episodic memory. Skipped when memory
	// is disabled so the orchestrator behaves exactly as before this phase.
	if cfg.Memory.Enabled {
		consolidationCfg := resolveProviderConfig(cfg, cfg.Memory.ConsolidationProvider)
		embedder := memory.NewAPIProvider(consolidationCfg, nil)
		mgr, mErr := memory.New(ctx, cfg.Memory,
			memory.WithProjectID(cfg.Project.ID),
			memory.WithAudit(writer),
			memory.WithLogger(rctx.log),
			memory.WithEmbedder(embedder),
			memory.WithSynthesizer(embedder),
		)
		if mErr != nil {
			return fmt.Errorf("start: construct memory manager: %w", mErr)
		}
		defer func() { _ = mgr.Close() }()
		orchOpts = append(orchOpts, orchestrator.WithMemoryPostProcessor(memory.NewPostProcessor(mgr)))
	}

	// Wire the Phase 12 Harness Enforcer (SPEC §11). The enforcer implements the
	// orchestrator's EnforcerCheck seam (pre-dispatch drift check) and runs the
	// scheduled GC cron. Rule checks run in the workspace root via a dir-pinned
	// command factory. Skipped when enforcement is disabled so the orchestrator
	// behaves exactly as before this phase.
	if cfg.Enforcement.Enabled {
		enforcer, gcStop := wireEnforcer(ctx, rctx, cfg, configFn, writer)
		if gcStop != nil {
			defer gcStop()
		}
		orchOpts = append(orchOpts, orchestrator.WithEnforcer(enforcerSeam{enforcer}))
	}

	o := orchestrator.New(orchOpts...)

	rctx.log.Info().Msg("orchestrator started")
	if err := o.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("start: orchestrator: %w", err)
	}
	rctx.log.Info().Msg("orchestrator stopped")
	return nil
}

// wireKnowledge constructs the Knowledge Engine (Phase 10) and reports its
// knowledge_index_status (SPEC §4.1.11). When knowledge.enabled is false it is a
// no-op returning the disabled status, leaving startup behavior unchanged. When
// enabled it opens the store, indexes the workspace repos if index_on_startup is
// set, and starts the fsnotify watcher if watch_for_changes is set.
func wireKnowledge(
	ctx context.Context, rctx *rootContext, cfg config.Config, writer *audit.Writer,
) knowledge.IndexStatus {
	if !cfg.Knowledge.Enabled {
		return knowledge.StatusDisabled
	}
	store, err := knowledge.OpenStore(ctx, cfg.Knowledge, rctx.log)
	if err != nil {
		rctx.log.Warn().Err(err).Msg("knowledge engine disabled: store open failed")
		return knowledge.StatusDisabled
	}

	eng := knowledge.New(cfg.Knowledge, cfg.Project.ID,
		knowledge.WithStore(store),
		knowledge.WithAudit(writer),
		knowledge.WithLogger(rctx.log),
	)

	roots := workspaceRoots(cfg)
	if cfg.Knowledge.IndexOnStartup {
		if _, err := eng.Index(ctx, roots); err != nil {
			rctx.log.Warn().Err(err).Msg("knowledge startup index failed")
		}
	}
	if cfg.Knowledge.WatchForChanges {
		w := knowledge.NewWatcher(eng, roots, knowledge.WithWatchLogger(rctx.log))
		go func() {
			if err := w.Start(ctx); err != nil {
				rctx.log.Error().Err(err).Msg("knowledge watcher exited with error")
			}
		}()
	}
	return eng.Status()
}

// enforcerSeam adapts the harness Enforcer to the orchestrator's EnforcerCheck
// seam, mapping harness.EnforcerStatus onto orchestrator.EnforcerStatus (the two
// share string values; the adapter avoids harness importing the orchestrator).
type enforcerSeam struct {
	e *harness.Enforcer
}

// PreDispatch runs the harness pre-dispatch check and maps its status.
func (s enforcerSeam) PreDispatch(ctx context.Context) (orchestrator.EnforcerStatus, error) {
	st, err := s.e.PreDispatch(ctx)
	return orchestrator.EnforcerStatus(string(st)), err
}

// wireEnforcer constructs the Harness Enforcer and starts its scheduled GC cron.
// Rule checks run in the workspace root via a dir-pinned command factory. The
// returned stop func stops the GC scheduler; it is nil when no GC was scheduled.
//
// TODO(phase-12): wire a TrackerIssuer (GC issue create + dedup) and a
// LayerChecker (knowledge.CheckLayerViolations) adapter. Both require either a
// high-level tracker create-issue helper or returning the knowledge Engine from
// wireKnowledge; until then GC runs the rules but creates no issues and layer
// translation is exercised via unit tests + `conductor harness check`.
func wireEnforcer(
	ctx context.Context, rctx *rootContext, cfg config.Config,
	configFn func() config.Config, writer *audit.Writer,
) (*harness.Enforcer, func()) {
	root := cfg.Workspace.Root
	if root == "" {
		root = "."
	}
	runner := harness.NewRunner(
		harness.NewDirCommandFactory(root),
		harness.WithRunnerAudit(writer),
		harness.WithRunnerLogger(rctx.log),
		harness.WithRunnerProjectID(cfg.Project.ID),
	)
	enforcer := harness.NewEnforcer(runner, configFn,
		harness.WithEnforcerAudit(writer),
		harness.WithEnforcerLogger(rctx.log),
		harness.WithEnforcerProjectID(cfg.Project.ID),
	)

	rctx.log.Info().
		Bool("drift_check_on_dispatch", cfg.Enforcement.DriftCheckOnDispatch).
		Str("gc_schedule_cron", cfg.Enforcement.GCScheduleCron).
		Msg("harness enforcer ready")

	c := cron.New()
	if err := enforcer.StartGC(ctx, c); err != nil {
		rctx.log.Warn().Err(err).Msg("harness enforcer: gc schedule failed")
		return enforcer, nil
	}
	return enforcer, func() { c.Stop() }
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
