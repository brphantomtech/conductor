package tracker

import (
	"context"
	"net/http"
	"time"

	"github.com/rs/zerolog"
)

// Adapter is the SPEC §20.1 surface every concrete tracker implements.
// The orchestrator (Phase 6) and the Phase-13 tool dispatcher are the
// only callers; pipeline / router code receives normalized Issue
// records and never reaches through to a tracker package directly.
// Phase 18 will expose this interface as a plugin extension point per
// SPEC §20.1 — until then, the supported implementations live inside
// this package.
type Adapter interface {
	// FetchCandidateIssues returns issues in the configured
	// active_states. The adapter pages internally and returns at most
	// maxIssuesPerCall records; reaching the cap is logged but is not
	// an error.
	FetchCandidateIssues(ctx context.Context) ([]Issue, error)

	// FetchIssuesByStates fetches issues whose state matches any of
	// the supplied names. Used by the orchestrator's reconciliation
	// pass (SPEC §13.5 Part B). An empty states slice returns no
	// issues without contacting the tracker.
	FetchIssuesByStates(ctx context.Context, states []string) ([]Issue, error)

	// FetchIssueStatesByIDs is a batched state-only lookup used by
	// reconciliation. Returns map keyed on the supplied IDs. IDs the
	// tracker does not recognize are omitted from the result, not
	// represented as an empty string.
	FetchIssueStatesByIDs(ctx context.Context, ids []string) (map[string]string, error)

	// ExecuteQuery proxies a read-only request to the tracker. For
	// GraphQL trackers (Linear) the `query` argument is a GraphQL
	// document and `variables` is the variables block; for REST
	// trackers (GitHub) `query` is the URL path and `variables` is
	// ignored. Phase 13's conductor_tracker_query tool wraps this.
	ExecuteQuery(ctx context.Context, query string, variables map[string]any) (map[string]any, error)

	// ExecuteMutation proxies a write request to the tracker. Same
	// argument conventions as ExecuteQuery. Phase 13's
	// conductor_tracker_mutate tool wraps this.
	ExecuteMutation(ctx context.Context, mutation string, variables map[string]any) (map[string]any, error)
}

// options captures the shared dependencies every adapter constructor
// accepts. Test code injects fakes; production code accepts the defaults.
type options struct {
	httpClient *http.Client
	logger     zerolog.Logger
	clock      func() time.Time
}

// defaultOptions returns the production defaults. Unlike the provider
// layer, tracker calls are unary request/response so a 30 s overall
// timeout on the HTTP client is the right ceiling.
func defaultOptions() options {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	return options{
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   30 * time.Second,
		},
		logger: zerolog.Nop(),
		clock:  time.Now,
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

// WithLogger overrides the default no-op logger. Production callers
// pass the per-issue logger so transport faults correlate with the
// poll tick that triggered them.
func WithLogger(l zerolog.Logger) Option {
	return func(o *options) {
		o.logger = l
	}
}

// WithClock overrides time.Now. Tests inject a deterministic clock so
// timestamp-derived fields stay stable.
func WithClock(f func() time.Time) Option {
	return func(o *options) {
		if f != nil {
			o.clock = f
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
