package tracker

import (
	"fmt"

	"github.com/conductor-sh/conductor/internal/config"
)

// Supported tracker kinds. The list mirrors SPEC §5.3.2 and feeds both
// the New factory and Validate. Phase 4 wires two concrete adapters
// (linear, github); the other three (jira, plane, shortcut) are valid
// in HARNESS.md config but their constructors return ErrUnsupportedKind
// until their phases land. Adding them here without an adapter would
// break the spec's "supported list" guarantee, so they are gated on a
// real implementation.
const (
	KindLinear   = "linear"
	KindGitHub   = "github"
	KindJira     = "jira"
	KindPlane    = "plane"
	KindShortcut = "shortcut"
)

// supportedKinds is the SPEC §5.3.2 union; populated for use by both
// Validate (rule 1) and New (the deferred-kind branch).
var supportedKinds = map[string]struct{}{
	KindLinear:   {},
	KindGitHub:   {},
	KindJira:     {},
	KindPlane:    {},
	KindShortcut: {},
}

// New constructs the concrete Adapter for the given tracker kind.
// Returns ErrUnsupportedKind for kinds that have no Phase-4 adapter
// (jira / plane / shortcut) or unknown values. The variadic opts
// override the default HTTP client, logger, and clock.
func New(cfg config.Tracker, opts ...Option) (Adapter, error) {
	o := applyOptions(opts)
	switch cfg.Kind {
	case KindLinear:
		return newLinearAdapter(cfg, o)
	case KindGitHub:
		return newGithubAdapter(cfg, o)
	case KindJira, KindPlane, KindShortcut:
		return nil, fmt.Errorf("tracker: new %q: adapter not yet implemented: %w",
			cfg.Kind, ErrUnsupportedKind)
	default:
		return nil, fmt.Errorf("tracker: new %q: %w", cfg.Kind, ErrUnsupportedKind)
	}
}
