package cmd

import (
	"fmt"
	"runtime"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// VersionInfo bundles the build identifiers printed by `conductor version`.
// Callers that need the data programmatically (the API /status handler in
// Phase 14) read it via ReadVersionInfo rather than parsing stdout.
type VersionInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit,omitempty"`
	BuildDate string `json:"build_date,omitempty"`
	GoVersion string `json:"go_version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

// ReadVersionInfo extracts version metadata from the binary's build info.
// `go install` and `go build` populate Main.Version with the module
// pseudo-version; in dev builds (no module info) it falls back to "dev".
func ReadVersionInfo() VersionInfo {
	v := VersionInfo{
		Version:   "dev",
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return v
	}

	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		v.Version = info.Main.Version
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			v.Commit = s.Value
		case "vcs.time":
			v.BuildDate = s.Value
		}
	}
	return v
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version and build info",
		RunE: func(cmd *cobra.Command, _ []string) error {
			v := ReadVersionInfo()
			out := cmd.OutOrStdout()
			if _, err := fmt.Fprintf(out, "conductor %s\n", v.Version); err != nil {
				return fmt.Errorf("version: write: %w", err)
			}
			if v.Commit != "" {
				if _, err := fmt.Fprintf(out, "  commit:    %s\n", v.Commit); err != nil {
					return fmt.Errorf("version: write: %w", err)
				}
			}
			if v.BuildDate != "" {
				if _, err := fmt.Fprintf(out, "  built:     %s\n", v.BuildDate); err != nil {
					return fmt.Errorf("version: write: %w", err)
				}
			}
			if _, err := fmt.Fprintf(out, "  go:        %s\n", v.GoVersion); err != nil {
				return fmt.Errorf("version: write: %w", err)
			}
			if _, err := fmt.Fprintf(out, "  platform:  %s/%s\n", v.OS, v.Arch); err != nil {
				return fmt.Errorf("version: write: %w", err)
			}
			return nil
		},
	}
}
