# container-isolation Specification

## Purpose
TBD - created by archiving change phase-17-container-isolation. Update Purpose after archive.
## Requirements
### Requirement: Container Configuration

The workspace configuration SHALL support an optional `workspace.container` block (SPEC §14.3) with
`image`, `network` (defaulting to `none`), `memory_limit`, `cpu_limit`, and `extra_mounts`. When the
block is absent, agent runs SHALL use the default goroutine + subprocess isolation unchanged.

#### Scenario: Container config defaults to no network

- **WHEN** a `workspace.container` block is provided without a `network` value
- **THEN** the effective network mode is `none`

#### Scenario: Absent container config uses the subprocess path

- **WHEN** no `workspace.container` block is configured
- **THEN** agent runs use the existing subprocess isolation path unchanged

### Requirement: Container Execution Path

When `workspace.container` is configured, an agent run SHALL execute inside a Docker container with
the workspace directory mounted as its only writable mount, with the configured resource limits
applied, and with networking disabled by default (SPEC §21.3). The container runtime SHALL be
accessed through an abstraction so tests can run without a live Docker daemon.

#### Scenario: Agent run launches a container with the workspace mounted

- **WHEN** an agent run starts with a container configured
- **THEN** a container is launched from the configured image with the workspace mounted and the
  resource limits applied

#### Scenario: Container has no network by default

- **WHEN** a container-isolated agent run starts with the default network mode
- **THEN** the container is created with networking disabled

### Requirement: Host Tool Transport over Unix Socket

When running container-isolated, Conductor SHALL expose a host-side Unix socket mounted into the
container so the in-container agent can reach the host tool server while the container itself has no
external network access (SPEC §21.3).

#### Scenario: Socket is mounted into the container

- **WHEN** a container-isolated agent run starts
- **THEN** a host Unix socket is mounted into the container as the agent's channel to Conductor

### Requirement: Audit Secret Redaction

The audit writer SHALL redact known secret fields (such as API keys and tokens) and
configuration-resolved secret values from event payloads before any sink persists them (SPEC §21.1),
while preserving non-secret payload fields.

#### Scenario: Secret fields are redacted before persistence

- **WHEN** an audit event payload contains a known secret field
- **THEN** the persisted event has that field redacted and its non-secret fields intact

### Requirement: Cloud Deployment Artifacts

The project SHALL provide Profile C deployment artifacts (SPEC §3.2): a Dockerfile that builds the
Conductor binary and a sample Helm chart or deployment manifest.

#### Scenario: Dockerfile builds the binary

- **WHEN** the provided Dockerfile is built
- **THEN** it produces an image containing the Conductor binary

#### Scenario: Sample deployment manifest is provided

- **WHEN** an operator looks for a cloud deployment starting point
- **THEN** a sample Helm chart or deployment manifest is present in the repository

