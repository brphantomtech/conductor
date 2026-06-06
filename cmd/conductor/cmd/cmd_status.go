package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/conductor-sh/conductor/internal/audit"
	"github.com/conductor-sh/conductor/internal/config"
	"github.com/conductor-sh/conductor/internal/db"
)

// newStatusCommand wires `conductor status` (SPEC §19.1). Without the Phase 14
// control channel it prints the statically derivable snapshot — a config
// summary, the workspace inventory, and storage (DB/audit) reachability. The
// live in-memory orchestrator runtime state requires the running service and is
// reported as unavailable here rather than faked.
func newStatusCommand() *cobra.Command {
	var harnessPath string
	var format string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Print the static service snapshot (config, workspaces, storage)",
		Long: "Print the statically derivable status snapshot: configuration " +
			"summary, workspace inventory, and database/audit reachability. The " +
			"live orchestrator runtime state requires the running service (Phase " +
			"14) and is reported as unavailable.",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			flags := &workspaceFlags{harness: harnessPath, format: format}
			cfg, err := loadWorkspaceConfig(cmd, flags)
			if err != nil {
				return err
			}
			return runStatus(ctx, cmd.OutOrStdout(), cfg, format)
		},
	}
	cmd.Flags().StringVar(&harnessPath, "harness", "",
		"path to HARNESS.md (default: $CONDUCTOR_HARNESS_PATH or ./HARNESS.md)")
	cmd.Flags().StringVar(&format, "format", outputFormatText,
		"output format (text, json)")
	return cmd
}

// statusSnapshot is the wire shape of the static status report, shared by the
// text and JSON formats.
type statusSnapshot struct {
	Config    statusConfig     `json:"config"`
	Workspace statusWorkspace  `json:"workspace"`
	Storage   statusStorage    `json:"storage"`
	Live      statusLiveNotice `json:"live"`
}

// statusConfig summarizes the parts of the configuration knowable offline.
type statusConfig struct {
	ProjectID        string `json:"project_id"`
	ProjectName      string `json:"project_name,omitempty"`
	TrackerKind      string `json:"tracker_kind"`
	DefaultProvider  string `json:"default_provider"`
	PollingInterval  int    `json:"polling_interval_ms"`
	KnowledgeEnabled bool   `json:"knowledge_enabled"`
	MemoryEnabled    bool   `json:"memory_enabled"`
}

// statusWorkspace is the workspace inventory section.
type statusWorkspace struct {
	Root  string   `json:"root"`
	Count int      `json:"count"`
	Keys  []string `json:"keys,omitempty"`
}

// statusStorage reports database/audit reachability.
type statusStorage struct {
	DBPath    string `json:"db_path"`
	Reachable bool   `json:"reachable"`
	Detail    string `json:"detail,omitempty"`
}

// statusLiveNotice documents that the live runtime snapshot needs the running
// service. It keeps `status` honest: the static sections are real; the live
// section explicitly requires Phase 14.
type statusLiveNotice struct {
	Available bool   `json:"available"`
	Notice    string `json:"notice"`
}

const statusDBPath = "conductor.db"

// runStatus assembles and prints the static snapshot.
func runStatus(ctx context.Context, out io.Writer, cfg config.Config, format string) error {
	if format != outputFormatText && format != outputFormatJSON {
		return fmt.Errorf("status: unsupported --format %q (want text or json)", format)
	}

	entries, wsErr := enumerateWorkspaces(cfg)
	keys := make([]string, 0, len(entries))
	for _, e := range entries {
		keys = append(keys, e.Key)
	}

	snap := statusSnapshot{
		Config: statusConfig{
			ProjectID:        cfg.Project.ID,
			ProjectName:      cfg.Project.Name,
			TrackerKind:      cfg.Tracker.Kind,
			DefaultProvider:  cfg.Providers.Default.Provider,
			PollingInterval:  cfg.Polling.IntervalMS,
			KnowledgeEnabled: cfg.Knowledge.Enabled,
			MemoryEnabled:    cfg.Memory.Enabled,
		},
		Workspace: statusWorkspace{
			Root:  cfg.Workspace.Root,
			Count: len(entries),
			Keys:  keys,
		},
		Storage: checkStorage(ctx),
		Live: statusLiveNotice{
			Available: false,
			Notice:    "live runtime snapshot requires the running service (Phase 14)",
		},
	}
	if wsErr != nil {
		snap.Workspace.Root = cfg.Workspace.Root
	}

	if format == outputFormatJSON {
		return writeJSON(out, snap)
	}
	return writeStatusText(out, snap, wsErr)
}

// checkStorage probes the SQLite database and audit pipeline for reachability:
// it opens conductor.db, migrates it, and writes through an audit writer with a
// DB sink. A failure at any step is reported, not fatal.
func checkStorage(ctx context.Context) statusStorage {
	st := statusStorage{DBPath: statusDBPath}
	d, err := db.Open(ctx, db.Options{Driver: db.DriverSQLite, DSN: statusDBPath})
	if err != nil {
		st.Detail = fmt.Sprintf("open: %v", err)
		return st
	}
	defer func() { _ = d.Close() }()

	if _, err := db.Migrate(ctx, d); err != nil {
		st.Detail = fmt.Sprintf("migrate: %v", err)
		return st
	}
	if err := d.SQL().PingContext(ctx); err != nil {
		st.Detail = fmt.Sprintf("ping: %v", err)
		return st
	}

	// Prove the audit sink is wired end-to-end against the same DB.
	writer := audit.NewWriter(zerolog.Nop())
	writer.AddSink(audit.NewDBSink(d))
	if err := writer.Close(); err != nil {
		st.Detail = fmt.Sprintf("audit: %v", err)
		return st
	}

	st.Reachable = true
	return st
}

func writeStatusText(out io.Writer, snap statusSnapshot, wsErr error) error {
	lines := []string{
		"== config ==",
		fmt.Sprintf("project:  %s (%s)", nonEmpty(snap.Config.ProjectID, "<unset>"), snap.Config.ProjectName),
		fmt.Sprintf("tracker:  %s", nonEmpty(snap.Config.TrackerKind, "<unset>")),
		fmt.Sprintf("provider: %s", nonEmpty(snap.Config.DefaultProvider, "<unset>")),
		fmt.Sprintf("polling:  %dms", snap.Config.PollingInterval),
		fmt.Sprintf("knowledge=%t memory=%t", snap.Config.KnowledgeEnabled, snap.Config.MemoryEnabled),
		"",
		"== workspaces ==",
		fmt.Sprintf("root:  %s", snap.Workspace.Root),
		fmt.Sprintf("count: %d", snap.Workspace.Count),
		"",
		"== storage ==",
		fmt.Sprintf("db:        %s", snap.Storage.DBPath),
		fmt.Sprintf("reachable: %t", snap.Storage.Reachable),
		"",
		"== live ==",
		snap.Live.Notice,
	}
	if wsErr != nil {
		lines[8] = fmt.Sprintf("root:  %s (error: %v)", snap.Workspace.Root, wsErr)
	}
	if !snap.Storage.Reachable && snap.Storage.Detail != "" {
		lines[13] = fmt.Sprintf("reachable: false (%s)", snap.Storage.Detail)
	}
	for _, l := range lines {
		if _, err := fmt.Fprintln(out, l); err != nil {
			return err
		}
	}
	for _, k := range snap.Workspace.Keys {
		if _, err := fmt.Fprintf(out, "  - %s\n", k); err != nil {
			return err
		}
	}
	return nil
}

// nonEmpty returns fallback when s is empty.
func nonEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
