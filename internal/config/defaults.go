package config

import (
	"os"
	"path/filepath"
)

// DefaultHarnessPath is the conventional location of HARNESS.md when the
// caller has not overridden it via flag or environment.
const DefaultHarnessPath = "HARNESS.md"

// Defaults returns the built-in defaults for every field SPEC §5.3 declares
// a default for. Required-but-unset fields (project.id, tracker.kind, etc.)
// remain at their zero value and are caught by Validate (SPEC §6.4).
//
// Callers must overlay defaults BENEATH user-supplied values; that is
// exactly what Load does — see SPEC §6.1 for the precedence chain.
func Defaults() Config {
	return Config{
		Polling: Polling{
			IntervalMS: 30_000,
		},
		Workspace: Workspace{
			Root: filepath.Join(os.TempDir(), "conductor_workspaces"),
		},
		Hooks: Hooks{
			TimeoutMS: 60_000,
		},
		Tracker: Tracker{
			ActiveStates:   []string{"Todo", "In Progress"},
			TerminalStates: []string{"Done", "Cancelled", "Canceled", "Closed", "Duplicate"},
		},
		Providers: Providers{
			Default: ProviderConfig{
				MaxTokens:          8192,
				CompactionStrategy: "summarize",
			},
			Roles: map[string]ProviderConfig{},
		},
		Routing: Routing{
			Pipeline: []string{"planner", "coder", "verifier"},
		},
		Knowledge: Knowledge{
			Enabled:         true,
			IndexOnStartup:  true,
			WatchForChanges: true,
			ExcludePatterns: []string{
				"**/node_modules/**",
				"**/.git/**",
				"**/vendor/**",
				"**/__pycache__/**",
			},
			EmbeddingProvider: "default",
			StoreBackend:      "sqlite_vec",
			TopK:              10,
			UseAST:            true,
		},
		Memory: Memory{
			Enabled:                    true,
			StoreBackend:               "sqlite",
			EpisodicTTLDays:            90,
			MaxContextMemories:         5,
			ConsolidationEnabled:       true,
			ConsolidationIntervalHours: 24,
			ConsolidationProvider:      "default",
		},
		Docs: Docs{
			Enabled: true,
		},
		Enforcement: Enforcement{
			Enabled:              true,
			GCIssueLabel:         "gc_task",
			DriftCheckOnDispatch: true,
		},
		Validation: Validation{
			Enabled:                  true,
			RunAfterTurn:             true,
			InjectResultsIntoContext: true,
			TimeoutMS:                300_000,
		},
		Agent: Agent{
			MaxConcurrentAgents:        10,
			MaxTurns:                   20,
			MaxRetryBackoffMS:          300_000,
			MaxConcurrentAgentsByState: map[string]int{},
		},
		Server: Server{
			Port:            8080,
			Host:            "127.0.0.1",
			CORSOrigins:     []string{"http://localhost:5173"},
			EnableAPI:       true,
			EnableDashboard: true,
		},
	}
}
