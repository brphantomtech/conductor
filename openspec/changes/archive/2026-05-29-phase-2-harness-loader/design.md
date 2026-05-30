## Context

Phase 1 produced a binary that loads config and writes audit events, but `HARNESS.md` is still
inert: `conductor harness validate` is a file-existence stub, and there is no in-memory
representation the orchestrator can use. `internal/harness` already has its package skeleton
(`doc.go`, `errors.go` with the SPEC §23.1 sentinels) and zero-value `Definition` /
`HarnessRule` / `DocRef` types from Phase 1. Phase 2 fills the package in.

Constraints that shape the design:
- The `Definition` shape must stay stable across phases — `HarnessRules` and `DocRefs` stay as
  zero-value stubs (owned by Phases 11–12) so this phase changes no struct field later phases
  depend on.
- One Liquid engine and one strictness contract must be shared by every prompt consumer.
- Reload must never tear down a working configuration on a bad edit.

Dependencies already vendored on this branch: `github.com/osteele/liquid v1.8.1` and
`github.com/fsnotify/fsnotify v1.9.0`.

## Goals / Non-Goals

**Goals:**
- Parse `HARNESS.md` into a `Definition` (front matter map + per-role prompt templates).
- Classify every failure into the SPEC §23.1 sentinel set (`missing_harness_file`,
  `harness_parse_error`, `harness_front_matter_not_a_map`, `template_parse_error`,
  `template_render_error`).
- Enforce SPEC §6.4 startup validation and expose a reusable subset for per-tick validation.
- Render role prompts with the SPEC §16.2 variable set under strict undefined-variable/filter
  rules.
- Hot-reload with fsnotify, debounce, and last-known-good fallback emitting audit events.
- Turn `conductor harness validate` into a real validator.

**Non-Goals:**
- Decoding the typed `internal/config.Config` from the front matter (config layer's job; this
  phase only produces the raw map).
- Populating `HarnessRules` (Phase 12 Enforcer) or `DocRefs` / `docs://` resolution (Phase 11).
- Building the full prompt assembly order from SPEC §16.1 (Phase 7 router); this phase ships the
  renderer and variable contract only.

## Decisions

**Single shared Liquid engine, strict mode.** `internal/harness` owns a process-wide
`*liquid.Engine` (lazy `sync.Once`) so the orchestrator, validation pipeline, and hooks all share
one filter set and one strictness contract. Undefined variables and unknown filters map to
`template_render_error`; syntax failures map to `template_parse_error`. *Alternative considered:*
a fresh engine per render — rejected because divergent filter registration would make the same
template behave differently across consumers.

**Static variable allowlist enforced two ways.** The SPEC §16.2 root-variable allowlist
(`AllowedTemplateVariables`) backs both render-time strict binding and a parse-time static check
used by the validator, so an unknown variable is caught at `harness validate` time, not only at
dispatch. *Alternative considered:* render-time only — rejected because operators want validation
to fail fast in CI.

**Parser is byte-oriented and source-agnostic.** `Parse([]byte)` is the core; the loader adds
disk reading and `Source` path bookkeeping on top. This lets the watcher (fsnotify), a future
Doc Store backend (`docs://`), and unit tests all feed bytes through the same path.

**Watcher holds last-known-good behind a lock.** The loader keeps the current `*Definition`
behind a mutex. On a debounced fsnotify event it parses+validates into a candidate; only a fully
valid candidate is swapped in (`config_reloaded`). Any failure leaves the pointer untouched and
emits `config_reload_failed`. *Alternative considered:* apply-then-rollback — rejected as more
error-prone than validate-before-swap.

**Debounce on a timer reset.** Editors emit multiple write/rename events per save; a reset timer
(coalescing window) collapses them into one reload. fsnotify rename/remove on the watched path is
handled by re-establishing the watch on the new inode.

## Risks / Trade-offs

- **Editor rename-on-save drops the watch** → re-add the watch after rename/remove events and
  re-read by path rather than holding a file handle.
- **Liquid's notion of "undefined"/"unknown filter" may not map cleanly onto the two SPEC error
  codes** → wrap the library `SourceError` and assert classification in tests so a library
  upgrade that changes messages is caught.
- **Per-tick validation drift from startup validation** → factor the field checks into one
  function and have both entry points call it; per-tick only adds connectivity concerns layered
  on top.
- **Variable allowlist falls out of sync with SPEC §16.2** → keep the allowlist in one exported
  var with a test that pins the expected set.

## Migration Plan

Additive only — no persisted data or schema changes. New audit event types `config_reloaded` /
`config_reload_failed` are appended to the registry. Rollback is reverting the branch; the
`harness validate` command degrades back to the Phase 1 stub behavior with no data impact.

## Open Questions

- None blocking. Doc Store (`docs://`) harness resolution from SPEC §5.1 step 4 is intentionally
  deferred to Phase 11 and noted as out of scope here.
