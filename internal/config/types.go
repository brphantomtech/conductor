package config

// Config is the typed representation of HARNESS.md front matter (SPEC §5.3).
// Each top-level field maps to a front-matter section. Field zero values are
// chosen so an empty Config that has been merged with built-in defaults is a
// usable runtime configuration; required fields are validated at startup
// (SPEC §6.4) rather than enforced at construction time.
type Config struct {
	Project      Project       `yaml:"project"      mapstructure:"project"`
	Tracker      Tracker       `yaml:"tracker"      mapstructure:"tracker"`
	Polling      Polling       `yaml:"polling"      mapstructure:"polling"`
	Workspace    Workspace     `yaml:"workspace"    mapstructure:"workspace"`
	Hooks        Hooks         `yaml:"hooks"        mapstructure:"hooks"`
	Providers    Providers     `yaml:"providers"    mapstructure:"providers"`
	Routing      Routing       `yaml:"routing"      mapstructure:"routing"`
	Knowledge    Knowledge     `yaml:"knowledge"    mapstructure:"knowledge"`
	Memory       Memory        `yaml:"memory"       mapstructure:"memory"`
	Docs         Docs          `yaml:"docs"         mapstructure:"docs"`
	HarnessRules []HarnessRule `yaml:"harness_rules" mapstructure:"harness_rules"`
	Enforcement  Enforcement   `yaml:"enforcement"  mapstructure:"enforcement"`
	Validation   Validation    `yaml:"validation"   mapstructure:"validation"`
	Agent        Agent         `yaml:"agent"        mapstructure:"agent"`
	Server       Server        `yaml:"server"       mapstructure:"server"`
}

// Project corresponds to SPEC §5.3.1.
type Project struct {
	ID          string `yaml:"id"          mapstructure:"id"`
	Name        string `yaml:"name"        mapstructure:"name"`
	Description string `yaml:"description" mapstructure:"description"`
}

// Tracker corresponds to SPEC §5.3.2.
type Tracker struct {
	Kind           string            `yaml:"kind"            mapstructure:"kind"`
	Endpoint       string            `yaml:"endpoint"        mapstructure:"endpoint"`
	APIKey         string            `yaml:"api_key"         mapstructure:"api_key"`
	ProjectSlug    string            `yaml:"project_slug"    mapstructure:"project_slug"`
	ProjectID      string            `yaml:"project_id"      mapstructure:"project_id"`
	ActiveStates   []string          `yaml:"active_states"   mapstructure:"active_states"`
	TerminalStates []string          `yaml:"terminal_states" mapstructure:"terminal_states"`
	Extra          map[string]string `yaml:"extra"           mapstructure:"extra"`
}

// Polling corresponds to SPEC §5.3.3.
type Polling struct {
	IntervalMS int `yaml:"interval_ms" mapstructure:"interval_ms"`
}

// Workspace corresponds to SPEC §5.3.4.
type Workspace struct {
	Root  string          `yaml:"root"  mapstructure:"root"`
	Repos []WorkspaceRepo `yaml:"repos" mapstructure:"repos"`
}

// WorkspaceRepo describes a single repository inside a multi-repo workspace.
type WorkspaceRepo struct {
	Name           string   `yaml:"name"            mapstructure:"name"`
	URL            string   `yaml:"url"             mapstructure:"url"`
	BranchTemplate string   `yaml:"branch_template" mapstructure:"branch_template"`
	SparseCheckout []string `yaml:"sparse_checkout" mapstructure:"sparse_checkout"`
	Depth          int      `yaml:"depth"           mapstructure:"depth"`
}

// Hooks corresponds to SPEC §5.3.5. All scripts are optional; a zero string
// means the hook is disabled.
type Hooks struct {
	AfterCreate        string `yaml:"after_create"         mapstructure:"after_create"`
	BeforeRun          string `yaml:"before_run"           mapstructure:"before_run"`
	AfterRun           string `yaml:"after_run"            mapstructure:"after_run"`
	AfterTurn          string `yaml:"after_turn"           mapstructure:"after_turn"`
	BeforeRemove       string `yaml:"before_remove"        mapstructure:"before_remove"`
	OnHarnessViolation string `yaml:"on_harness_violation" mapstructure:"on_harness_violation"`
	TimeoutMS          int    `yaml:"timeout_ms"           mapstructure:"timeout_ms"`
}

// Providers corresponds to SPEC §5.3.6. The Default key is required; any
// other entry overrides the default for the named role.
type Providers struct {
	Default ProviderConfig            `yaml:"default" mapstructure:"default"`
	Roles   map[string]ProviderConfig `yaml:",inline" mapstructure:",remain"`
}

// ProviderConfig corresponds to SPEC §4.1.3.
type ProviderConfig struct {
	Provider           string         `yaml:"provider"            mapstructure:"provider"`
	Model              string         `yaml:"model"               mapstructure:"model"`
	APIKey             string         `yaml:"api_key"             mapstructure:"api_key"`
	BaseURL            string         `yaml:"base_url"            mapstructure:"base_url"`
	MaxTokens          int            `yaml:"max_tokens"          mapstructure:"max_tokens"`
	Temperature        *float64       `yaml:"temperature"         mapstructure:"temperature"`
	ContextBudget      int            `yaml:"context_budget"      mapstructure:"context_budget"`
	CompactionStrategy string         `yaml:"compaction_strategy" mapstructure:"compaction_strategy"`
	ExtraParams        map[string]any `yaml:"extra_params"        mapstructure:"extra_params"`
}

// Routing corresponds to SPEC §5.3.7.
type Routing struct {
	Pipeline []string      `yaml:"pipeline" mapstructure:"pipeline"`
	Rules    []RoutingRule `yaml:"rules"    mapstructure:"rules"`
}

// RoutingRule is one entry in routing.rules.
type RoutingRule struct {
	When     RoutingMatch `yaml:"when"     mapstructure:"when"`
	Pipeline []string     `yaml:"pipeline" mapstructure:"pipeline"`
}

// RoutingMatch holds the conjunctive match conditions of a single
// RoutingRule.
type RoutingMatch struct {
	Labels       []string `yaml:"labels"        mapstructure:"labels"`
	AnyLabel     []string `yaml:"any_label"     mapstructure:"any_label"`
	TaskType     string   `yaml:"task_type"     mapstructure:"task_type"`
	State        string   `yaml:"state"         mapstructure:"state"`
	TitleMatches string   `yaml:"title_matches" mapstructure:"title_matches"`
	Complexity   string   `yaml:"complexity"    mapstructure:"complexity"`
}

// Knowledge corresponds to SPEC §5.3.8.
type Knowledge struct {
	Enabled           bool                `yaml:"enabled"            mapstructure:"enabled"`
	IndexOnStartup    bool                `yaml:"index_on_startup"   mapstructure:"index_on_startup"`
	WatchForChanges   bool                `yaml:"watch_for_changes"  mapstructure:"watch_for_changes"`
	IncludePatterns   []string            `yaml:"include_patterns"   mapstructure:"include_patterns"`
	ExcludePatterns   []string            `yaml:"exclude_patterns"   mapstructure:"exclude_patterns"`
	EmbeddingProvider string              `yaml:"embedding_provider" mapstructure:"embedding_provider"`
	EmbeddingModel    string              `yaml:"embedding_model"    mapstructure:"embedding_model"`
	StoreBackend      string              `yaml:"store_backend"      mapstructure:"store_backend"`
	StorePath         string              `yaml:"store_path"         mapstructure:"store_path"`
	QdrantURL         string              `yaml:"qdrant_url"         mapstructure:"qdrant_url"`
	QdrantCollection  string              `yaml:"qdrant_collection"  mapstructure:"qdrant_collection"`
	TopK              int                 `yaml:"top_k"              mapstructure:"top_k"`
	UseAST            bool                `yaml:"use_ast"            mapstructure:"use_ast"`
	LayerDefinitions  map[string][]string `yaml:"layer_definitions"  mapstructure:"layer_definitions"`
}

// Memory corresponds to SPEC §5.3.9.
type Memory struct {
	Enabled                    bool   `yaml:"enabled"                      mapstructure:"enabled"`
	StoreBackend               string `yaml:"store_backend"                mapstructure:"store_backend"`
	StorePath                  string `yaml:"store_path"                   mapstructure:"store_path"`
	EpisodicTTLDays            int    `yaml:"episodic_ttl_days"            mapstructure:"episodic_ttl_days"`
	MaxContextMemories         int    `yaml:"max_context_memories"         mapstructure:"max_context_memories"`
	ConsolidationEnabled       bool   `yaml:"consolidation_enabled"        mapstructure:"consolidation_enabled"`
	ConsolidationIntervalHours int    `yaml:"consolidation_interval_hours" mapstructure:"consolidation_interval_hours"`
	ConsolidationProvider      string `yaml:"consolidation_provider"       mapstructure:"consolidation_provider"`
}

// Docs corresponds to SPEC §5.3.10.
type Docs struct {
	Enabled bool             `yaml:"enabled" mapstructure:"enabled"`
	Stores  []DocStoreConfig `yaml:"stores"  mapstructure:"stores"`
}

// DocStoreConfig describes a single doc store entry.
type DocStoreConfig struct {
	ID                  string            `yaml:"id"                    mapstructure:"id"`
	Backend             string            `yaml:"backend"               mapstructure:"backend"`
	PathOrURL           string            `yaml:"path_or_url"           mapstructure:"path_or_url"`
	Auth                map[string]string `yaml:"auth"                  mapstructure:"auth"`
	SyncIntervalMinutes int               `yaml:"sync_interval_minutes" mapstructure:"sync_interval_minutes"`
	IncludePatterns     []string          `yaml:"include_patterns"      mapstructure:"include_patterns"`
	Tags                []string          `yaml:"tags"                  mapstructure:"tags"`
}

// HarnessRule corresponds to SPEC §4.1.8.
type HarnessRule struct {
	ID       string `yaml:"id"        mapstructure:"id"`
	Name     string `yaml:"name"      mapstructure:"name"`
	Category string `yaml:"category"  mapstructure:"category"`
	Severity string `yaml:"severity"  mapstructure:"severity"`
	Check    string `yaml:"check"     mapstructure:"check"`
	FixHint  string `yaml:"fix_hint"  mapstructure:"fix_hint"`
	AutoFix  bool   `yaml:"auto_fix"  mapstructure:"auto_fix"`
}

// Enforcement corresponds to SPEC §5.3.12.
type Enforcement struct {
	Enabled                        bool   `yaml:"enabled" mapstructure:"enabled"`
	GCScheduleCron                 string `yaml:"gc_schedule_cron" mapstructure:"gc_schedule_cron"`
	GCIssueLabel                   string `yaml:"gc_issue_label" mapstructure:"gc_issue_label"`
	GCIssueState                   string `yaml:"gc_issue_state" mapstructure:"gc_issue_state"`
	DriftCheckOnDispatch           bool   `yaml:"drift_check_on_dispatch" mapstructure:"drift_check_on_dispatch"`
	BlockingViolationsHaltDispatch bool   `yaml:"blocking_violations_halt_dispatch" mapstructure:"blocking_violations_halt_dispatch"` //nolint:lll // tag length unavoidable
}

// Validation corresponds to SPEC §5.3.13.
type Validation struct {
	Enabled                  bool              `yaml:"enabled" mapstructure:"enabled"`
	RunAfterTurn             bool              `yaml:"run_after_turn" mapstructure:"run_after_turn"`
	FailOnSeverity           string            `yaml:"fail_on_severity" mapstructure:"fail_on_severity"`
	InjectResultsIntoContext bool              `yaml:"inject_results_into_context" mapstructure:"inject_results_into_context"` //nolint:lll // tag length unavoidable
	TimeoutMS                int               `yaml:"timeout_ms" mapstructure:"timeout_ms"`
	Checks                   []ValidationCheck `yaml:"checks" mapstructure:"checks"`
}

// ValidationCheck is one entry in validation.checks.
type ValidationCheck struct {
	ID             string `yaml:"id"               mapstructure:"id"`
	Name           string `yaml:"name"             mapstructure:"name"`
	Command        string `yaml:"command"          mapstructure:"command"`
	TimeoutMS      int    `yaml:"timeout_ms"       mapstructure:"timeout_ms"`
	Severity       string `yaml:"severity"         mapstructure:"severity"`
	OutputMaxBytes int    `yaml:"output_max_bytes" mapstructure:"output_max_bytes"`
}

// Agent corresponds to SPEC §5.3.14.
type Agent struct {
	MaxConcurrentAgents        int            `yaml:"max_concurrent_agents" mapstructure:"max_concurrent_agents"`
	MaxTurns                   int            `yaml:"max_turns" mapstructure:"max_turns"`
	MaxRetryBackoffMS          int            `yaml:"max_retry_backoff_ms" mapstructure:"max_retry_backoff_ms"`
	MaxConcurrentAgentsByState map[string]int `yaml:"max_concurrent_agents_by_state" mapstructure:"max_concurrent_agents_by_state"` //nolint:lll // tag length unavoidable
}

// Server corresponds to SPEC §5.3.15.
type Server struct {
	Port            int      `yaml:"port"             mapstructure:"port"`
	Host            string   `yaml:"host"             mapstructure:"host"`
	CORSOrigins     []string `yaml:"cors_origins"     mapstructure:"cors_origins"`
	EnableAPI       bool     `yaml:"enable_api"       mapstructure:"enable_api"`
	EnableDashboard bool     `yaml:"enable_dashboard" mapstructure:"enable_dashboard"`
	AuditWebhookURL string   `yaml:"audit_webhook_url" mapstructure:"audit_webhook_url"`
}
