package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/conductor-sh/conductor/internal/audit"
	"github.com/conductor-sh/conductor/internal/config"
	"github.com/conductor-sh/conductor/internal/db"
	"github.com/conductor-sh/conductor/internal/harness"
	"github.com/conductor-sh/conductor/internal/knowledge"
)

// newKnowledgeCommand wires the `conductor knowledge ...` parent command
// (SPEC §8, §19). Subcommands: index (build/refresh the graph) and search
// (hybrid query). Both load HARNESS.md to resolve knowledge config + roots.
func newKnowledgeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "knowledge",
		Short: "Index the codebase and run hybrid knowledge searches",
	}
	cmd.AddCommand(newKnowledgeIndexCommand())
	cmd.AddCommand(newKnowledgeSearchCommand())
	return cmd
}

// knowledgeFlags carries the shared options for the knowledge subcommands.
type knowledgeFlags struct {
	harness string
	format  string
}

func newKnowledgeIndexCommand() *cobra.Command {
	flags := &knowledgeFlags{}
	cmd := &cobra.Command{
		Use:   "index",
		Short: "Index the workspace repositories into the knowledge graph",
		Long: "Run the seven-stage indexing pipeline over the workspace repos " +
			"and persist nodes + edges to the configured store_backend. Writes " +
			"a KnowledgeIndexed audit event on completion.",
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			return runKnowledgeIndex(ctx, cmd, flags)
		},
	}
	addKnowledgeFlags(cmd, flags)
	return cmd
}

func newKnowledgeSearchCommand() *cobra.Command {
	flags := &knowledgeFlags{}
	var topK int
	var includeDeps bool
	cmd := &cobra.Command{
		Use:           "search [query]",
		Short:         "Run a hybrid knowledge search over the indexed graph",
		Args:          cobra.MinimumNArgs(1),
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			return runKnowledgeSearch(ctx, cmd, flags, args[0], topK, includeDeps)
		},
	}
	addKnowledgeFlags(cmd, flags)
	cmd.Flags().IntVar(&topK, "top-k", 0, "max results (default: knowledge.top_k or 10)")
	cmd.Flags().BoolVar(&includeDeps, "include-dependencies", false,
		"expand results with one dependency hop")
	return cmd
}

func addKnowledgeFlags(cmd *cobra.Command, flags *knowledgeFlags) {
	cmd.Flags().StringVar(&flags.harness, "harness", "",
		"path to HARNESS.md (default: $CONDUCTOR_HARNESS_PATH or ./HARNESS.md)")
	cmd.Flags().StringVar(&flags.format, "format", outputFormatText,
		"output format (text, json)")
}

// loadKnowledgeContext resolves the config from HARNESS.md (or defaults) and
// the workspace repo roots for the knowledge subcommands.
func loadKnowledgeContext(cmd *cobra.Command, flags *knowledgeFlags) (config.Config, []string, error) {
	harnessPath := harness.ResolvePath(flags.harness, nil)
	loadOpts := config.LoadOptions{Flags: cmd.Flags()}
	res, _ := harness.Load(harnessPath, loadOpts)
	cfg := res.Config
	if res.Definition == nil {
		c, err := config.Load(loadOpts)
		if err != nil {
			return config.Config{}, nil, fmt.Errorf("knowledge: load config: %w", err)
		}
		cfg = c
	}
	return cfg, workspaceRoots(cfg), nil
}

// workspaceRoots returns the per-repo roots to index. A single-repo workspace
// (no repos configured) indexes the workspace root itself.
func workspaceRoots(cfg config.Config) []string {
	if len(cfg.Workspace.Repos) == 0 {
		root := cfg.Workspace.Root
		if root == "" {
			root = "."
		}
		return []string{root}
	}
	roots := make([]string, 0, len(cfg.Workspace.Repos))
	for _, r := range cfg.Workspace.Repos {
		roots = append(roots, r.Name)
	}
	return roots
}

func runKnowledgeIndex(ctx context.Context, cmd *cobra.Command, flags *knowledgeFlags) error {
	if flags.format != outputFormatText && flags.format != outputFormatJSON {
		return fmt.Errorf("knowledge index: unsupported --format %q", flags.format)
	}
	cfg, roots, err := loadKnowledgeContext(cmd, flags)
	if err != nil {
		return err
	}

	eng, store, writer, cleanup, err := buildKnowledgeEngine(ctx, cfg)
	if err != nil {
		return err
	}
	defer cleanup()
	_ = store

	count, err := eng.Index(ctx, roots)
	if err != nil {
		return fmt.Errorf("knowledge index: %w", err)
	}
	_ = writer

	out := cmd.OutOrStdout()
	if flags.format == outputFormatJSON {
		return writeJSON(out, map[string]any{"indexed_nodes": count, "roots": roots})
	}
	_, werr := fmt.Fprintf(out, "indexed %d node(s) from %d root(s)\n", count, len(roots))
	return werr
}

func runKnowledgeSearch(
	ctx context.Context, cmd *cobra.Command, flags *knowledgeFlags,
	query string, topK int, includeDeps bool,
) error {
	if flags.format != outputFormatText && flags.format != outputFormatJSON {
		return fmt.Errorf("knowledge search: unsupported --format %q", flags.format)
	}
	cfg, _, err := loadKnowledgeContext(cmd, flags)
	if err != nil {
		return err
	}

	eng, _, _, cleanup, err := buildKnowledgeEngine(ctx, cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	results, err := eng.Search(ctx, knowledge.SearchParams{
		Query:               query,
		TopK:                topK,
		IncludeDependencies: includeDeps,
	})
	if err != nil {
		return fmt.Errorf("knowledge search: %w", err)
	}
	return writeSearchResults(cmd.OutOrStdout(), results, flags.format)
}

// buildKnowledgeEngine constructs the store, audit writer, and engine for a CLI
// invocation. The returned cleanup closes both. The audit writer persists to the
// same conductor.db so `KnowledgeIndexed` events land in the provenance graph.
func buildKnowledgeEngine(
	ctx context.Context, cfg config.Config,
) (*knowledge.Engine, knowledge.Store, *audit.Writer, func(), error) {
	log := zerolog.Nop()

	store, err := knowledge.OpenStore(ctx, cfg.Knowledge, log)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("knowledge: open store: %w", err)
	}

	d, dbErr := db.Open(ctx, db.Options{Driver: db.DriverSQLite, DSN: "conductor.db"})
	writer := audit.NewWriter(log)
	if dbErr == nil {
		if _, mErr := db.Migrate(ctx, d); mErr == nil {
			writer.AddSink(audit.NewDBSink(d))
		}
	}

	eng := knowledge.New(cfg.Knowledge, cfg.Project.ID,
		knowledge.WithStore(store),
		knowledge.WithAudit(writer),
		knowledge.WithLogger(log),
	)

	cleanup := func() {
		_ = writer.Close()
		if d != nil {
			_ = d.Close()
		}
		_ = store.Close()
	}
	return eng, store, writer, cleanup, nil
}

func writeSearchResults(out io.Writer, results []knowledge.ScoredNode, format string) error {
	if format == outputFormatJSON {
		return writeJSON(out, results)
	}
	if len(results) == 0 {
		_, err := fmt.Fprintln(out, "no results")
		return err
	}
	for _, r := range results {
		name := r.Node.Name
		if _, err := fmt.Fprintf(out, "%.3f  [%s] %s (%s)\n",
			r.Score, r.Node.Type, r.Node.Path, name); err != nil {
			return err
		}
	}
	return nil
}

func writeJSON(out io.Writer, v any) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("knowledge: encode json: %w", err)
	}
	return nil
}
