## Why

Phase 1 left `conductor harness validate` as a stub that only checks the file exists, and the
orchestrator has no way to turn `HARNESS.md` into the role prompts, effective config, and live
reload behavior the rest of the system depends on. Phase 2 makes `HARNESS.md` a real,
authoritative source: parsed, schema-validated, hot-reloaded, and rendered into agent prompts.
Every later phase (provider layer, router, orchestrator) reads what this phase produces, so it
is the next dependency on the critical path.

## What Changes

- Implement a front-matter + Markdown-body parser for `HARNESS.md` (SPEC §5.2) producing a
  `harness.Definition` (front matter map + per-role prompt templates).
- Implement role-section extraction: split the body on top-level `## <role>` headings; assign
  the whole body to the implicit `coder` role when no headings are present.
- Implement a Liquid template renderer over the standard variable set (SPEC §16.2), classifying
  unknown-variable and unknown-filter conditions as `template_render_error`, and bad template
  syntax as `template_parse_error`.
- Implement startup schema validation (SPEC §6.4): required `project`/`tracker`/`providers`
  fields, supported backend kinds, and that every role referenced in `routing.pipeline` /
  `routing.rules[].pipeline` has a prompt template.
- Implement an fsnotify-based watcher with debounced reload and last-known-good fallback on a
  failed reload (SPEC §6.3).
- Promote `conductor harness validate <path>` from a stub to a real validator that emits
  structured errors and a non-zero exit on failure.
- Emit `config_reloaded` and `config_reload_failed` audit events on reload outcomes.

## Capabilities

### New Capabilities
- `harness-loader`: parsing `HARNESS.md` into a typed Definition (front matter + prompt
  templates), startup and per-tick schema validation, Liquid prompt rendering with error
  classification, and debounced hot-reload with last-known-good fallback.

### Modified Capabilities
<!-- No existing capability specs in openspec/specs/ yet; nothing to modify. -->

## Impact

- **New code**: `internal/harness/{parser,loader,templates,validator,watcher,types}.go` and
  tests (already drafted on this branch; this change formalizes their contract).
- **CLI**: `cmd/conductor/cmd/harness.go` — `harness validate` becomes a full validator.
- **Audit**: new event types `config_reloaded`, `config_reload_failed` consumed by
  `internal/audit`.
- **Dependencies**: adds a Liquid template engine and `fsnotify` to `go.mod`.
- **Consumers (future)**: `internal/config` decodes its typed `Config` from the Definition front
  matter; the orchestrator/router (Phases 6–7) read `prompt_templates`. `HarnessRules` /
  `DocRefs` remain zero-value stubs owned by Phases 11–12.
