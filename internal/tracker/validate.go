package tracker

import (
	"errors"
	"fmt"

	"github.com/rs/zerolog"

	"github.com/conductor-sh/conductor/internal/config"
)

// trackerRequiresProjectSlug lists kinds whose project reference is the
// slug rather than the numeric/string id.
var trackerRequiresProjectSlug = map[string]struct{}{
	KindLinear: {},
	KindPlane:  {},
}

// trackerRequiresProjectID lists kinds whose project reference is the
// id (GitHub owner/repo, Jira project key, Shortcut project id).
var trackerRequiresProjectID = map[string]struct{}{
	KindGitHub:   {},
	KindJira:     {},
	KindShortcut: {},
}

// Validate enforces SPEC §6.4 startup rules for the tracker block.
// Returns errors.Join over every problem so the harness CLI can show
// all failures at once.
//
// The opts variadic exists so callers (Phase 6 orchestrator startup)
// can pass a logger; Validate emits warnings for non-fatal issues like
// an empty active_states slice. Passing no opts yields a no-op logger.
func Validate(cfg config.Tracker, opts ...Option) error {
	o := applyOptions(opts)
	var errs []error

	switch {
	case cfg.Kind == "":
		errs = append(errs, fmt.Errorf("tracker: kind is required: %w", ErrUnsupportedKind))
	default:
		if _, ok := supportedKinds[cfg.Kind]; !ok {
			errs = append(errs, fmt.Errorf("tracker: unsupported kind %q: %w",
				cfg.Kind, ErrUnsupportedKind))
		}
	}

	if cfg.APIKey == "" {
		errs = append(errs, fmt.Errorf("tracker: api_key is required for %q: %w",
			cfg.Kind, ErrMissingAPIKey))
	}

	if cfg.Kind != "" {
		if _, ok := trackerRequiresProjectSlug[cfg.Kind]; ok {
			if cfg.ProjectSlug == "" {
				errs = append(errs, fmt.Errorf("tracker: %q requires project_slug: %w",
					cfg.Kind, ErrMissingProjectID))
			}
		}
		if _, ok := trackerRequiresProjectID[cfg.Kind]; ok {
			if cfg.ProjectID == "" {
				errs = append(errs, fmt.Errorf("tracker: %q requires project_id: %w",
					cfg.Kind, ErrMissingProjectID))
			}
		}
	}

	// Non-fatal warning: an empty active_states slice (after the
	// config defaults apply) means "no filter" to the adapters. The
	// defaults in config/defaults.go populate ["Todo", "In Progress"],
	// so the only way to land here is a HARNESS.md that explicitly
	// clears the slice. Warn the operator without failing startup.
	if len(cfg.ActiveStates) == 0 {
		warnEmptyActiveStates(o.logger, cfg.Kind)
	}

	return errors.Join(errs...)
}

func warnEmptyActiveStates(log zerolog.Logger, kind string) {
	log.Warn().
		Str("kind", kind).
		Str("key", "tracker.active_states_empty").
		Msg("tracker: active_states is empty; adapter will treat as 'no filter'")
}
