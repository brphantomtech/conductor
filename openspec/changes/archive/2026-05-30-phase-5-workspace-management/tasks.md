# Tasks — Phase 5 Workspace Management

Atomic tasks for the `internal/workspace` package. Complexity: **S** ≤ 2h, **M** ≤ 1 day,
**L** ≤ 2–3 days. IDs continue the repo's `P<n>-NN` convention.

## 1. Package scaffolding & errors

- [x] 1.1 (P5-01, S) Replace the `internal/workspace/doc.go` stub with the package doc;
  define core types: `Manager`, `Workspace`, `Layout`, hook identifiers (§14, §5.3.4).
- [x] 1.2 (P5-02, S) Add `internal/workspace/errors.go` with workspace / run-attempt
  sentinels per SPEC §23.4; string values match the spec identifiers exactly. Add a test
  asserting each §23.4 identifier maps to exactly one sentinel.

## 2. Key sanitization & layout

- [x] 2.1 (P5-03, M) Workspace key sanitization: slug derivation, rejection of traversal
  (`..`) / separators / absolute components, and root-anchored confinement check (§14.2).
- [x] 2.2 (P5-04, M) Layout creation + `.conductor/` skeleton: workspace dir,
  `.conductor/audit.jsonl` sink path, `.conductor/validation/`, `metadata.json` (§14.1).

## 3. Multi-repo support

- [x] 3.1 (P5-05, M) Materialize each `workspace.repos` entry into a stable subdirectory;
  record locations in the `Layout` (§5.3.4).

## 4. Hook runner

- [x] 4.1 (P5-06, M) `RunHook` via `os/exec` with workspace cwd, `context.WithTimeout`
  per hook, and stdout/stderr capture for the six hook types (§5.3.5).
- [x] 4.2 (P5-07, M) Timeout handling: terminate the process group (OS-appropriate) and
  classify timeout / non-zero exit to the §23.4 sentinels.

## 5. Safety invariants

- [x] 5.1 (P5-08, M) Single invariant guard enforcing the four §14.2 invariants before
  every create / remove / hook operation; removal refuses paths outside the root.

## 6. Isolation seam

- [x] 6.1 (P5-09, S) Subprocess agent-execution entry point scoped to the workspace,
  shaped so a container backend can be added behind an interface in Phase 17 (§14.3).

## 7. Audit integration

- [x] 7.1 (P5-10, S) Emit workspace lifecycle + hook-execution audit events through
  `internal/audit`; add any missing event constants per SPEC §17.2.

## 8. Tests

- [x] 8.1 (P5-11, M) Unit tests: key sanitization + path-escape rejection (Windows `\\`
  and POSIX `/`), layout/`.conductor/` creation, multi-repo checkout, hook
  success/timeout/non-zero + audit, removal confinement. Target ≥ 70% coverage for
  `internal/workspace`.
