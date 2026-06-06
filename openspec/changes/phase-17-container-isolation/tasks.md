# Phase 17 — Atomic Tasks (Container Isolation & Hardening)

Phase 17 goal (per [docs/phases.md](../../../docs/phases.md)): air-gapped agent execution profile +
security hardening. SPEC §14.3, §21.3, §3.2 Profile C, §21.1. Wave B worktree — container code in
`internal/workspace`, redaction in `internal/audit`, the isolated `workspace.container` config field
and `AGENTS.md` note in the final commit (see
[docs/parallel-execution-plan.md](../../../docs/parallel-execution-plan.md)). Each task is sized for
one focused session.

## 1. Container Config

- [ ] 1.1 Add the `Container` struct (`image`, `network` default `none`, `memory_limit`, `cpu_limit`, `extra_mounts`) to `config.Workspace` in `internal/config/types.go`; default `network` to `none` in `config.Defaults()` when a container block is present.
- [ ] 1.2 Unit tests: container config decodes; `network` defaults to `none`; absent block leaves workspace config as before.

## 2. Container Runner

- [ ] 2.1 Define a `ContainerRunner` interface and an injected Docker client interface (create/start/attach/wait/remove).
- [ ] 2.2 Implement the Docker runner: launch from `image`, mount the workspace as the only writable mount, apply `memory_limit`/`cpu_limit`, set `network: none` by default, honor `extra_mounts`.
- [ ] 2.3 Wire selection into the workspace manager: when `workspace.container` is set, the agent run uses the container runner; otherwise the existing subprocess `AgentCommand` path is unchanged.
- [ ] 2.4 Unit tests (fake Docker client): container launched with workspace mount + limits + no network; absent config → subprocess path selected and unchanged.

## 3. Host Tool Socket Transport

- [ ] 3.1 Implement the host-side Unix socket and mount it into the container as the agent's channel to Conductor (transport/plumbing only; tool dispatch is Phase 13).
- [ ] 3.2 Unit tests: socket created and mounted into the container spec (via fake client); minimal framing round-trip.

## 4. Audit Secret Redaction

- [ ] 4.1 Implement redaction at the audit writer boundary: scrub known secret fields (api_key, token, password, etc.) and config-resolved secret values from payloads before sinks persist them.
- [ ] 4.2 Unit tests: secret fields redacted in persisted events; non-secret fields preserved; covers DB and JSONL sinks.

## 5. Profile C Artifacts

- [ ] 5.1 Add a `Dockerfile` building the Conductor binary (multi-stage, matching the Makefile build flags).
- [ ] 5.2 Add a sample Helm chart / deployment manifest under `deploy/` (minimal Deployment + Service + ConfigMap).
- [ ] 5.3 Verify the Dockerfile builds (or document the build command if Docker is unavailable in this environment).

## 6. Integration & Verification

- [ ] 6.1 **Integration commit (final):** the `workspace.container` config field, the manager selection wiring, and `AGENTS.md` navigation note.
- [ ] 6.2 `go build ./...` and `go vet ./...` clean; repo linter passes for the new `internal/workspace` and `internal/audit` code; existing workspace/audit tests still green.
- [ ] 6.3 `go test ./internal/workspace/... ./internal/audit/... ./internal/config/...` passes; new code coverage ≥ 70%. Live-Docker smoke is build-tagged / skipped when the daemon is absent.
