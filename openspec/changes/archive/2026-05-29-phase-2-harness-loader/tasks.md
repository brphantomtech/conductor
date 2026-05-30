# Phase 2 — Harness Loader: Tasks

Tasks build `internal/harness` and wire `conductor harness validate`. Complexity: **S** ≤ 2h,
**M** ≤ 1 day. Ordered by dependency.

## 1. Dependencies & types

- [x] 1.1 Promote `github.com/osteele/liquid` and `github.com/fsnotify/fsnotify` from indirect to direct in `go.mod`; run `go mod tidy` (S)
- [x] 1.2 Confirm `Definition`, `HarnessRule`, `DocRef`, `BuiltinRoles`, `DefaultRole` in `internal/harness/types.go` match SPEC §4.1.2 / §5.2; keep `HarnessRules`/`DocRefs` as zero-value stubs (S)
- [x] 1.3 Confirm error sentinels in `internal/harness/errors.go` cover SPEC §23.1 (`missing_harness_file`, `harness_parse_error`, `harness_front_matter_not_a_map`, `template_parse_error`, `template_render_error`) (S)

## 2. Parser (`parser.go`)

- [x] 2.1 Implement `Parse([]byte) (*Definition, error)`: detect leading `---`, extract YAML front matter, return remaining body (M)
- [x] 2.2 Decode front matter to a YAML map; non-map → `harness_front_matter_not_a_map`; invalid YAML → `harness_parse_error`; ignore unknown keys (S)
- [x] 2.3 Implement role-section extraction: split body on top-level `## <role>` headings into trimmed `PromptTemplates`; no headings → whole body to `coder` (M)
- [x] 2.4 `parser_test.go`: front matter present/absent, non-map front matter, malformed YAML, multi-role split, no-heading → coder, whitespace trimming (M)

## 3. Loader & discovery (`loader.go`)

- [x] 3.1 Implement path resolution per SPEC §5.1: `--harness` flag → `CONDUCTOR_HARNESS_PATH` → cwd `HARNESS.md`; missing → `missing_harness_file` (S)
- [x] 3.2 Implement `Load(path)` that reads bytes, calls `Parse`, and sets `Definition.Source` (S)
- [x] 3.3 `loader_test.go`: precedence ordering, missing-file error, source path recorded (M)

## 4. Liquid renderer (`templates.go`)

- [x] 4.1 Provide the shared lazy `Engine()` (`sync.Once`) and `AllowedTemplateVariables` allowlist per SPEC §16.2 (S)
- [x] 4.2 Implement strict render: parse error → `template_parse_error`; unknown variable/filter → `template_render_error`, wrapping the liquid `SourceError` (M)
- [x] 4.3 Implement a parse-time static variable check reusable by the validator (S)
- [x] 4.4 `templates_test.go`: known-variable render, unknown variable, unknown filter, malformed tag, error-classification assertions (M)

## 5. Validator (`validator.go`)

- [x] 5.1 Implement SPEC §6.4 field checks: `project.id`; `tracker.kind`+`api_key`+project id; `providers.default`+`provider`+`api_key`; `knowledge`/`memory`/`docs[].backend` kinds (M)
- [x] 5.2 Implement routed-role check: every role in `routing.pipeline` and `routing.rules[].pipeline` has a `PromptTemplates` entry (S)
- [x] 5.3 Factor checks so a per-tick subset (parseable + key presence) reuses the same code path (S)
- [x] 5.4 `validator_test.go`: valid passes, each missing field fails with structured error, routed role without template fails (M)

## 6. Hot-reload watcher (`watcher.go`)

- [x] 6.1 Implement fsnotify watch on the resolved path with a debounce/coalescing timer (M)
- [x] 6.2 Implement validate-before-swap: parse+validate candidate, swap behind a mutex on success, retain last-known-good on failure (M)
- [x] 6.3 Emit `config_reloaded` / `config_reload_failed` audit events; register both event types in `internal/audit` (S)
- [x] 6.4 Handle rename/remove by re-establishing the watch on the new inode (S)
- [x] 6.5 `watcher_test.go`: valid reload swaps + emits event, invalid reload keeps last-known-good + emits failure, rapid edits debounce to one reload (M)

## 7. CLI wiring (`cmd/conductor/cmd/harness.go`)

- [x] 7.1 Replace the Phase 1 `harness validate` stub with real parse+validate; print structured errors; non-zero exit on failure, zero on success (M)
- [x] 7.2 Add/adjust command test asserting exit codes and error output for valid and invalid files (S)

## 8. Verification

- [x] 8.1 `go test ./internal/harness/... ./cmd/conductor/...` green; coverage ≥ 70% for `internal/harness` (S)
- [x] 8.2 `golangci-lint run` clean; `go vet ./...` clean (S)
- [x] 8.3 Manual smoke: `conductor harness validate` against a valid and an intentionally broken `HARNESS.md` shows expected exit codes (S)
