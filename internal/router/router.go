package router

import (
	"context"
	"strings"

	"github.com/rs/zerolog"

	"github.com/conductor-sh/conductor/internal/audit"
	"github.com/conductor-sh/conductor/internal/config"
	"github.com/conductor-sh/conductor/internal/harness"
	"github.com/conductor-sh/conductor/internal/provider"
	"github.com/conductor-sh/conductor/internal/tools"
	"github.com/conductor-sh/conductor/internal/tracker"
)

// validTaskTypes is the closed SPEC §12.2 task-type enum. A classification
// response that is not one of these collapses to unknown.
var validTaskTypes = map[string]struct{}{
	"feature":       {},
	"bug":           {},
	"refactor":      {},
	"investigation": {},
	"docs":          {},
	"gc_task":       {},
	"unknown":       {},
}

// taskTypeUnknown is the fallback task type used when classification fails or
// returns an unrecognized value (SPEC §12.2, design "never block dispatch").
const taskTypeUnknown = "unknown"

// Provider is the subset of provider.Adapter the router consumes to classify
// issues and run agent turns. The concrete adapters satisfy it directly;
// tests inject a fake.
type Provider interface {
	CreateSession(ctx context.Context, cfg config.ProviderConfig, workspace string) (*provider.Session, error)
	StartTurn(
		ctx context.Context, s *provider.Session, prompt string, tools []provider.ToolSpec,
	) (provider.TurnStream, error)
	ContinueTurn(ctx context.Context, s *provider.Session, prompt string) (provider.TurnStream, error)
	ContinueWithToolResults(
		ctx context.Context, s *provider.Session, results []provider.ToolResult,
	) (provider.TurnStream, error)
	EndSession(ctx context.Context, s *provider.Session) error
}

// ToolDispatcher is the subset of the tool-injection layer (Phase 13) the router
// drives in its tool-call loop. *tools.Dispatcher satisfies it; tests inject a
// fake. It is declared here (consumer side) so the router depends on the
// behavior, not the concrete dispatcher.
type ToolDispatcher interface {
	Dispatch(ctx context.Context, call tools.Call, policy tools.ApprovalPolicy, ec tools.ExecutionContext) tools.ToolResult
}

// ToolRegistry is the subset of the tool registry the router reads to advertise
// tools on StartTurn. *tools.Registry satisfies it.
type ToolRegistry interface {
	Specs() []provider.ToolSpec
}

// defaultMaxToolTurns bounds the per-role tool-call loop so a model that keeps
// calling tools cannot loop forever (design.md "Tool-turn cap"). SPEC §7.3 is
// silent on the value; 10 is a conservative default revisited if real runs need
// tuning.
const defaultMaxToolTurns = 10

// Tracker is the subset of the tracker adapter the router consumes to re-fetch
// issue state during continuation handling (SPEC §12.5).
type Tracker interface {
	FetchIssueStatesByIDs(ctx context.Context, ids []string) (map[string]string, error)
}

// Validator is the SPEC §12.4 step-5 Validation Pipeline seam (Phase 8). It is
// nil until Phase 8 is merged into this worktree; RunPipeline guards it with a
// nil check and skips validation when absent.
type Validator interface {
	// Run executes the configured validation checks against the workspace
	// after a role's turn. A non-nil error fails the attempt.
	Run(ctx context.Context, workspace, role string) error
}

// renderFunc renders a Liquid prompt template against the SPEC §16.2 variable
// set. It defaults to harness.Render; tests override it.
type renderFunc func(source string, vars map[string]any) (string, error)

// Router is the Agent Router (SPEC §12). It is constructed once with its
// collaborators and reused across dispatches; it holds no per-issue state.
type Router struct {
	provider     Provider
	tracker      Tracker
	validator    Validator
	toolRegistry ToolRegistry
	dispatcher   ToolDispatcher
	maxToolTurns int
	configFn     func() config.Config
	templateFn   func() map[string]string
	render       renderFunc
	audit        *audit.Writer
	log          zerolog.Logger
}

// Option configures a Router at construction.
type Option func(*Router)

// WithProvider injects the provider adapter used for classification and turns.
func WithProvider(p Provider) Option { return func(r *Router) { r.provider = p } }

// WithTracker injects the tracker adapter used for continuation re-fetch.
func WithTracker(t Tracker) Option { return func(r *Router) { r.tracker = t } }

// WithValidator wires the Phase 8 Validation Pipeline (SPEC §12.4 step 5).
// When unset, validation is skipped.
func WithValidator(v Validator) Option {
	return func(r *Router) {
		if v != nil {
			r.validator = v
		}
	}
}

// WithConfig sets the config accessor read fresh each dispatch (hot-reload).
func WithConfig(fn func() config.Config) Option {
	return func(r *Router) {
		if fn != nil {
			r.configFn = fn
		}
	}
}

// WithTemplates sets the role→source template accessor read fresh each
// dispatch so a hot-reloaded HARNESS.md body takes effect.
func WithTemplates(fn func() map[string]string) Option {
	return func(r *Router) {
		if fn != nil {
			r.templateFn = fn
		}
	}
}

// WithRenderer overrides the prompt renderer (test seam).
func WithRenderer(fn renderFunc) Option {
	return func(r *Router) {
		if fn != nil {
			r.render = fn
		}
	}
}

// WithTools wires the Phase 13 tool registry and dispatcher. When both are set,
// the router advertises the registry's tools on each role's StartTurn and drives
// the tool-call dispatch loop. When unset, turns run exactly as before (no tools
// advertised, no loop).
func WithTools(registry ToolRegistry, dispatcher ToolDispatcher) Option {
	return func(r *Router) {
		if registry != nil && dispatcher != nil {
			r.toolRegistry = registry
			r.dispatcher = dispatcher
		}
	}
}

// WithMaxToolTurns overrides the per-role tool-call loop cap (default 10). A
// non-positive value leaves the default in place.
func WithMaxToolTurns(n int) Option {
	return func(r *Router) {
		if n > 0 {
			r.maxToolTurns = n
		}
	}
}

// WithAudit wires the audit writer. Without it, router events are dropped.
func WithAudit(w *audit.Writer) Option { return func(r *Router) { r.audit = w } }

// WithLogger sets the structured logger.
func WithLogger(l zerolog.Logger) Option { return func(r *Router) { r.log = l } }

// New constructs a Router. Collaborators are normally supplied via Options; a
// defaults-only Router is valid for tests that exercise pure logic (selection,
// prompt assembly).
func New(opts ...Option) *Router {
	r := &Router{
		configFn:     func() config.Config { return config.Defaults() },
		templateFn:   func() map[string]string { return map[string]string{} },
		render:       harness.Render,
		maxToolTurns: defaultMaxToolTurns,
		log:          zerolog.Nop(),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(r)
		}
	}
	return r
}

// Classify implements the orchestrator's classification seam (SPEC §12.2). For
// each unclassified candidate it invokes the default provider with the
// classification prompt, sets task_type to a valid value, and records a
// classification audit event. Already-classified issues pass through
// untouched, and a provider failure defaults the issue to unknown so a flaky
// classifier never blocks dispatch.
func (r *Router) Classify(ctx context.Context, candidates []tracker.Issue) ([]tracker.Issue, error) {
	out := make([]tracker.Issue, len(candidates))
	copy(out, candidates)
	for i := range out {
		if out[i].TaskType != nil && *out[i].TaskType != "" {
			continue
		}
		tt := r.classifyOne(ctx, out[i])
		out[i].TaskType = &tt
		r.emitClassification(ctx, out[i], tt)
	}
	return out, nil
}

// classifyOne runs a single classification turn against the default provider
// and maps the response to a valid task type. Any error or unrecognized
// response yields taskTypeUnknown.
func (r *Router) classifyOne(ctx context.Context, iss tracker.Issue) string {
	if r.provider == nil {
		return taskTypeUnknown
	}
	cfg := r.configFn()
	prompt, err := r.render(classificationPrompt, classificationVars(iss))
	if err != nil {
		r.log.Warn().Err(err).Str("issue_identifier", iss.Identifier).
			Msg("classification prompt render failed")
		return taskTypeUnknown
	}

	sess, err := r.provider.CreateSession(ctx, cfg.Providers.Default, "")
	if err != nil {
		r.log.Warn().Err(err).Str("issue_identifier", iss.Identifier).
			Msg("classification session failed")
		return taskTypeUnknown
	}
	defer func() { _ = r.provider.EndSession(context.WithoutCancel(ctx), sess) }()

	stream, err := r.provider.StartTurn(ctx, sess, prompt, nil)
	if err != nil {
		r.log.Warn().Err(err).Str("issue_identifier", iss.Identifier).
			Msg("classification turn failed")
		return taskTypeUnknown
	}
	res := stream.Wait()
	if res.Err != nil {
		r.log.Warn().Err(res.Err).Str("issue_identifier", iss.Identifier).
			Msg("classification turn errored")
		return taskTypeUnknown
	}
	return normalizeTaskType(res.Text)
}

// normalizeTaskType reduces a free-form classification response to the closed
// SPEC §12.2 enum, defaulting to unknown for anything unrecognized.
func normalizeTaskType(raw string) string {
	tt := strings.ToLower(strings.TrimSpace(raw))
	// The prompt asks for the bare task type, but tolerate trailing
	// punctuation or a single trailing word.
	tt = strings.TrimRight(tt, ".\n\r\t ")
	if fields := strings.Fields(tt); len(fields) > 0 {
		tt = fields[0]
	}
	if _, ok := validTaskTypes[tt]; ok {
		return tt
	}
	return taskTypeUnknown
}

// emitClassification records the SPEC §12.2 step-3 classification audit event.
// The SPEC §17.2 registry has no dedicated classification type, so the router
// records it as an IssueClassified-payloaded event keyed to the issue; the
// task_type and a classification marker live in the payload for provenance.
func (r *Router) emitClassification(ctx context.Context, iss tracker.Issue, taskType string) {
	if r.audit == nil {
		return
	}
	evt := audit.AuditEvent{
		ProjectID: r.configFn().Project.ID,
		IssueID:   iss.ID,
		EventType: audit.EventIssueDispatched,
		Payload: map[string]any{
			"identifier":     iss.Identifier,
			"task_type":      taskType,
			"classification": true,
		},
	}
	if err := r.audit.Write(ctx, evt); err != nil {
		r.log.Warn().Err(err).Msg("classification audit write failed")
	}
}
