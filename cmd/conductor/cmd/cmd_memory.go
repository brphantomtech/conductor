package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/conductor-sh/conductor/internal/config"
	"github.com/conductor-sh/conductor/internal/memory"
)

// newMemoryCommand wires `conductor memory ...` (SPEC §9 CLI). The three
// subcommands inspect and maintain the three-layer memory store.
func newMemoryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "memory",
		Short: "Inspect and maintain the three-layer memory store",
	}
	cmd.AddCommand(newMemoryListCommand())
	cmd.AddCommand(newMemoryRetireCommand())
	cmd.AddCommand(newMemoryConsolidateCommand())
	return cmd
}

// memoryFlags holds the shared flags for the memory subcommands.
type memoryFlags struct {
	harness string
}

// openMemoryManager loads config from HARNESS.md and constructs a Manager
// against the configured store. The consolidation provider is wired so
// `consolidate` can synthesize; list/retire do not need it.
func openMemoryManager(ctx context.Context, flags *memoryFlags, withProvider bool) (*memory.Manager, config.Config, error) {
	loadOpts := config.LoadOptions{}
	cfg, err := config.Load(loadOpts)
	if err != nil {
		return nil, config.Config{}, fmt.Errorf("memory: load config: %w", err)
	}

	memCfg := cfg.Memory
	// The CLI always operates on the store regardless of the runtime
	// enabled flag so operators can inspect memory even when the live
	// orchestrator has memory disabled.
	memCfg.Enabled = true

	opts := []memory.Option{memory.WithProjectID(cfg.Project.ID)}
	if withProvider {
		pc := resolveProviderConfig(cfg, cfg.Memory.ConsolidationProvider)
		ap := memory.NewAPIProvider(pc, nil)
		opts = append(opts, memory.WithEmbedder(ap), memory.WithSynthesizer(ap))
	}

	mgr, err := memory.New(ctx, memCfg, opts...)
	if err != nil {
		return nil, config.Config{}, err
	}
	return mgr, cfg, nil
}

func newMemoryListCommand() *cobra.Command {
	flags := &memoryFlags{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Show per-layer memory counts for the project",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			mgr, cfg, err := openMemoryManager(ctx, flags, false)
			if err != nil {
				return err
			}
			defer func() { _ = mgr.Close() }()

			counts, err := mgr.Counts(ctx, cfg.Project.ID)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "episodic:   %d\n", counts.Episodic)
			_, _ = fmt.Fprintf(out, "semantic:   %d\n", counts.Semantic)
			_, _ = fmt.Fprintf(out, "procedural: %d\n", counts.Procedural)
			_, _ = fmt.Fprintf(out, "total:      %d\n", counts.Total())
			return nil
		},
	}
	cmd.Flags().StringVar(&flags.harness, "harness", "", "path to HARNESS.md")
	return cmd
}

func newMemoryRetireCommand() *cobra.Command {
	flags := &memoryFlags{}
	cmd := &cobra.Command{
		Use:   "retire <id>",
		Short: "Retire (delete) a memory entry by id",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			mgr, _, err := openMemoryManager(ctx, flags, false)
			if err != nil {
				return err
			}
			defer func() { _ = mgr.Close() }()

			if err := mgr.Retire(ctx, args[0]); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "retired %s\n", args[0])
			return nil
		},
	}
	cmd.Flags().StringVar(&flags.harness, "harness", "", "path to HARNESS.md")
	return cmd
}

func newMemoryConsolidateCommand() *cobra.Command {
	flags := &memoryFlags{}
	cmd := &cobra.Command{
		Use:   "consolidate",
		Short: "Run one consolidation pass (cluster, synthesize, expire)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			mgr, cfg, err := openMemoryManager(ctx, flags, true)
			if err != nil {
				return err
			}
			defer func() { _ = mgr.Close() }()

			if err := mgr.Consolidate(ctx, cfg.Project.ID); err != nil {
				return err
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "consolidation pass complete")
			return nil
		},
	}
	cmd.Flags().StringVar(&flags.harness, "harness", "", "path to HARNESS.md")
	return cmd
}
