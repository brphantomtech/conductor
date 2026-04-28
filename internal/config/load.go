package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// envPrefix is the namespace under which all Conductor environment variables
// live, e.g. CONDUCTOR_SERVER_PORT, CONDUCTOR_LOG_LEVEL. The prefix keeps the
// host environment readable when the operator is debugging which variables
// belong to Conductor versus other tools.
const envPrefix = "CONDUCTOR"

// flagBindings declares the canonical mapping between the Cobra flag names
// the CLI exposes and the dot-separated config keys they bind to. Every
// flag the SPEC §19.2 surface defines is listed here so Load can wire them
// in one shot regardless of which subcommand owns them.
var flagBindings = map[string]string{
	"port":             "server.port",
	"no-dashboard":     "server.enable_dashboard",
	"no-api":           "server.enable_api",
	"log-level":        "log.level",
	"log-format":       "log.format",
	"audit-webhook":    "server.audit_webhook_url",
	"polling-interval": "polling.interval_ms",
}

// LoadOptions adjusts how Load assembles the Config. The zero value is the
// production default: read the process environment, expand $VAR references,
// and use the provided FlagSet for CLI flag overrides.
type LoadOptions struct {
	// Flags is the CLI flag set whose values should override env and YAML.
	// It may be nil — Load skips flag binding when no flag set is passed
	// (useful for offline subcommands and tests).
	Flags *pflag.FlagSet

	// FrontMatter is the parsed YAML front matter of HARNESS.md, already
	// decoded into a map. Phase 2 wires the harness loader; Phase 1 callers
	// pass nil.
	FrontMatter map[string]any

	// Lookup is the environment lookup function used during $VAR expansion
	// of selected string fields. nil falls back to os.LookupEnv.
	Lookup LookupFunc
}

// Load assembles a Config honoring SPEC §6.1 source precedence:
//
//  1. CLI flags          — highest priority
//  2. Environment vars   — CONDUCTOR_*
//  3. YAML front matter  — HARNESS.md
//  4. $VAR indirection   — only inside selected string fields
//  5. Built-in defaults  — lowest priority
//
// Step 4 is applied to the resolved value of path-like and credential
// fields after step 1–3 merge, since we need the final string before we
// can decide whether it contains a $VAR reference.
//
// Load does not validate semantic constraints (project.id non-empty,
// providers.default present, etc.) — that is the job of Validate (SPEC §6.4).
func Load(opts LoadOptions) (Config, error) {
	v := viper.New()
	v.SetEnvPrefix(envPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	v.AutomaticEnv()

	// 5. Built-in defaults (lowest priority).
	d := Defaults()
	if err := v.MergeConfigMap(structToMap(d)); err != nil {
		return Config{}, fmt.Errorf("config: merge defaults: %w", err)
	}

	// 3. YAML front matter.
	if opts.FrontMatter != nil {
		if err := v.MergeConfigMap(opts.FrontMatter); err != nil {
			return Config{}, fmt.Errorf("config: merge front matter: %w", err)
		}
	}

	// 2. Environment variables: AutomaticEnv + a hint per key. Viper does
	//    not bind env vars eagerly when the key was only seen via defaults
	//    or front matter, so explicit BindEnv calls keep precedence honest.
	for _, k := range v.AllKeys() {
		_ = v.BindEnv(k)
	}

	// 1. CLI flags (highest priority).
	if opts.Flags != nil {
		for flagName, key := range flagBindings {
			if f := opts.Flags.Lookup(flagName); f != nil {
				_ = v.BindPFlag(key, f)
			}
		}
	}

	cfg := Config{}
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("config: unmarshal: %w", err)
	}

	// 4. $VAR indirection over the resolved string fields. The set of
	//    fields that participate in expansion is intentionally small — only
	//    fields the SPEC §6.2 lists as path/credential strings.
	if err := expandReferences(&cfg, opts.Lookup); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// Validate runs the SPEC §6.4 startup checks. The orchestrator calls this
// before the scheduling loop starts; the dry-run path calls it as the
// last step of `conductor start --dry-run` to surface bad config before any
// work is dispatched.
func Validate(cfg Config) error {
	var errs []error

	if cfg.Project.ID == "" {
		errs = append(errs, errors.New("project.id is required"))
	}
	if cfg.Tracker.Kind == "" {
		errs = append(errs, errors.New("tracker.kind is required"))
	} else if !supportedTrackerKind(cfg.Tracker.Kind) {
		errs = append(errs, fmt.Errorf("tracker.kind %q is not supported", cfg.Tracker.Kind))
	}
	if cfg.Tracker.APIKey == "" {
		errs = append(errs, errors.New("tracker.api_key is required"))
	}
	if requiresProjectID(cfg.Tracker.Kind) && cfg.Tracker.ProjectID == "" && cfg.Tracker.ProjectSlug == "" {
		errs = append(errs, fmt.Errorf("tracker.project_id or project_slug is required for kind %q", cfg.Tracker.Kind))
	}
	if cfg.Providers.Default.Provider == "" {
		errs = append(errs, errors.New("providers.default.provider is required"))
	} else if !supportedProvider(cfg.Providers.Default.Provider) {
		errs = append(errs, fmt.Errorf("providers.default.provider %q is not supported", cfg.Providers.Default.Provider))
	}
	if cfg.Providers.Default.APIKey == "" && providerNeedsAPIKey(cfg.Providers.Default.Provider) {
		errs = append(errs, errors.New("providers.default.api_key is required"))
	}
	if !supportedKnowledgeBackend(cfg.Knowledge.StoreBackend) {
		errs = append(errs, fmt.Errorf("knowledge.store_backend %q is not supported", cfg.Knowledge.StoreBackend))
	}
	if !supportedMemoryBackend(cfg.Memory.StoreBackend) {
		errs = append(errs, fmt.Errorf("memory.store_backend %q is not supported", cfg.Memory.StoreBackend))
	}
	for i, store := range cfg.Docs.Stores {
		if !supportedDocBackend(store.Backend) {
			errs = append(errs, fmt.Errorf("docs.stores[%d].backend %q is not supported", i, store.Backend))
		}
	}

	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

func supportedTrackerKind(k string) bool {
	switch k {
	case "linear", "github", "jira", "plane", "shortcut":
		return true
	}
	return false
}

func requiresProjectID(k string) bool {
	switch k {
	case "linear", "plane", "github", "jira", "shortcut":
		return true
	}
	return false
}

func supportedProvider(p string) bool {
	switch p {
	case "openrouter", "anthropic", "openai", "ollama", "lm_studio", "custom":
		return true
	}
	return false
}

// providerNeedsAPIKey reports whether a provider string requires an api_key
// at startup. Local providers (Ollama, LM Studio) talk to a localhost socket
// and have no key concept; SPEC §6.4 only requires api_key for hosted APIs.
func providerNeedsAPIKey(p string) bool {
	switch p {
	case "ollama", "lm_studio":
		return false
	}
	return true
}

func supportedKnowledgeBackend(b string) bool {
	switch b {
	case "sqlite_vec", "qdrant":
		return true
	}
	return false
}

func supportedMemoryBackend(b string) bool {
	switch b {
	case "sqlite", "postgres":
		return true
	}
	return false
}

func supportedDocBackend(b string) bool {
	switch b {
	case "local_fs", "git_repo", "s3", "notion", "confluence", "custom":
		return true
	}
	return false
}
