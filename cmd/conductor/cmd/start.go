package cmd

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"sync"

	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/conductor-sh/conductor/internal/audit"
	"github.com/conductor-sh/conductor/internal/config"
	"github.com/conductor-sh/conductor/internal/db"
	"github.com/conductor-sh/conductor/internal/docstore"
	"github.com/conductor-sh/conductor/internal/harness"
	"github.com/conductor-sh/conductor/internal/knowledge"
	"github.com/conductor-sh/conductor/internal/memory"
	"github.com/conductor-sh/conductor/internal/orchestrator"
	"github.com/conductor-sh/conductor/internal/provider"
	"github.com/conductor-sh/conductor/internal/router"
	"github.com/conductor-sh/conductor/internal/tools"
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

	// Construct the Validation Pipeline runner (SPEC §15) that the Phase 7 router
	// invokes after each role's turn (SPEC §12.4 step 5). The router's Validator
	// seam is per-(workspace, role); validationRunner builds a workspace-pinned
	// pipeline per call and applies the fail_on_severity decision.
	validator := &validationRunner{
		cfg:       cfg.Validation,
		log:       rctx.log,
		audit:     writer,
		projectID: cfg.Project.ID,
		turns:     map[string]int{},
	}
	rctx.log.Info().
		Bool("validation_enabled", cfg.Validation.Enabled).
		Int("validation_checks", len(cfg.Validation.Checks)).
		Msg("validation pipeline ready")

	templates := map[string]string{}
	if def != nil {
		templates = def.PromptTemplates
	}

	// Construct and wire the Knowledge Engine (Phase 10). It is a standalone
	// background service: when enabled we open its store, optionally index on
	// startup, optionally start the incremental watcher, and report its
	// knowledge_index_status. The Phase 13 tool and the Phase 6 poll-loop seam
	// consume the engine later; here we own its lifecycle and log its status.
	knowledgeEngine, knowledgeStatus := wireKnowledge(ctx, rctx, cfg, writer)
	rctx.log.Info().Str("knowledge_index_status", string(knowledgeStatus)).Msg("knowledge engine status")

	configFn := func() config.Config { return cfg }
	templatesFn := func() map[string]string { return templates }

	// Wire the Memory Manager (Phase 9) ahead of the router so the Phase 13
	// conductor_memory_* tools can dispatch to it. It is also wired as
	// reconciliation Part C below. nil when memory is disabled.
	memoryManager, memCleanup, mErr := wireMemory(ctx, rctx, cfg, writer)
	if mErr != nil {
		return fmt.Errorf("start: construct memory manager: %w", mErr)
	}
	if memCleanup != nil {
		defer memCleanup()
	}

	// Build the Phase 13 tool registry (SPEC §7.3) from the wired engines and a
	// dispatcher that emits redacted ToolCalled/ToolResult audit events. The
	// router advertises these tools on every session and drives the dispatch
	// loop. With no engines enabled the built-ins are still advertised but return
	// unavailable results, keeping the tool surface stable.
	toolRegistry := tools.NewRegistry()
	toolRegistry.RegisterBuiltins(buildToolEngines(cfg, toolEngines{
		tracker:    trackerAdapter,
		knowledge:  knowledgeEngine,
		memory:     memoryManager,
		validation: cfg.Validation,
	}))
	toolDispatcher := tools.NewDispatcher(toolRegistry,
		tools.WithAudit(writer),
		tools.WithLogger(rctx.log),
	)
	rctx.log.Info().Int("tools", toolRegistry.Len()).Msg("tool registry ready")

	// Construct the Phase 7 Agent Router (SPEC §12). It implements the
	// orchestrator's classification seam and drives router-selected pipelines.
	// Per-role provider resolution is config-driven; the single adapter serves
	// every role's ProviderConfig (multi-kind adapter routing lands later). The
	// validation runner is wired as the SPEC §12.4 step-5 Validator; the tool
	// registry + dispatcher drive the SPEC §7.3 tool-call loop.
	agentRouter := router.New(
		router.WithProvider(providerAdapter),
		router.WithTracker(trackerAdapter),
		router.WithValidator(validator),
		router.WithTools(toolRegistry, toolDispatcher),
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

	// Wire the Doc Store Manager (Phase 11) as the orchestrator's DocStoreSync
	// poll-loop seam (SPEC §10.3 step 4): each tick syncs pending stores,
	// indexing changed documents as doc nodes. Skipped when docs are disabled so
	// the poll loop behaves exactly as before this phase. Only local_fs stores
	// are constructed here; git_repo/s3 need injected clients (SPEC §10.2).
	if cfg.Docs.Enabled {
		docMgr, dErr := wireDocStore(ctx, rctx, cfg, writer)
		if dErr != nil {
			return fmt.Errorf("start: construct doc store manager: %w", dErr)
		}
		if docMgr != nil {
			orchOpts = append(orchOpts, orchestrator.WithDocStoreSync(docMgr))
		}
	}

	// Wire the Memory Manager as reconciliation Part C (SPEC §13.5): each
	// terminal run writes a session-end episodic memory. The manager was
	// constructed above (shared with the Phase 13 tools); nil when disabled.
	if memoryManager != nil {
		orchOpts = append(orchOpts, orchestrator.WithMemoryPostProcessor(memory.NewPostProcessor(memoryManager)))
	}

	// Wire the Phase 12 Harness Enforcer (SPEC §11). The enforcer implements the
	// orchestrator's EnforcerCheck seam (pre-dispatch drift check) and runs the
	// scheduled GC cron. Rule checks run in the workspace root via a dir-pinned
	// command factory. Skipped when enforcement is disabled so the orchestrator
	// behaves exactly as before this phase.
	if cfg.Enforcement.Enabled {
		var layers harness.LayerChecker
		if knowledgeEngine != nil {
			layers = knowledgeLayerChecker{eng: knowledgeEngine, configFn: configFn}
		}
		enforcer, gcStop := wireEnforcer(ctx, rctx, cfg, configFn, writer, layers, trackerAdapter)
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
// knowledge_index_status (SPEC §4.1.11) along with the engine itself (nil when
// disabled) so the Harness Enforcer can consume its layer-violation output. When
// knowledge.enabled is false it is a no-op returning (nil, disabled), leaving
// startup behavior unchanged. When enabled it opens the store, indexes the
// workspace repos if index_on_startup is set, and starts the fsnotify watcher if
// watch_for_changes is set.
func wireKnowledge(
	ctx context.Context, rctx *rootContext, cfg config.Config, writer *audit.Writer,
) (*knowledge.Engine, knowledge.IndexStatus) {
	if !cfg.Knowledge.Enabled {
		return nil, knowledge.StatusDisabled
	}
	store, err := knowledge.OpenStore(ctx, cfg.Knowledge, rctx.log)
	if err != nil {
		rctx.log.Warn().Err(err).Msg("knowledge engine disabled: store open failed")
		return nil, knowledge.StatusDisabled
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
	return eng, eng.Status()
}

// wireMemory constructs the Memory Manager (Phase 9) when memory is enabled,
// returning the manager (nil when disabled), a cleanup func that closes it (nil
// when disabled), and any construction error. It is wired once and shared by the
// Phase 13 tools and the reconciliation Part-C post-processor.
func wireMemory(
	ctx context.Context, rctx *rootContext, cfg config.Config, writer *audit.Writer,
) (*memory.Manager, func(), error) {
	if !cfg.Memory.Enabled {
		return nil, nil, nil
	}
	consolidationCfg := resolveProviderConfig(cfg, cfg.Memory.ConsolidationProvider)
	embedder := memory.NewAPIProvider(consolidationCfg, nil)
	mgr, err := memory.New(ctx, cfg.Memory,
		memory.WithProjectID(cfg.Project.ID),
		memory.WithAudit(writer),
		memory.WithLogger(rctx.log),
		memory.WithEmbedder(embedder),
		memory.WithSynthesizer(embedder),
	)
	if err != nil {
		return nil, nil, err
	}
	return mgr, func() { _ = mgr.Close() }, nil
}

// knowledgeLayerChecker adapts the Knowledge Engine to the harness Enforcer's
// LayerChecker seam (SPEC §8.6 / §11.4), mapping knowledge.LayerViolation onto
// harness.LayerViolationInput so harness does not import the knowledge package.
type knowledgeLayerChecker struct {
	eng      *knowledge.Engine
	configFn func() config.Config
}

// LayerViolations runs the Knowledge Engine's cross-layer dependency check and
// translates the results into the enforcer's input shape.
func (k knowledgeLayerChecker) LayerViolations(ctx context.Context) ([]harness.LayerViolationInput, error) {
	vs, err := k.eng.CheckLayerViolations(ctx, k.configFn().HarnessRules)
	if err != nil {
		return nil, err
	}
	out := make([]harness.LayerViolationInput, 0, len(vs))
	for _, v := range vs {
		out = append(out, harness.LayerViolationInput{
			FromPath:  v.FromPath,
			FromLayer: v.FromLayer,
			ToPath:    v.ToPath,
			ToLayer:   v.ToLayer,
		})
	}
	return out, nil
}

// wireDocStore constructs the Doc Store Manager (Phase 11) backed by the
// Knowledge Engine store so synced documents are indexed as doc nodes. It
// returns nil (and no error) when no usable store could be opened, so the
// orchestrator runs without the seam rather than failing startup. The Knowledge
// store the manager indexes into is opened independently of wireKnowledge's
// engine; both target the same configured store_backend.
func wireDocStore(
	ctx context.Context, rctx *rootContext, cfg config.Config, writer *audit.Writer,
) (*docstore.Manager, error) {
	store, err := knowledge.OpenStore(ctx, cfg.Knowledge, rctx.log)
	if err != nil {
		rctx.log.Warn().Err(err).Msg("doc store manager: knowledge store open failed; docs not indexed")
		store = nil
	}

	opts := []docstore.Option{
		docstore.WithProjectID(cfg.Project.ID),
		docstore.WithAudit(writer),
		docstore.WithLogger(rctx.log),
	}
	if store != nil {
		opts = append(opts,
			docstore.WithKnowledgeStore(store),
			docstore.WithEmbedder(docstore.NewHashEmbedder(docstore.DefaultEmbedDim)),
		)
	}
	mgr, err := docstore.New(cfg.Docs, cfg.Project.ID, opts...)
	if err != nil {
		if store != nil {
			_ = store.Close()
		}
		return nil, err
	}
	if err := mgr.Hydrate(ctx); err != nil {
		rctx.log.Warn().Err(err).Msg("doc store manager: hydrate failed; first sync re-indexes all docs")
	}
	rctx.log.Info().Int("doc_stores", len(cfg.Docs.Stores)).Msg("doc store manager ready")
	return mgr, nil
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
// layers argument (nil when knowledge is disabled) supplies the SPEC §8.6 / §11.4
// layer-violation translation. The returned stop func stops the GC scheduler; it
// is nil when no GC was scheduled.
//
// The Phase 12 GC TrackerIssuer (GC issue create + dedup) is wired here over the
// tracker adapter's write surface — the same surface the Phase 13
// conductor_tracker_mutate tool uses — closing the deferred follow-up. GitHub is
// fully implemented; for trackers without an issuer (see newTrackerIssuer) GC
// runs the rules but creates no issues.
func wireEnforcer(
	ctx context.Context, rctx *rootContext, cfg config.Config,
	configFn func() config.Config, writer *audit.Writer, layers harness.LayerChecker,
	trackerAdapter tracker.Adapter,
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
	enforcerOpts := []harness.EnforcerOption{
		harness.WithEnforcerAudit(writer),
		harness.WithEnforcerLogger(rctx.log),
		harness.WithEnforcerProjectID(cfg.Project.ID),
	}
	if layers != nil {
		enforcerOpts = append(enforcerOpts, harness.WithEnforcerLayers(layers))
	}
	if issuer := newTrackerIssuer(cfg, trackerAdapter); issuer != nil {
		enforcerOpts = append(enforcerOpts, harness.WithEnforcerTracker(issuer))
		rctx.log.Info().Str("tracker_kind", cfg.Tracker.Kind).Msg("harness gc tracker issuer wired")
	} else {
		rctx.log.Info().Str("tracker_kind", cfg.Tracker.Kind).
			Msg("harness gc tracker issuer not implemented for this tracker; gc creates no issues")
	}
	enforcer := harness.NewEnforcer(runner, configFn, enforcerOpts...)

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

// validationRunner adapts the Validation Pipeline (SPEC §15) to the router's
// per-(workspace, role) Validator seam (SPEC §12.4 step 5). It builds a
// workspace-pinned pipeline per call, persists results under the workspace's
// .conductor/validation/<turn_index>.json with a monotonic per-workspace index,
// and returns an error (failing the attempt) when the result trips the
// configured fail_on_severity threshold.
type validationRunner struct {
	cfg       config.Validation
	log       zerolog.Logger
	audit     *audit.Writer
	projectID string

	mu    sync.Mutex
	turns map[string]int
}

// Run executes the configured checks in the workspace after a role's turn.
func (v *validationRunner) Run(ctx context.Context, workspacePath, role string) error {
	if !v.cfg.Enabled || !v.cfg.RunAfterTurn {
		return nil
	}

	v.mu.Lock()
	idx := v.turns[workspacePath]
	v.turns[workspacePath] = idx + 1
	v.mu.Unlock()

	pipeline := validation.New(v.cfg,
		validation.WithLogger(v.log),
		validation.WithAudit(v.audit),
		validation.WithProjectID(v.projectID),
		validation.WithCommandFactory(validation.NewDirCommandFactory(workspacePath)),
	)
	dir := filepath.Join(workspacePath, ".conductor", "validation")
	res, err := pipeline.Run(ctx, dir, idx)
	if err != nil {
		return fmt.Errorf("validation run (role %s): %w", role, err)
	}
	if failed, reason := res.FailTurn(v.cfg.FailOnSeverity); failed {
		return fmt.Errorf("validation failed after role %s: %s", role, reason)
	}
	return nil
}
