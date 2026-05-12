## Why

Phase 1 produced a buildable binary that boots, loads config, writes audit events, and exits — but `conductor harness validate` is a stub that only checks the file exists, and the start command runs without any HARNESS.md parsing. Phase 2 closes that gap: a real front-matter + prompt-template parser, schema validation, fsnotify-based hot reload with last-known-good fallback, and the Liquid renderer that downstream phases (router, prompts) depend on. Without this, no later phase can read the harness contract that the whole system is built around.

## What Changes

- New `internal/harness` parser: extract YAML front matter, split the Markdown body into `## <role>` prompt-template sections, return a `Definition` containing both. Empty/no-front-matter and "body without sections → coder" cases handled per SPEC §5.2.
- Front-matter validator: enforce SPEC §5.3 + §6.4 startup rules (project.id, tracker fields, providers.default, supported backends, role-template coverage for every pipeline role).
- Liquid renderer wrapper around `osteele/liquid`: per-role compiled templates, strict mode where unknown variables and unknown filters fail with `template_render_error`. SPEC §16.2 variable set wired.
- Discovery resolver implementing SPEC §5.1 precedence (`--harness` flag → `CONDUCTOR_HARNESS_PATH` → cwd `HARNESS.md` → doc-store stub returning "not yet implemented").
- fsnotify-based `Watcher` with 250 ms debounce; on change → re-parse + re-validate + atomic swap of the live `Definition`; on failure → keep last known good + emit `ConfigReloadFailed`. Successful reload emits `ConfigReloaded`.
- Real `conductor harness validate <path>` subcommand replacing the Phase 1 stub: prints structured errors (one per failed check, all errors collected via `errors.Join`), exits non-zero on any failure.
- Wire the parsed front matter into `config.Load(opts.FrontMatter)` so Phase 1's precedence chain finally reads YAML values from HARNESS.md.
- Two new audit event types are already in the registry (`ConfigReloaded`, `ConfigReloadFailed`) — Phase 2 just emits them.

## Capabilities

### New Capabilities

- `harness-loader`: parsing HARNESS.md (front matter + prompt templates), startup validation, Liquid rendering, fsnotify-based hot reload with last-known-good fallback, and the `conductor harness validate` CLI surface.

### Modified Capabilities

None. Phase 1 did not formalize any capability specs under `openspec/specs/`, and Phase 2 only adds the harness-loader contract.

## Impact

- **Code (new files):** `internal/harness/{discovery,parser,validator,renderer,watcher,definition,loader}.go` + tests; `cmd/conductor/cmd/harness.go` rewritten.
- **Code (modified):** `cmd/conductor/cmd/start.go` (resolve + load harness before `config.Load`, register a watcher on non-dry-run runs); `internal/config/load.go` already accepts `FrontMatter` — no API change needed.
- **Audit events:** `ConfigReloaded`, `ConfigReloadFailed` (already declared; now emitted).
- **Dependencies (new):** `github.com/osteele/liquid` for templating; `github.com/fsnotify/fsnotify` for file watching. Both pure Go, MIT-licensed, fits the cgo-free constraint.
- **Backward compatibility:** Phase 1 callers that pass `LoadOptions{FrontMatter: nil}` keep working. The Phase 1 harness-validate stub message disappears (replaced by real validation output) — this is the intended deliverable, not a breaking change for any external user.
- **Out of scope (deferred):** doc-store-resolved harnesses (Phase 11), `HarnessRule` enforcement (Phase 12), continuation-prompt section handling beyond parsing (used by Phase 7 router).
