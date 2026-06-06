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
	"github.com/conductor-sh/conductor/internal/docstore"
	"github.com/conductor-sh/conductor/internal/harness"
	"github.com/conductor-sh/conductor/internal/knowledge"
)

// newDocsCommand wires the `conductor docs ...` parent command (SPEC §10, §19).
// Subcommands: sync (run every configured Doc Store) and search (hybrid query
// restricted to `doc` nodes). Both load HARNESS.md to resolve docs config.
func newDocsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "docs",
		Short: "Sync Doc Stores and search indexed documentation",
	}
	cmd.AddCommand(newDocsSyncCommand())
	cmd.AddCommand(newDocsSearchCommand())
	return cmd
}

// docsFlags carries the shared options for the docs subcommands.
type docsFlags struct {
	harness string
	format  string
}

func newDocsSyncCommand() *cobra.Command {
	flags := &docsFlags{}
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync every configured Doc Store and index changed documents",
		Long: "Sync each configured Doc Store, downloading only changed documents " +
			"and indexing them as doc nodes in the Knowledge Engine. Emits a " +
			"DocStoreSynced audit event per store.",
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			return runDocsSync(ctx, cmd, flags)
		},
	}
	addDocsFlags(cmd, flags)
	return cmd
}

func newDocsSearchCommand() *cobra.Command {
	flags := &docsFlags{}
	var topK int
	cmd := &cobra.Command{
		Use:           "search [query]",
		Short:         "Search indexed documentation ranked by relevance",
		Args:          cobra.MinimumNArgs(1),
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			return runDocsSearch(ctx, cmd, flags, args[0], topK)
		},
	}
	addDocsFlags(cmd, flags)
	cmd.Flags().IntVar(&topK, "top-k", 0, "max results (default: knowledge.top_k or 10)")
	return cmd
}

func addDocsFlags(cmd *cobra.Command, flags *docsFlags) {
	cmd.Flags().StringVar(&flags.harness, "harness", "",
		"path to HARNESS.md (default: $CONDUCTOR_HARNESS_PATH or ./HARNESS.md)")
	cmd.Flags().StringVar(&flags.format, "format", outputFormatText,
		"output format (text, json)")
}

// loadDocsConfig resolves the typed config from HARNESS.md (or defaults).
func loadDocsConfig(cmd *cobra.Command, flags *docsFlags) (config.Config, error) {
	harnessPath := harness.ResolvePath(flags.harness, nil)
	loadOpts := config.LoadOptions{Flags: cmd.Flags()}
	res, _ := harness.Load(harnessPath, loadOpts)
	cfg := res.Config
	if res.Definition == nil {
		c, err := config.Load(loadOpts)
		if err != nil {
			return config.Config{}, fmt.Errorf("docs: load config: %w", err)
		}
		cfg = c
	}
	return cfg, nil
}

func runDocsSync(ctx context.Context, cmd *cobra.Command, flags *docsFlags) error {
	if flags.format != outputFormatText && flags.format != outputFormatJSON {
		return fmt.Errorf("docs sync: unsupported --format %q", flags.format)
	}
	cfg, err := loadDocsConfig(cmd, flags)
	if err != nil {
		return err
	}

	mgr, store, cleanup, err := buildDocsManager(ctx, cfg)
	if err != nil {
		return err
	}
	defer cleanup()
	_ = store

	if err := mgr.SyncAll(ctx); err != nil {
		return fmt.Errorf("docs sync: %w", err)
	}

	out := cmd.OutOrStdout()
	if flags.format == outputFormatJSON {
		return writeJSON(out, map[string]any{"synced_stores": len(cfg.Docs.Stores)})
	}
	_, werr := fmt.Fprintf(out, "synced %d store(s)\n", len(cfg.Docs.Stores))
	return werr
}

func runDocsSearch(ctx context.Context, cmd *cobra.Command, flags *docsFlags, query string, topK int) error {
	if flags.format != outputFormatText && flags.format != outputFormatJSON {
		return fmt.Errorf("docs search: unsupported --format %q", flags.format)
	}
	cfg, err := loadDocsConfig(cmd, flags)
	if err != nil {
		return err
	}

	_, store, cleanup, err := buildDocsManager(ctx, cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	eng := knowledge.New(cfg.Knowledge, cfg.Project.ID, knowledge.WithStore(store))
	results, err := eng.Search(ctx, knowledge.SearchParams{
		Query: query,
		Types: []knowledge.NodeType{knowledge.NodeDoc},
		TopK:  topK,
	})
	if err != nil {
		return fmt.Errorf("docs search: %w", err)
	}
	return writeDocsResults(cmd.OutOrStdout(), results, flags.format)
}

// buildDocsManager constructs the Knowledge store, audit writer, and Doc Store
// Manager for a CLI invocation. The returned cleanup closes them. Only local_fs
// stores are constructed automatically; git_repo/s3 stores need injected
// clients and are skipped (logged) by the manager.
func buildDocsManager(
	ctx context.Context, cfg config.Config,
) (*docstore.Manager, knowledge.Store, func(), error) {
	log := zerolog.Nop()

	store, err := knowledge.OpenStore(ctx, cfg.Knowledge, log)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("docs: open knowledge store: %w", err)
	}

	d, dbErr := db.Open(ctx, db.Options{Driver: db.DriverSQLite, DSN: "conductor.db"})
	writer := audit.NewWriter(log)
	if dbErr == nil {
		if _, mErr := db.Migrate(ctx, d); mErr == nil {
			writer.AddSink(audit.NewDBSink(d))
		}
	}

	mgr, err := docstore.New(cfg.Docs, cfg.Project.ID,
		docstore.WithKnowledgeStore(store),
		docstore.WithAudit(writer),
		docstore.WithLogger(log),
	)
	if err != nil {
		_ = writer.Close()
		if d != nil {
			_ = d.Close()
		}
		_ = store.Close()
		return nil, nil, nil, fmt.Errorf("docs: construct manager: %w", err)
	}

	cleanup := func() {
		_ = writer.Close()
		if d != nil {
			_ = d.Close()
		}
		_ = store.Close()
	}
	return mgr, store, cleanup, nil
}

func writeDocsResults(out io.Writer, results []knowledge.ScoredNode, format string) error {
	if format == outputFormatJSON {
		return writeJSON(out, results)
	}
	if len(results) == 0 {
		_, err := fmt.Fprintln(out, "no results")
		return err
	}
	for _, r := range results {
		if _, err := fmt.Fprintf(out, "%.3f  %s (%s)\n", r.Score, r.Node.Name, r.Node.Path); err != nil {
			return err
		}
	}
	return nil
}
