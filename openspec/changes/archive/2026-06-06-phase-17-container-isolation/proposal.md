# Phase 17 — Container Isolation & Hardening

## Why

Conductor's default agent isolation is goroutine + subprocess (Phase 5/6): fast and simple, but the
agent runs with the host's filesystem and network reach. For untrusted code, regulated environments,
or air-gapped operation, that is not enough. Phase 17 adds the opt-in Docker-container-per-workspace
isolation the SPEC describes: each agent run executes inside a container with the workspace mounted,
network disabled by default, and all external access funneled through Conductor's tool server over a
Unix socket. It also hardens the audit trail with secret redaction.

This is a **Wave B** change (see [docs/parallel-execution-plan.md](../../../docs/parallel-execution-plan.md)):
it depends only on the workspace manager (Phase 5, on `main`) and lands a new container execution
path behind config, buildable concurrently with Phases 11, 12, and 16.

Authoritative scope: [docs/phases.md](../../../docs/phases.md) → "Phase 17 — Container Isolation &
Hardening"; SPEC §14.3 (agent isolation), §21.3 (container isolation), §3.2 Profile C, §21.1
(secret redaction).

## What Changes

- **`workspace.container` config** (SPEC §14.3): a new `Container` struct on the workspace config
  (`image`, `network` default `none`, `memory_limit`, `cpu_limit`, `extra_mounts`). This is the one
  config-schema addition Phase 17 needs (the field was not pre-declared).
- **Container execution path**: when `workspace.container` is set, an agent run launches a Docker
  container with the workspace mounted (read/write only its mount), resource limits applied, and
  `network: none` by default. Selected behind a `ContainerRunner` abstraction so the default
  subprocess path is untouched when no container is configured.
- **Conductor tool server over Unix socket** (SPEC §21.3): a host-side socket mounted into the
  container so the agent's tool calls reach Conductor while the container has no direct network.
  (Tool *dispatch* is Phase 13; this phase provides the socket transport/plumbing the container
  needs.)
- **Cloud profile (Profile C)** (SPEC §3.2): a `Dockerfile` for the Conductor binary and a sample
  Helm chart / deployment manifest.
- **Secret redaction in audit events** (SPEC §21.1): known secret fields (api keys, tokens) are
  redacted in audit payloads before they are written.
- **Unit/integration tests**: the container runner behind a fake Docker client (no live Docker in
  CI), config defaulting (`network: none`), the subprocess path unchanged when no container config,
  and audit secret-redaction. A build-tagged or skip-when-unavailable live Docker smoke is optional.

## Capabilities

### New Capabilities

- `container-isolation`: the `workspace.container` config, the Docker-container-per-workspace
  execution path with workspace mount + resource limits + `network: none`, the Unix-socket tool
  transport plumbing, the Profile C Dockerfile/Helm artifacts, and audit secret redaction.

### Modified Capabilities

None as a separate spec delta — the container path is additive and selected by config; the default
subprocess isolation (`workspace-management`) is unchanged when `workspace.container` is absent.

## Impact

- **Affected specs**: new capability `container-isolation`.
- **Affected code**: `internal/workspace/` (container runner alongside the existing subprocess path;
  `AgentCommand` gains a container-backed sibling selected by config). `internal/config/types.go`
  (add the `Container` struct to `Workspace`). `internal/audit/` (secret redaction in the writer).
  New deploy artifacts under `deploy/` or `build/` (Dockerfile, Helm chart).
- **Integration points (last commit only)** per the Wave B integration-commit rule:
  - `internal/config/types.go` — add the `Container` field (isolated; no other Wave B phase touches
    workspace config).
  - `cmd/conductor/cmd/start.go` — no new wiring required beyond what the workspace manager already
    receives; container selection is internal to the manager.
  - `AGENTS.md` — navigation note.
- **Consumes (unchanged)**: `internal/workspace` (layout, hooks, safety invariants),
  `internal/config`, `internal/audit`.

### Non-goals

- Conductor tool *dispatch* (the actual tool implementations) — Phase 13. This phase provides the
  Unix-socket transport the container relies on, not the tools themselves.
- The full production Helm chart / cloud deployment automation — a sample chart + Dockerfile only.
- Non-Docker runtimes (Podman, containerd, gVisor, Firecracker) — Docker only for v0.1; the
  `ContainerRunner` abstraction leaves room for others later.
- Approval policies and dashboard auth (SPEC §21.2/§21.4) — separate concerns (Phase 13/14).
