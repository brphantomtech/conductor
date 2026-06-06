package provider

import (
	"context"
	"net/http"
	"time"

	"github.com/rs/zerolog"

	"github.com/conductor-sh/conductor/internal/config"
)

// Adapter is the SPEC §7.1 surface every concrete provider implements.
// The orchestrator (Phase 6), router (Phase 7), and memory consolidator
// (Phase 9) hold an Adapter value and never touch a provider package
// directly. Phase 18 will expose this interface as a plugin extension
// point per SPEC §20.2 — until then, the supported implementations live
// inside this package.
type Adapter interface {
	// CreateSession initializes per-issue conversation state for the
	// given workspace. The returned Session is opaque to callers.
	CreateSession(ctx context.Context, cfg config.ProviderConfig, workspace string) (*Session, error)

	// StartTurn opens a new agent turn on the session. The tools slice
	// is the union of Conductor built-in tools (Phase 13) and any
	// per-pipeline plugin tools (Phase 18); Phase 3 accepts the slice
	// for forward compatibility but does not yet inject Conductor tools.
	StartTurn(ctx context.Context, s *Session, prompt string, tools []ToolSpec) (TurnStream, error)

	// ContinueTurn extends the same session thread with another user
	// prompt. Distinct from StartTurn so adapters that maintain a
	// separate thread / cache surface (e.g., Anthropic prompt cache)
	// have a hook.
	ContinueTurn(ctx context.Context, s *Session, prompt string) (TurnStream, error)

	// ContinueWithToolResults continues the same session with the results of
	// one or more tool calls the model emitted on the prior turn (SPEC §7.1,
	// §7.3). The adapter renders the results in its native shape (Anthropic
	// `tool_result` content blocks; OpenAI-compatible `role: "tool"` messages)
	// and returns a TurnStream for the model's follow-up turn. It returns
	// ErrNoToolCall when the session has not emitted a tool call.
	ContinueWithToolResults(ctx context.Context, s *Session, results []ToolResult) (TurnStream, error)

	// EndSession releases per-session state. Subsequent StartTurn /
	// ContinueTurn calls against the same Session return an error.
	EndSession(ctx context.Context, s *Session) error

	// GetUsage returns the cumulative TokenUsage for the session.
	GetUsage(s *Session) TokenUsage
}

// options captures the shared dependencies every adapter constructor
// accepts. Test code injects fakes; production code accepts the defaults.
type options struct {
	httpClient *http.Client
	logger     zerolog.Logger
	clock      func() time.Time
	summarizer summarizer
}

// defaultOptions returns the production defaults. The HTTP client has no
// overall timeout — streaming turns may run for many minutes — so the
// caller's context deadline is the only stop signal.
func defaultOptions() options {
	return options{
		httpClient: &http.Client{},
		logger:     zerolog.Nop(),
		clock:      time.Now,
	}
}

// Option is the functional-options handle for Adapter construction.
type Option func(*options)

// WithHTTPClient overrides the default HTTP client. Tests use
// httptest.Server.Client() to talk to a local fixture server.
func WithHTTPClient(c *http.Client) Option {
	return func(o *options) {
		if c != nil {
			o.httpClient = c
		}
	}
}

// WithLogger overrides the default no-op logger. Production callers pass
// the per-issue logger so streaming faults are correlated with the run
// attempt that triggered them.
func WithLogger(l zerolog.Logger) Option {
	return func(o *options) {
		o.logger = l
	}
}

// WithClock overrides time.Now. Tests use a fixed clock so timing-derived
// fields (the session ID derives elsewhere) stay deterministic.
func WithClock(f func() time.Time) Option {
	return func(o *options) {
		if f != nil {
			o.clock = f
		}
	}
}

// WithSummarizer overrides the default provider-call summarizer used by the
// compaction_strategy: summarize path (SPEC §7.4). Tests inject a
// deterministic summarizer so compaction never makes a live API call.
func WithSummarizer(s summarizer) Option {
	return func(o *options) {
		if s != nil {
			o.summarizer = s
		}
	}
}

// applyOptions overlays an Option list onto the defaults.
func applyOptions(opts []Option) options {
	out := defaultOptions()
	for _, opt := range opts {
		if opt != nil {
			opt(&out)
		}
	}
	return out
}
