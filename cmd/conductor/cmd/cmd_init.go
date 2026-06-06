package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"
)

// initProfile names one of the SPEC §3.2 deployment profiles the scaffold can
// target. Each profile selects a template set written by `conductor init`.
type initProfile string

const (
	// profileLocal scaffolds a single HARNESS.md for local single-binary use.
	profileLocal initProfile = "local"
	// profileTeam adds a docker-compose.yml + Dockerfile for a shared deploy.
	profileTeam initProfile = "team"
	// profileCloud adds the same deploy stub plus a cloud deployment note.
	profileCloud initProfile = "cloud"
)

// newInitCommand wires `conductor init --profile local|team|cloud` (SPEC §19.1,
// §3.2). It scaffolds a starter HARNESS.md into the current directory — and, for
// team/cloud, a deployment stub — without overwriting existing files unless
// --force is given.
func newInitCommand() *cobra.Command {
	var profile string
	var force bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Scaffold a starter HARNESS.md (and deploy stub) for a profile",
		Long: "Write a starter HARNESS.md whose configuration passes startup " +
			"validation once secrets are supplied. The team and cloud profiles " +
			"also write a docker-compose.yml and Dockerfile deploy stub. Existing " +
			"files are never overwritten without --force.",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("init: resolve working directory: %w", err)
			}
			return runInit(cmd.OutOrStdout(), dir, initProfile(profile), force)
		},
	}
	cmd.Flags().StringVar(&profile, "profile", string(profileLocal),
		"deployment profile to scaffold (local, team, cloud)")
	cmd.Flags().BoolVar(&force, "force", false,
		"overwrite existing files")
	return cmd
}

// runInit writes the template set for the chosen profile into dir. It first
// checks every target file for the no-overwrite guard so a partial scaffold is
// never produced when one file already exists.
func runInit(out io.Writer, dir string, profile initProfile, force bool) error {
	files, err := profileTemplates(profile)
	if err != nil {
		return err
	}

	if !force {
		var existing []string
		for name := range files {
			if _, statErr := os.Stat(filepath.Join(dir, name)); statErr == nil {
				existing = append(existing, name)
			}
		}
		if len(existing) > 0 {
			sort.Strings(existing)
			return fmt.Errorf("init: %v already exist(s); pass --force to overwrite", existing)
		}
	}

	written := make([]string, 0, len(files))
	for name := range files {
		written = append(written, name)
	}
	sort.Strings(written)

	for _, name := range written {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(files[name]), 0o644); err != nil {
			return fmt.Errorf("init: write %s: %w", name, err)
		}
	}

	for _, name := range written {
		if _, werr := fmt.Fprintf(out, "wrote %s\n", name); werr != nil {
			return werr
		}
	}
	if _, werr := fmt.Fprintf(out,
		"\nscaffolded %s profile; fill in placeholder secrets, then run `conductor harness validate`\n",
		profile); werr != nil {
		return werr
	}
	return nil
}

// profileTemplates returns the filename→content map for a profile. local writes
// only HARNESS.md; team/cloud add a docker-compose.yml + Dockerfile deploy stub.
func profileTemplates(profile initProfile) (map[string]string, error) {
	switch profile {
	case profileLocal:
		return map[string]string{
			"HARNESS.md": harnessTemplate(profileLocal),
		}, nil
	case profileTeam:
		return map[string]string{
			"HARNESS.md":         harnessTemplate(profileTeam),
			"docker-compose.yml": dockerComposeTemplate(profileTeam),
			"Dockerfile":         dockerfileTemplate(),
		}, nil
	case profileCloud:
		return map[string]string{
			"HARNESS.md":         harnessTemplate(profileCloud),
			"docker-compose.yml": dockerComposeTemplate(profileCloud),
			"Dockerfile":         dockerfileTemplate(),
		}, nil
	default:
		return nil, fmt.Errorf("init: unsupported --profile %q (want local, team, or cloud)", profile)
	}
}

// harnessTemplate renders a starter HARNESS.md for the profile. The front matter
// carries every field SPEC §6.4 requires for startup validation, with secrets as
// $VAR placeholders so the scaffold validates once the operator supplies them.
// The body has the three default-pipeline role sections (planner, coder,
// verifier) so the template renderer accepts it.
func harnessTemplate(profile initProfile) string {
	return `---
project:
  id: my-project
  name: My Project
tracker:
  kind: linear
  api_key: ${LINEAR_API_KEY}
  project_slug: my-team
providers:
  default:
    provider: openrouter
    api_key: ${OPENROUTER_API_KEY}
    model: anthropic/claude-3.5-sonnet
polling:
  interval_ms: 30000
` + profileWorkspaceBlock(profile) + `---

## planner

Plan the work for {{ issue.identifier }}: {{ issue.title }}.

## coder

Implement the change for {{ issue.identifier }}.

## verifier

Verify the implementation for {{ issue.identifier }} is complete and correct.
`
}

// profileWorkspaceBlock returns the workspace front-matter block. team/cloud use
// a container-friendly absolute root; local relies on the built-in default.
func profileWorkspaceBlock(profile initProfile) string {
	switch profile {
	case profileTeam, profileCloud:
		return "workspace:\n  root: /var/lib/conductor/workspaces\n"
	default:
		return ""
	}
}

// dockerComposeTemplate renders a minimal docker-compose.yml deploy stub.
func dockerComposeTemplate(profile initProfile) string {
	return `# Conductor ` + string(profile) + ` profile deploy stub (SPEC §3.2).
# Fill in the secret values via a .env file or your orchestrator's secret store.
services:
  conductor:
    build: .
    command: ["conductor", "start"]
    ports:
      - "8080:8080"
    environment:
      LINEAR_API_KEY: ${LINEAR_API_KEY}
      OPENROUTER_API_KEY: ${OPENROUTER_API_KEY}
    volumes:
      - conductor-data:/var/lib/conductor
      - ./HARNESS.md:/app/HARNESS.md:ro
volumes:
  conductor-data:
`
}

// dockerfileTemplate renders a minimal multi-stage Dockerfile deploy stub.
func dockerfileTemplate() string {
	return `# Conductor deploy stub (SPEC §3.2). Builds the single-binary image.
FROM golang:1.25 AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -o /out/conductor ./cmd/conductor

FROM gcr.io/distroless/static-debian12
WORKDIR /app
COPY --from=build /out/conductor /usr/local/bin/conductor
EXPOSE 8080
ENTRYPOINT ["conductor"]
CMD ["start"]
`
}
