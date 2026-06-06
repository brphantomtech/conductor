## Context

Phase 17 adds opt-in Docker isolation on top of the Phase 5 workspace manager. It depends only on
substrate on `main`: `internal/workspace` (layout, hooks, `AgentCommand`, the §14.2 safety
invariants), `config.Workspace`, and `audit.Writer`. The default isolation today is goroutine +
subprocess (SPEC §14.3); this phase makes container-per-workspace a config-selected alternative
without disturbing the default.

The one schema addition is `workspace.container` — it was not pre-declared in
[internal/config/types.go](../../../internal/config/types.go) (unlike the other config sections), so
Phase 17 adds the `Container` struct. No other Wave B phase touches workspace config, so this edit is
conflict-free within the wave.

SPEC anchors: §14.3 (isolation modes + container config shape), §21.3 (container security: no
network, socket-only external access, mount-scoped FS, resource limits), §3.2 Profile C (cloud
deployment), §21.1 (secret redaction). Conventions to match: constructor + Options, injected client
for the container runtime so CI needs no Docker, recorded fixtures, audit via the existing writer.

This is a Wave B worktree; container code lives in `internal/workspace`, redaction in
`internal/audit`, the config field is isolated, and deploy artifacts are new files.

## Goals / Non-Goals

**Goals:**

- A `ContainerRunner` abstraction and a Docker implementation behind an injected client.
- Container-per-workspace execution: workspace mounted, resource limits, `network: none` default.
- Unix-socket transport plumbing so an in-container agent reaches the host tool server.
- Profile C Dockerfile + sample Helm chart.
- Secret redaction in audit events.
- The default subprocess path completely unchanged when `workspace.container` is unset.

**Non-Goals:**

- Tool dispatch (Phase 13); full production Helm; non-Docker runtimes; approval policies / dashboard
  auth.

## Decisions

### `ContainerRunner` interface, Docker impl behind an injected client

The workspace manager gains a `ContainerRunner` selected when `workspace.container` is set;
otherwise the existing subprocess `AgentCommand` path runs unchanged. The Docker implementation uses
an injected Docker client interface (create/start/attach/wait/remove).

- **Why:** SPEC §14.3 names "Docker container per workspace (opt-in)". An interface keeps the default
  path untouched and lets tests use a fake client — no live Docker in CI. The runtime selection is
  internal to the manager, so `start.go` wiring is unchanged.

### Air-gapped by default: `network: none`, mount-scoped, resource-limited

Containers launch with `network: none` unless overridden, only the workspace directory mounted
(read/write), and `memory_limit`/`cpu_limit` applied (SPEC §21.3).

- **Why:** verbatim SPEC §21.3; the secure default is the whole point — the agent cannot make
  external calls except through Conductor. `extra_mounts` is opt-in for explicit exceptions.

### Host tool server reachable via a mounted Unix socket

A host-side Unix socket is mounted into the container; the in-container agent directs tool calls to
it. This phase delivers the socket transport and mount; the tools themselves are Phase 13.

- **Why:** SPEC §21.3 ("tool server runs on the host, accessible via a Unix socket mounted into the
  container") — with `network: none`, the socket is the only channel out, which is the security
  model. Delivering the transport now means Phase 13 plugs tools into an existing pipe.

### Secret redaction at the audit writer boundary

A redaction step in the audit writer scrubs known secret fields (api_key, token, password, and
`$VAR`-resolved values) from payloads before any sink writes them.

- **Why:** SPEC §21.1 ("secrets never written to ... logs"; "audit events ... redact known secret
  fields"). Doing it at the writer boundary covers every sink (DB, JSONL) uniformly and is testable
  in isolation.

### Profile C as sample artifacts, not automation

A `Dockerfile` building the Conductor binary and a sample Helm chart / manifest under `deploy/`.

- **Why:** SPEC §3.2 Profile C calls for a cloud profile; a sample (not full automation) is the
  phase-plan scope, giving operators a starting point.

## Risks / Trade-offs

- **[No Docker in CI]** → The Docker runner is behind an injected client; all unit tests use a fake.
  A live-Docker smoke is build-tagged / skipped when the daemon is absent, so CI stays green on
  machines without Docker.
- **[Default path regressions]** → Container selection is gated on `workspace.container != nil`; when
  unset, the exact Phase 5/6 subprocess path runs. A test asserts the subprocess path is chosen and
  behaves identically when no container config is present.
- **[Windows/Docker variance]** → The runner targets the Docker Engine API via the client interface;
  platform-specific socket/mount details are isolated in the Docker impl, and tests exercise the
  abstraction, not a live daemon.
- **[Over-redaction hiding useful audit data]** → Redaction targets a known secret-field allowlist and
  `$VAR`-resolved secret values, not arbitrary fields; tested to redact secrets while preserving
  non-secret payload keys.
- **[Socket security]** → The mounted socket is the only egress under `network: none`; its host-side
  permissions restrict access to the Conductor process. Full tool authz is Phase 13.

## Migration Plan

Additive plus one isolated config-field addition (`workspace.container`) and an audit-writer
redaction step. New container files in `internal/workspace`, redaction in `internal/audit`, deploy
artifacts under `deploy/`. The config field and `AGENTS.md` note land in the final commit per the
Wave B integration-commit rule. Rollback is reverting the container files, the config field, and the
redaction step. With `workspace.container` unset (the default), behavior is identical to before this
phase; redaction is always-on but only affects known secret fields.

## Open Questions

- **Docker client dependency** — the official `docker/docker` SDK vs. a thin Engine-API HTTP client;
  start behind the `ContainerRunner` interface so the concrete dependency is decided in
  implementation without affecting tests.
- **Socket protocol** — reuse the eventual tool-server framing (Phase 13) vs. a placeholder now;
  deliver the transport/mount and a minimal framing, aligning with Phase 13 when it lands.
- **Helm chart depth** — minimal Deployment+Service+ConfigMap vs. a richer chart; start minimal under
  `deploy/helm/` and expand if a real cloud deployment drives requirements.
