package orchestrator

import (
	"context"
	"os/exec"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/conductor-sh/conductor/internal/audit"
	"github.com/conductor-sh/conductor/internal/config"
	"github.com/conductor-sh/conductor/internal/harness"
	"github.com/conductor-sh/conductor/internal/provider"
	"github.com/conductor-sh/conductor/internal/tracker"
	"github.com/conductor-sh/conductor/internal/workspace"
)

// coderRole is the single hardcoded pipeline role Phase 6 dispatches. Phase 7
// replaces this with router-selected pipelines.
const coderRole = "coder"

// defaultStallTimeout is the no-progress window after which reconciliation
// Part A terminates a worker (SPEC §13.5). The orchestrator config (SPEC
// §5.3) does not yet carry a dedicated field, so it is an Option with this
// default; tests inject a small value.
const defaultStallTimeout = 10 * time.Minute

// Tracker is the subset of tracker.Adapter the orchestrator consumes. The
// concrete adapters satisfy it directly; tests inject a fake.
type Tracker interface {
	FetchCandidateIssues(ctx context.Context) ([]tracker.Issue, error)
	FetchIssuesByStates(ctx context.Context, states []string) ([]tracker.Issue, error)
	FetchIssueStatesByIDs(ctx context.Context, ids []string) (map[string]string, error)
}

// Workspaces is the subset of *workspace.Manager the orchestrator consumes.
// Defining it as an interface keeps the dispatch path unit-testable without
// touching the filesystem.
type Workspaces interface {
	Create(ctx context.Context, issueID, issueIdentifier string, hookEnv map[string]string) (*workspace.Workspace, error)
	Resolve(issueID, issueIdentifier string) (*workspace.Workspace, error)
	Remove(ctx context.Context, ws *workspace.Workspace, hookEnv map[string]string) error
	AgentCommand(ctx context.Context, ws *workspace.Workspace, name string, args ...string) (*exec.Cmd, error)
}

// Provider is the subset of provider.Adapter the orchestrator consumes to run
// a single agent turn.
type Provider interface {
	CreateSession(ctx context.Context, cfg config.ProviderConfig, workspace string) (*provider.Session, error)
	StartTurn(
		ctx context.Context, s *provider.Session, prompt string, tools []provider.ToolSpec,
	) (provider.TurnStream, error)
	EndSession(ctx context.Context, s *provider.Session) error
}

// renderFunc renders a Liquid prompt template against the SPEC §16.2
// variables. It defaults to harness.Render; tests override it to force a
// render failure.
type renderFunc func(source string, vars map[string]any) (string, error)

// Orchestrator is the Tier-4 coordinator that owns the poll loop and the
// single authoritative runtime state (SPEC §13). It is constructed once and
// run via Run; RuntimeState exposes a read-only snapshot for observability.
type Orchestrator struct {
	tracker     Tracker
	workspaces  Workspaces
	provider    Provider
	providerCfg config.ProviderConfig
	audit       *audit.Writer
	configFn    func() config.Config
	templateFn  func() map[string]string
	render      renderFunc
	clock       func() time.Time
	log         zerolog.Logger

	stallTimeout time.Duration

	// Poll-loop seams (SPEC §13.2), no-op by default.
	enforcer   EnforcerCheck
	preflight  PreflightCheck
	docSync    DocStoreSync
	classifier Classifier
	memoryPost MemoryPostProcessor

	// router is the Phase 7 Agent Router (SPEC §12). When nil, dispatch uses
	// the Phase 6 single-coder turn path. When wired, dispatch selects the
	// pipeline and drives RunPipeline through it.
	router PipelineRouter

	store *RuntimeStore

	// runCtx is the long-lived base context worker goroutines derive from so
	// a per-run cancellation (stall, reconciliation) survives across ticks.
	// Run sets it to its cancellable context; otherwise it is Background.
	runCtx context.Context

	// sem bounds concurrent workers (SPEC §14.3). Sized from the initial
	// max_concurrent_agents; per-tick selection enforces the dynamic cap.
	sem chan struct{}

	// workers tracks in-flight worker cancel funcs + progress for stall
	// detection. Guarded by its own mutex, separate from the runtime store.
	wmu     sync.Mutex
	workers map[string]*worker

	// wg tracks in-flight worker goroutines so Run can drain on shutdown.
	wg sync.WaitGroup
}

// worker is the per-run bookkeeping reconciliation needs: the cancel func to
// terminate the run, the last-progress timestamp for stall detection, and the
// reason a cancellation was initiated (so the worker classifies its own
// terminal outcome correctly).
type worker struct {
	cancel       context.CancelFunc
	startedAt    time.Time
	lastProgress time.Time
	cancelReason string
}

// Option configures an Orchestrator at construction.
type Option func(*Orchestrator)

// WithTracker injects the tracker adapter.
func WithTracker(t Tracker) Option { return func(o *Orchestrator) { o.tracker = t } }

// WithWorkspaces injects the workspace manager.
func WithWorkspaces(w Workspaces) Option { return func(o *Orchestrator) { o.workspaces = w } }

// WithProvider injects the provider adapter and the resolved provider config
// used to open sessions for the coder role.
func WithProvider(p Provider, cfg config.ProviderConfig) Option {
	return func(o *Orchestrator) {
		o.provider = p
		o.providerCfg = cfg
	}
}

// WithAudit wires the audit writer. Without it, lifecycle events are dropped.
func WithAudit(w *audit.Writer) Option { return func(o *Orchestrator) { o.audit = w } }

// WithConfig sets the config accessor read fresh each tick (hot-reload aware).
func WithConfig(fn func() config.Config) Option {
	return func(o *Orchestrator) {
		if fn != nil {
			o.configFn = fn
		}
	}
}

// WithTemplates sets the prompt-template accessor (role → source) read fresh
// each dispatch so a hot-reloaded HARNESS.md body takes effect.
func WithTemplates(fn func() map[string]string) Option {
	return func(o *Orchestrator) {
		if fn != nil {
			o.templateFn = fn
		}
	}
}

// WithRenderer overrides the prompt renderer (test seam).
func WithRenderer(fn renderFunc) Option {
	return func(o *Orchestrator) {
		if fn != nil {
			o.render = fn
		}
	}
}

// WithClock overrides the time source. Tests inject a controllable clock.
func WithClock(fn func() time.Time) Option {
	return func(o *Orchestrator) {
		if fn != nil {
			o.clock = fn
		}
	}
}

// WithLogger sets the structured logger.
func WithLogger(l zerolog.Logger) Option { return func(o *Orchestrator) { o.log = l } }

// WithStallTimeout overrides the no-progress stall window (SPEC §13.5).
func WithStallTimeout(d time.Duration) Option {
	return func(o *Orchestrator) {
		if d > 0 {
			o.stallTimeout = d
		}
	}
}

// WithEnforcer wires the Phase 12 enforcer pre-dispatch check (seam).
func WithEnforcer(e EnforcerCheck) Option {
	return func(o *Orchestrator) {
		if e != nil {
			o.enforcer = e
		}
	}
}

// WithPreflight wires the dispatch preflight check (seam).
func WithPreflight(p PreflightCheck) Option {
	return func(o *Orchestrator) {
		if p != nil {
			o.preflight = p
		}
	}
}

// WithDocStoreSync wires the Phase 11 doc-store sync step (seam).
func WithDocStoreSync(d DocStoreSync) Option {
	return func(o *Orchestrator) {
		if d != nil {
			o.docSync = d
		}
	}
}

// WithClassifier wires the Phase 7 classification step (seam).
func WithClassifier(c Classifier) Option {
	return func(o *Orchestrator) {
		if c != nil {
			o.classifier = c
		}
	}
}

// WithPipelineRouter wires the Phase 7 Agent Router so dispatch selects the
// pipeline and drives RunPipeline through it (SPEC §12).
func WithPipelineRouter(r PipelineRouter) Option {
	return func(o *Orchestrator) {
		if r != nil {
			o.router = r
		}
	}
}

// WithMemoryPostProcessor wires the Phase 9 reconciliation Part C step (seam).
func WithMemoryPostProcessor(m MemoryPostProcessor) Option {
	return func(o *Orchestrator) {
		if m != nil {
			o.memoryPost = m
		}
	}
}

// New constructs an Orchestrator. Collaborators (tracker, workspaces,
// provider) are normally supplied via Options; a defaults-only Orchestrator is
// valid for tests that only exercise the runtime state.
func New(opts ...Option) *Orchestrator {
	o := &Orchestrator{
		configFn:     func() config.Config { return config.Defaults() },
		templateFn:   func() map[string]string { return map[string]string{} },
		render:       harness.Render,
		clock:        time.Now,
		log:          zerolog.Nop(),
		stallTimeout: defaultStallTimeout,
		enforcer:     noopEnforcer{},
		preflight:    noopPreflight{},
		docSync:      noopDocStoreSync{},
		classifier:   noopClassifier{},
		memoryPost:   noopMemoryPostProcessor{},
		store:        newRuntimeStore(),
		workers:      map[string]*worker{},
		runCtx:       context.Background(),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(o)
		}
	}
	maxC := o.configFn().Agent.MaxConcurrentAgents
	if maxC <= 0 {
		maxC = 1
	}
	o.sem = make(chan struct{}, maxC)
	return o
}

// RuntimeState returns a deep-copied snapshot of the runtime state (SPEC
// §4.1.11). Mutating the returned value cannot affect the live state.
func (o *Orchestrator) RuntimeState() RuntimeState { return o.store.snapshot() }

// Wait blocks until all in-flight worker goroutines have finished. Used by
// Run for graceful shutdown and by tests to await dispatch completion.
func (o *Orchestrator) Wait() { o.wg.Wait() }

// runContext returns the long-lived base context worker goroutines derive
// from. It is never nil.
func (o *Orchestrator) runContext() context.Context {
	if o.runCtx == nil {
		return context.Background()
	}
	return o.runCtx
}

// selectionConfigFrom builds the per-tick selection config from a live config
// snapshot so hot-reloaded caps and state lists take effect immediately.
func selectionConfigFrom(cfg config.Config) selectionConfig {
	return selectionConfig{
		maxConcurrent:  cfg.Agent.MaxConcurrentAgents,
		maxByState:     cfg.Agent.MaxConcurrentAgentsByState,
		activeStates:   cfg.Tracker.ActiveStates,
		terminalStates: cfg.Tracker.TerminalStates,
	}
}
