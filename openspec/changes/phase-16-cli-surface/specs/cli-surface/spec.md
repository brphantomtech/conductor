## ADDED Requirements

### Requirement: Workspace Management Commands

The CLI SHALL provide `conductor workspace list`, `conductor workspace remove <key>`, and
`conductor workspace open <key>` (SPEC §19.1). `list` SHALL enumerate workspaces under
`workspace.root` reporting each workspace's key, issue, and path. `remove` SHALL delete a workspace
through the workspace manager so `before_remove` hooks and the SPEC §14.2 safety invariants run, and
SHALL require confirmation for non-interactive deletion. `open` SHALL launch the workspace in
`$EDITOR`, falling back to printing the resolved path when `$EDITOR` is unset.

#### Scenario: list reports existing workspaces

- **WHEN** `conductor workspace list` runs with workspaces present under `workspace.root`
- **THEN** each workspace is listed with its key and path

#### Scenario: remove deletes via the manager

- **WHEN** `conductor workspace remove <key> --yes` runs for an existing workspace
- **THEN** the workspace is removed through the manager (running `before_remove`) and no longer
  appears in `list`

#### Scenario: open without EDITOR prints the path

- **WHEN** `conductor workspace open <key>` runs and `$EDITOR` is unset
- **THEN** the resolved workspace path is printed instead of failing

### Requirement: Project Initialization

The CLI SHALL provide `conductor init --profile local|team|cloud` (SPEC §19.1, §3.2) that scaffolds
a starter `HARNESS.md` into the current directory — and, for `team`/`cloud`, a deployment stub
(`docker-compose.yml` / Dockerfile) — without overwriting existing files unless `--force` is given.
The scaffolded `HARNESS.md` SHALL contain the configuration fields required for startup validation.

#### Scenario: init scaffolds a local profile

- **WHEN** `conductor init --profile local` runs in an empty directory
- **THEN** a `HARNESS.md` is written whose configuration passes startup validation once secrets are
  supplied

#### Scenario: init does not overwrite without force

- **WHEN** `conductor init` runs in a directory that already has `HARNESS.md`
- **THEN** the existing file is preserved and the command reports that `--force` is required

### Requirement: Dispatch, Cancel, and Status Commands

The CLI SHALL provide `conductor dispatch <id>`, `conductor cancel <id>`, and `conductor status`
(SPEC §19.1). For operations that require a running service control channel (live dispatch/cancel and
the live runtime snapshot, delivered in a later phase), the commands SHALL report a clear, structured
message that the running service is required rather than silently doing nothing. `conductor status`
SHALL print the statically derivable snapshot — configuration summary, workspace inventory, and
database/audit reachability — when no running service is available.

#### Scenario: status prints the static snapshot

- **WHEN** `conductor status` runs with no running service
- **THEN** it prints the configuration summary, workspace inventory, and storage reachability

#### Scenario: dispatch without a running service reports the requirement

- **WHEN** `conductor dispatch <id>` runs and no running service control channel is available
- **THEN** it reports a structured message that the running service is required rather than silently
  succeeding
