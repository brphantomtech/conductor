# Design — Phase 5 Workspace Management

## Context

`internal/workspace` is a **Tier 1** package (external adapters & infra) per
[docs/architecture.md](../../../docs/architecture.md) §1. The layering rules constrain
the implementation:

- It MAY import **Tier 0** (`internal/config`, `internal/db`) and the cross-cutting
  `internal/audit` package.
- It MUST NOT import sibling **Tier 1** packages (`internal/provider`,
  `internal/tracker`) or any higher tier (`router`, `orchestrator`, `api`, `cmd`).
- Every public method takes `context.Context` as its first argument; configuration is
  passed **by value** at construction; a `zerolog.Logger` is injected (no globals).
  (architecture.md §4 — cross-cutting concerns.)

Config and path normalization already exist from Phase 1 (`internal/config` provides
`~`/`$VAR`/separator normalization), so the workspace package consumes already-resolved
paths rather than reading the environment directly.

## Goals / Non-Goals

**Goals**
- Deterministic, root-anchored workspace layout with a `.conductor/` skeleton.
- Safe create / remove guarded by the SPEC §14.2 invariants.
- A hook runner with per-hook timeouts, captured output, and audit logging.
- Multi-repo checkout driven by `workspace.repos`.
- Subprocess isolation seam that a later phase can swap for containers.

**Non-Goals**
- Container isolation (Phase 17), orchestrator-driven invocation (Phase 6), harness rule
  evaluation (Phase 12), validation execution (Phase 8).

## Decisions

1. **`Manager` type.** Constructed with the resolved `workspace`/`hooks` config slices,
   a `zerolog.Logger`, and the audit `Writer`. Exposes `Create`, `Remove`, `RunHook`,
   and `Path`/lookup helpers. The orchestrator owns the Manager's lifetime.
2. **Key derivation.** A workspace key is derived from the issue identifier by slugging
   (lowercase, replace unsafe characters), then joined to the configured root and
   verified to remain inside the root. Traversal sequences and absolute components are
   rejected — never silently accepted.
3. **Layout.** `<root>/<key>/` containing the checked-out repo(s) plus
   `.conductor/{audit.jsonl, validation/, metadata.json}`. The per-workspace JSONL sink
   from Phase 1 (`internal/audit` JSONL sink) is pointed at `.conductor/audit.jsonl`.
4. **Hook runner.** Hooks execute via `os/exec` with the workspace as the working
   directory and a `context.WithTimeout` derived from the hook's configured timeout.
   stdout/stderr are captured; each invocation emits an audit event; non-zero exit and
   timeout are classified to the SPEC §23.4 sentinels. On timeout the process group is
   killed (OS-appropriate).
5. **Multi-repo.** Each `workspace.repos` entry is materialized into the workspace (clone
   or link, per config) under a stable subdirectory name.
6. **Invariants.** A single internal guard runs before any filesystem mutation and
   enforces the four §14.2 invariants (root-confinement for create and remove, key
   sanitization, hook-cwd confinement). Removal refuses any path that resolves outside
   the configured root.
7. **Isolation seam.** Agent execution is a subprocess scoped to the workspace now; the
   exec entry point is shaped so a container backend can be added behind an interface in
   Phase 17 without changing callers.

## Risks / Trade-offs

- **Cross-platform path & process semantics.** Windows vs POSIX separators (mitigated by
  reusing Phase 1 path normalization) and process-group kill on timeout differ per OS;
  tests cover both separator cases and the timeout path.
- **Invariant fidelity.** The §14.2 invariant text is authoritative in the SPEC; the
  spec delta and implementation must mirror it verbatim (see Open Questions).

## Migration

Additive only — no existing data, schema, or API changes.

## Open Questions

- Confirm the exact wording and count of the SPEC §14.2 safety invariants and the SPEC
  §17.2 workspace/hook audit-event names so the spec delta and `errors.go`/event
  constants match the source verbatim before `/opsx:apply`.
