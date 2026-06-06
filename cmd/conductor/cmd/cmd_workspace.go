package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/conductor-sh/conductor/internal/config"
	"github.com/conductor-sh/conductor/internal/harness"
	"github.com/conductor-sh/conductor/internal/workspace"
)

// newWorkspaceCommand wires `conductor workspace ...` (SPEC §19.1). The three
// subcommands inspect and manage the on-disk per-issue workspaces under
// workspace.root. They operate offline (no running service required) and route
// destructive operations through the workspace Manager so the SPEC §14.2 safety
// invariants and before_remove hook run.
func newWorkspaceCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workspace",
		Short: "List, remove, and open per-issue workspaces",
	}
	cmd.AddCommand(newWorkspaceListCommand())
	cmd.AddCommand(newWorkspaceRemoveCommand())
	cmd.AddCommand(newWorkspaceOpenCommand())
	return cmd
}

// workspaceFlags carries the shared options for the workspace subcommands.
type workspaceFlags struct {
	harness string
	format  string
}

// workspaceEntry is the wire shape of one workspace in `list` output. It is
// shared by the text and JSON formats so both report the same fields.
type workspaceEntry struct {
	Key             string   `json:"key"`
	IssueID         string   `json:"issue_id,omitempty"`
	IssueIdentifier string   `json:"issue_identifier,omitempty"`
	Path            string   `json:"path"`
	Repos           []string `json:"repos,omitempty"`
}

func newWorkspaceListCommand() *cobra.Command {
	flags := &workspaceFlags{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the workspaces under workspace.root",
		Long: "Enumerate the per-issue workspaces materialized under " +
			"workspace.root, reporting each workspace's key, issue identifier, " +
			"path, and materialized repositories.",
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadWorkspaceConfig(cmd, flags)
			if err != nil {
				return err
			}
			return runWorkspaceList(cmd.OutOrStdout(), cfg, flags.format)
		},
	}
	addWorkspaceFlags(cmd, flags)
	return cmd
}

func newWorkspaceRemoveCommand() *cobra.Command {
	flags := &workspaceFlags{}
	var yes bool
	cmd := &cobra.Command{
		Use:   "remove <key>",
		Short: "Remove a workspace through the workspace manager",
		Long: "Delete a workspace by key. Removal goes through the workspace " +
			"manager so the before_remove hook and the SPEC §14.2 safety " +
			"invariants run. Requires --yes for non-interactive confirmation.",
		Args:          cobra.ExactArgs(1),
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			cfg, err := loadWorkspaceConfig(cmd, flags)
			if err != nil {
				return err
			}
			return runWorkspaceRemove(ctx, cmd.OutOrStdout(), cfg, args[0], yes)
		},
	}
	addWorkspaceFlags(cmd, flags)
	cmd.Flags().BoolVar(&yes, "yes", false,
		"confirm removal without an interactive prompt")
	return cmd
}

func newWorkspaceOpenCommand() *cobra.Command {
	flags := &workspaceFlags{}
	cmd := &cobra.Command{
		Use:   "open <key>",
		Short: "Open a workspace in $EDITOR (or print its path)",
		Long: "Resolve the workspace path for a key and launch it in $EDITOR. " +
			"When $EDITOR is unset the resolved path is printed instead of " +
			"failing.",
		Args:          cobra.ExactArgs(1),
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadWorkspaceConfig(cmd, flags)
			if err != nil {
				return err
			}
			return runWorkspaceOpen(cmd.OutOrStdout(), cfg, args[0], os.LookupEnv)
		},
	}
	addWorkspaceFlags(cmd, flags)
	return cmd
}

func addWorkspaceFlags(cmd *cobra.Command, flags *workspaceFlags) {
	cmd.Flags().StringVar(&flags.harness, "harness", "",
		"path to HARNESS.md (default: $CONDUCTOR_HARNESS_PATH or ./HARNESS.md)")
	cmd.Flags().StringVar(&flags.format, "format", outputFormatText,
		"output format (text, json)")
}

// loadWorkspaceConfig resolves the effective config from HARNESS.md, falling
// back to defaults so the workspace commands run on a fresh checkout.
func loadWorkspaceConfig(cmd *cobra.Command, flags *workspaceFlags) (config.Config, error) {
	harnessPath := harness.ResolvePath(flags.harness, nil)
	loadOpts := config.LoadOptions{Flags: cmd.Flags()}
	res, _ := harness.Load(harnessPath, loadOpts)
	cfg := res.Config
	if res.Definition == nil {
		c, err := config.Load(loadOpts)
		if err != nil {
			return config.Config{}, fmt.Errorf("workspace: load config: %w", err)
		}
		cfg = c
	}
	return cfg, nil
}

// newWorkspaceManager constructs a workspace.Manager bound to the configured
// root. The CLI manager carries no audit writer (workspace audit events are the
// running service's concern); removal still runs hooks and invariants.
func newWorkspaceManager(cfg config.Config) *workspace.Manager {
	return workspace.New(cfg.Workspace, cfg.Hooks,
		workspace.WithLogger(zerolog.Nop()),
		workspace.WithProjectID(cfg.Project.ID),
	)
}

// enumerateWorkspaces lists the workspace directories directly under
// workspace.root. Each immediate subdirectory is one workspace keyed by its
// directory name; meta.json (when present) supplies the issue identifier/id.
func enumerateWorkspaces(cfg config.Config) ([]workspaceEntry, error) {
	root := cfg.Workspace.Root
	if root == "" {
		return nil, fmt.Errorf("workspace: workspace.root is not configured")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return []workspaceEntry{}, nil
		}
		return nil, fmt.Errorf("workspace: read root %q: %w", root, err)
	}

	out := make([]workspaceEntry, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(root, e.Name())
		entry := workspaceEntry{
			Key:   e.Name(),
			Path:  path,
			Repos: enumerateRepos(path),
		}
		if id, identifier, ok := readWorkspaceMeta(path); ok {
			entry.IssueID = id
			entry.IssueIdentifier = identifier
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// enumerateRepos lists the materialized repository subdirectories of a
// workspace (every immediate subdirectory except the .conductor layout dir).
func enumerateRepos(path string) []string {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil
	}
	var repos []string
	for _, e := range entries {
		if !e.IsDir() || e.Name() == ".conductor" {
			continue
		}
		repos = append(repos, e.Name())
	}
	sort.Strings(repos)
	return repos
}

// readWorkspaceMeta reads the issue id/identifier from a workspace's
// .conductor/meta.json. A missing or unreadable file is not an error — the
// workspace is still listed by its directory key.
func readWorkspaceMeta(path string) (id, identifier string, ok bool) {
	data, err := os.ReadFile(filepath.Join(path, ".conductor", "meta.json"))
	if err != nil {
		return "", "", false
	}
	var meta struct {
		IssueID         string `json:"issue_id"`
		IssueIdentifier string `json:"issue_identifier"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return "", "", false
	}
	return meta.IssueID, meta.IssueIdentifier, true
}

func runWorkspaceList(out io.Writer, cfg config.Config, format string) error {
	if format != outputFormatText && format != outputFormatJSON {
		return fmt.Errorf("workspace list: unsupported --format %q (want text or json)", format)
	}
	entries, err := enumerateWorkspaces(cfg)
	if err != nil {
		return err
	}
	if format == outputFormatJSON {
		return writeJSON(out, map[string]any{
			"root":       cfg.Workspace.Root,
			"workspaces": entries,
		})
	}
	if len(entries) == 0 {
		_, werr := fmt.Fprintf(out, "no workspaces under %s\n", cfg.Workspace.Root)
		return werr
	}
	for _, e := range entries {
		identifier := e.IssueIdentifier
		if identifier == "" {
			identifier = "-"
		}
		if _, werr := fmt.Fprintf(out, "%s\t%s\t%s\trepos=%d\n",
			e.Key, identifier, e.Path, len(e.Repos)); werr != nil {
			return werr
		}
	}
	return nil
}

func runWorkspaceRemove(ctx context.Context, out io.Writer, cfg config.Config, key string, yes bool) error {
	if !yes {
		return fmt.Errorf("workspace remove: refusing to delete %q without --yes", key)
	}

	// Confirm the workspace exists before invoking the manager so the operator
	// gets a clear "not found" rather than a silent success on a typo'd key.
	entries, err := enumerateWorkspaces(cfg)
	if err != nil {
		return err
	}
	found := false
	for _, e := range entries {
		if e.Key == key {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("workspace remove: no workspace with key %q under %s", key, cfg.Workspace.Root)
	}

	mgr := newWorkspaceManager(cfg)
	// Resolve uses the key as the issue identifier; SanitizeKey is idempotent on
	// an already-sanitized directory name, so the resolved path matches the
	// listed workspace.
	ws, err := mgr.Resolve("", key)
	if err != nil {
		return fmt.Errorf("workspace remove: resolve %q: %w", key, err)
	}
	if err := mgr.Remove(ctx, ws, nil); err != nil {
		return fmt.Errorf("workspace remove: %w", err)
	}
	_, werr := fmt.Fprintf(out, "removed workspace %s\n", key)
	return werr
}

func runWorkspaceOpen(out io.Writer, cfg config.Config, key string, lookupEnv func(string) (string, bool)) error {
	mgr := newWorkspaceManager(cfg)
	ws, err := mgr.Resolve("", key)
	if err != nil {
		return fmt.Errorf("workspace open: resolve %q: %w", key, err)
	}
	if info, statErr := os.Stat(ws.Path); statErr != nil || !info.IsDir() {
		return fmt.Errorf("workspace open: no workspace at %s", ws.Path)
	}

	editor, ok := lookupEnv("EDITOR")
	if !ok || editor == "" {
		_, werr := fmt.Fprintf(out, "$EDITOR not set; workspace path: %s\n", ws.Path)
		return werr
	}

	c := exec.Command(editor, ws.Path)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("workspace open: launch %q: %w", editor, err)
	}
	return nil
}
