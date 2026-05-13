package harness

import (
	"errors"
	"fmt"

	"github.com/conductor-sh/conductor/internal/config"
	"github.com/conductor-sh/conductor/internal/provider"
	"github.com/conductor-sh/conductor/internal/tracker"
)

// supportedTrackerKinds mirrors config.supportedTrackerKind. Duplicated
// here so the validator can attribute each failure to a sentinel without
// stringly matching on the message produced by config.Validate.
var supportedTrackerKinds = map[string]bool{
	"linear":   true,
	"github":   true,
	"jira":     true,
	"plane":    true,
	"shortcut": true,
}

// trackerRequiresProjectRef maps each tracker kind to whether it needs
// project_id or project_slug. Kept in lockstep with
// config.requiresProjectID; keep both in sync if the SPEC adds trackers.
var trackerRequiresProjectRef = map[string]bool{
	"linear":   true,
	"github":   true,
	"jira":     true,
	"plane":    true,
	"shortcut": true,
}

var supportedProviders = map[string]bool{
	"openrouter": true,
	"anthropic":  true,
	"openai":     true,
	"ollama":     true,
	"lm_studio":  true,
	"custom":     true,
}

// providersNeedAPIKey lists providers that talk to a hosted endpoint and
// therefore require an api_key. Local providers (ollama, lm_studio) are
// permitted to leave it blank.
var providersNeedAPIKey = map[string]bool{
	"openrouter": true,
	"anthropic":  true,
	"openai":     true,
	"custom":     true,
}

var supportedKnowledgeBackends = map[string]bool{
	"sqlite_vec": true,
	"qdrant":     true,
}

var supportedMemoryBackends = map[string]bool{
	"sqlite":   true,
	"postgres": true,
}

var supportedDocBackends = map[string]bool{
	"local_fs":   true,
	"git_repo":   true,
	"s3":         true,
	"notion":     true,
	"confluence": true,
	"custom":     true,
}

// Validate runs SPEC §6.4 startup checks against a parsed Definition +
// merged Config, plus the cross-cutting "every pipeline role has a
// template" rule. Every detected problem is included in the returned
// joined error so the operator sees them all at once.
//
// Validate also compile-tests every prompt template — a syntax error in
// any role surfaces as ErrTemplateParse inside the joined error.
//
// A nil error means the harness is ready to dispatch; a non-nil error
// means startup must abort.
func Validate(def *Definition, cfg config.Config) error {
	var errs []error

	if def == nil {
		return fmt.Errorf("harness: Validate called with nil definition")
	}

	if cfg.Project.ID == "" {
		errs = append(errs, fmt.Errorf("%w: project.id is required", ErrProjectIDMissing))
	}

	if cfg.Tracker.Kind == "" {
		errs = append(errs, fmt.Errorf("%w: tracker.kind is required", ErrTrackerKindMissing))
	} else if !supportedTrackerKinds[cfg.Tracker.Kind] {
		errs = append(errs, fmt.Errorf("%w: tracker.kind %q is not supported", ErrTrackerKindUnsupported, cfg.Tracker.Kind))
	}

	if cfg.Tracker.APIKey == "" {
		errs = append(errs, fmt.Errorf("%w: tracker.api_key is required", ErrTrackerAPIKeyMissing))
	}

	if cfg.Tracker.Kind != "" && trackerRequiresProjectRef[cfg.Tracker.Kind] &&
		cfg.Tracker.ProjectID == "" && cfg.Tracker.ProjectSlug == "" {
		errs = append(errs, fmt.Errorf("%w: tracker.project_id or project_slug is required for kind %q", ErrTrackerProjectRefMissing, cfg.Tracker.Kind))
	}

	if cfg.Providers.Default.Provider == "" {
		errs = append(errs, fmt.Errorf("%w: providers.default.provider is required", ErrProviderUnsupported))
	} else if !supportedProviders[cfg.Providers.Default.Provider] {
		errs = append(errs, fmt.Errorf("%w: providers.default.provider %q is not supported", ErrProviderUnsupported, cfg.Providers.Default.Provider))
	} else if providersNeedAPIKey[cfg.Providers.Default.Provider] && cfg.Providers.Default.APIKey == "" {
		errs = append(errs, fmt.Errorf("%w: providers.default.api_key is required for provider %q", ErrProviderAPIKeyMissing, cfg.Providers.Default.Provider))
	}

	if cfg.Knowledge.StoreBackend != "" && !supportedKnowledgeBackends[cfg.Knowledge.StoreBackend] {
		errs = append(errs, fmt.Errorf("%w: knowledge.store_backend %q is not supported", ErrKnowledgeBackendUnsupported, cfg.Knowledge.StoreBackend))
	}

	if cfg.Memory.StoreBackend != "" && !supportedMemoryBackends[cfg.Memory.StoreBackend] {
		errs = append(errs, fmt.Errorf("%w: memory.store_backend %q is not supported", ErrMemoryBackendUnsupported, cfg.Memory.StoreBackend))
	}

	for i, store := range cfg.Docs.Stores {
		if store.Backend == "" || supportedDocBackends[store.Backend] {
			continue
		}
		errs = append(errs, fmt.Errorf("%w: docs.stores[%d].backend %q is not supported", ErrDocStoreBackendUnsupported, i, store.Backend))
	}

	// Delegate the SPEC §23.3 provider-block checks to the provider
	// package — it owns the supported-kinds list, the loopback-loopback
	// auth rules, the custom-requires-base_url rule, and the
	// compaction_strategy enum. The harness sentinels above are kept so
	// the existing CLI categorisation continues to work; this call
	// supplements them with the SPEC §23.3 classifications.
	if err := provider.Validate(cfg.Providers); err != nil {
		errs = append(errs, err)
	}

	// Delegate the SPEC §23.2 tracker-block checks to the tracker
	// package — it owns the supported-kinds list, the project_slug /
	// project_id rules per kind, and the empty-active_states warning.
	// The harness sentinels above (ErrTrackerKindMissing, etc.) are
	// kept so the existing CLI categorisation continues to work; this
	// call supplements them with the SPEC §23.2 classifications.
	if err := tracker.Validate(cfg.Tracker); err != nil {
		errs = append(errs, err)
	}

	// Pipeline role → template coverage.
	missing := collectMissingTemplates(def, cfg)
	for _, m := range missing {
		errs = append(errs, fmt.Errorf("%w: role %q (from %s) has no `## %s` section", ErrPipelineRoleMissingTemplate, m.role, m.origin, m.role))
	}

	// Compile-test every template. Catches syntax errors at validate
	// time so the operator sees them in one shot rather than waiting
	// for the first turn to render.
	renderer := NewRenderer()
	for role, src := range def.PromptTemplates {
		if _, err := renderer.Compile(role, src); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

// missingTemplate records the role + the routing location that named it.
// "origin" is one of `routing.pipeline` or `routing.rules[<i>].pipeline`.
type missingTemplate struct {
	role   string
	origin string
}

// collectMissingTemplates returns the (role, origin) pairs for every
// role mentioned in cfg.Routing that has no matching prompt template.
func collectMissingTemplates(def *Definition, cfg config.Config) []missingTemplate {
	var out []missingTemplate
	seen := map[string]bool{}

	check := func(role, origin string) {
		if role == "" {
			return
		}
		if _, ok := def.PromptTemplates[role]; ok {
			return
		}
		key := role + "@" + origin
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, missingTemplate{role: role, origin: origin})
	}

	for _, role := range cfg.Routing.Pipeline {
		check(role, "routing.pipeline")
	}
	for i, rule := range cfg.Routing.Rules {
		origin := fmt.Sprintf("routing.rules[%d].pipeline", i)
		for _, role := range rule.Pipeline {
			check(role, origin)
		}
	}
	return out
}
