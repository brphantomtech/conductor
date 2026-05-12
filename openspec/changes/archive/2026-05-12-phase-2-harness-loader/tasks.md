## 1. Dependencies and Module Setup

- [x] 1.1 Add `github.com/osteele/liquid` to `go.mod` and verify the version against the current upstream tag
- [x] 1.2 Add `github.com/fsnotify/fsnotify` to `go.mod` and verify cross-platform support (Windows + POSIX)
- [x] 1.3 Run `go mod tidy` and check `go.sum` diff for unexpected transitive dependencies

## 2. Discovery (`internal/harness/discovery.go`)

- [x] 2.1 Define `type Source string` with constants `SourceFlag`, `SourceEnv`, `SourceCwd`, `SourceDocStore`
- [x] 2.2 Implement `Resolve(flag string, env func(string) (string, bool), cwd string) (path string, source Source, err error)` honoring SPEC §5.1 precedence
- [x] 2.3 Implement the doc-store stub that always returns `ErrMissingHarnessFile` with a "Phase 11" comment
- [x] 2.4 Write `discovery_test.go` covering: flag wins, env wins over cwd, cwd fallback, no file at any source returns `ErrMissingHarnessFile`

## 3. Parser (`internal/harness/parser.go`)

- [x] 3.1 Define `type Definition struct { FrontMatter map[string]any; PromptTemplates map[string]string; SourcePath string }`
- [x] 3.2 Implement `Parse(path string) (*Definition, error)` that reads the file and delegates to `parseBytes`
- [x] 3.3 Implement `parseBytes(b []byte, sourcePath string) (*Definition, error)`: detect leading `---` delimiter, extract front matter between the two `---` lines, leave the rest as body
- [x] 3.4 YAML-decode the front matter with `gopkg.in/yaml.v3` into `map[string]any`; non-map roots → `ErrHarnessFrontMatterShape`
- [x] 3.5 Split the body by top-level `## <role>` headings; assign trimmed section bodies to `PromptTemplates[<role>]`
- [x] 3.6 If the body contains no `##` heading, assign the trimmed body to `PromptTemplates["coder"]`
- [x] 3.7 Wrap any I/O or YAML failure with `ErrHarnessParse` so callers can `errors.Is`-match
- [x] 3.8 Write `parser_test.go` covering each scenario in `specs/harness-loader/spec.md` under "Front matter + prompt template parsing"

## 4. Renderer (`internal/harness/renderer.go`)

- [x] 4.1 Wrap `liquid.NewEngine()` in a `type Renderer struct { engine *liquid.Engine; allowed map[string]struct{} }`
- [x] 4.2 Build the allowed-variable set from SPEC §16.2 (`issue.*`, `attempt`, `agent_role`, `pipeline`, `pipeline_index`, `pipeline_length`, `memory_summary`, `knowledge_summary`)
- [x] 4.3 Implement `Compile(name, source string) (*Template, error)` returning a struct that bundles the compiled Liquid template; classify compile failures as `ErrTemplateParse`
- [x] 4.4 Implement `(*Template).Render(vars map[string]any) (string, error)`: pre-walk the AST to reject unknown variable paths against the allowlist, then render; classify failures as `ErrTemplateRender`
- [x] 4.5 Ensure standard filters (`downcase`, `upcase`, `default`, `date`, `truncate`, `strip`, `replace`) are available and unknown filters fail
- [x] 4.6 Write `renderer_test.go` covering: known-variable render, unknown variable → ErrTemplateRender, unknown filter → ErrTemplateParse or ErrTemplateRender, syntax error → ErrTemplateParse, `downcase` filter happy path

## 5. Validator (`internal/harness/validator.go`)

- [x] 5.1 Define `Validate(def *Definition, cfg config.Config) error` returning `errors.Join` over all collected problems
- [x] 5.2 Reuse the SPEC §6.4 checks already in `config.Validate` (call it directly so we do not duplicate logic)
- [x] 5.3 Add additional checks: every role in `cfg.Routing.Pipeline` exists as a key in `def.PromptTemplates`; every role in `cfg.Routing.Rules[*].Pipeline` exists in `def.PromptTemplates`
- [x] 5.4 Compile-test every entry in `def.PromptTemplates` via the renderer; surface `ErrTemplateParse` problems with the role name as context
- [x] 5.5 Add per-check sentinel error wrappers in `internal/harness/errors.go` so the CLI can classify them (e.g., `ErrPipelineRoleMissingTemplate`)
- [x] 5.6 Write `validator_test.go` covering: clean case returns nil, missing project.id reported, multiple problems joined, pipeline-role-without-template reported

## 6. Loader (`internal/harness/loader.go`)

- [x] 6.1 Define `Load(opts LoadOptions) (*Definition, config.Config, error)` where `LoadOptions{ Flag, Env, Cwd, CLIFlags *pflag.FlagSet }`
- [x] 6.2 Pipeline: `Resolve` → `Parse` → `config.Load(LoadOptions{FrontMatter: def.FrontMatter, Flags: opts.CLIFlags})` → `Validate(def, cfg)`
- [x] 6.3 Surface the resolved `Source` in audit/log context so operators can see which discovery branch won
- [x] 6.4 Write `loader_test.go` covering: env-only flow, CLI-flag flow, validation failure short-circuits with the joined error

## 7. Watcher (`internal/harness/watcher.go`)

- [x] 7.1 Define `type Watcher struct { path string; debounce time.Duration; live *atomic.Pointer[Definition]; writer auditWriter; log zerolog.Logger }`
- [x] 7.2 Implement `NewWatcher(path string, initial *Definition, writer auditWriter, log zerolog.Logger) *Watcher` with `debounce = 250*time.Millisecond`
- [x] 7.3 Implement `(*Watcher).Run(ctx context.Context) error`: watch `filepath.Dir(path)`, filter events to `filepath.Base(path)`, coalesce within the debounce window
- [x] 7.4 On a coalesced event: re-`Parse` + re-`Validate`; on success → `live.Store(newDef)` + emit `EventConfigReloaded`; on failure → keep last known good + emit `EventConfigReloadFailed`
- [x] 7.5 Provide `(*Watcher).Live() *Definition` for downstream readers
- [x] 7.6 Define an `auditWriter` interface inside `internal/harness` so the watcher does not import `internal/audit` directly (consumer-defined interface per `docs/conventions.md`)
- [x] 7.7 Write `watcher_test.go` covering: clean reload emits ConfigReloaded, parse failure preserves last known good + emits ConfigReloadFailed, validation failure preserves last known good, bursty writes coalesce to at most one reload

## 8. CLI Rewrite (`cmd/conductor/cmd/harness.go`)

- [x] 8.1 Replace the Phase 1 stub body of `newHarnessValidateCommand` with a call to `harness.Load(...)`
- [x] 8.2 On error, walk the joined error with `errors.As` against the harness sentinels and print one categorized line per underlying problem to stderr; exit with code 1
- [x] 8.3 On success, print `"OK: <path> (front matter <N> keys, <M> templates)"` to stdout
- [x] 8.4 Update `harness.go` doc comment to remove the "parser stub" language
- [x] 8.5 Write `harness_test.go` covering: missing file exits non-zero, valid file exits 0, multi-error file prints all problems

## 9. `conductor start` Integration

- [x] 9.1 In `cmd/conductor/cmd/start.go`, replace direct `config.Load(...)` with `harness.Load(...)` to pick up front matter
- [x] 9.2 On `--dry-run`: report the resolved harness source + path + template role list in the dry-run summary line
- [x] 9.3 On non-dry-run: construct a `harness.Watcher`, attach an `auditWriter` shim around the existing `audit.Writer`, and launch `Watcher.Run(ctx)` as a goroutine tied to `cmd.Context()`
- [x] 9.4 Update `start_test.go` (create if absent) covering: dry-run reports template count, validation failures cause non-zero exit on non-dry-run

## 10. Audit Event Emission

- [x] 10.1 Confirm `EventConfigReloaded` and `EventConfigReloadFailed` already exist in `internal/audit/event.go` (they do — Phase 1 wired the registry)
- [x] 10.2 Define the payload shape used by the watcher in a constant or local type so future readers can rely on it (`{"path": ..., "source": ..., "templates": [...]}` for reloaded; `{"path": ..., "error_code": ..., "error_message": ...}` for failed)
- [x] 10.3 Add a watcher-level test that captures emitted events in an in-memory sink and asserts the payload shape

## 11. Documentation and Phase 2 TASKS

- [x] 11.1 Update `AGENTS.md` Phase 2 note (currently links to `docs/phases.md` only) — add a one-line link to the new `internal/harness` package once it ships
- [x] 11.2 Add a Phase 2 atomic-task table to `TASKS.md` mirroring the Phase 1 format (ID, title, spec section, complexity, depends-on)
- [x] 11.3 Ensure `internal/harness/doc.go` retains the package overview; expand it to mention the four files added in this phase

## 12. Verification

- [x] 12.1 Run `make test` and confirm all unit tests pass on Windows + POSIX (CI will cover Linux) — `go test ./...` green on Windows
- [ ] 12.2 Run `golangci-lint run ./...` and clear any new findings; verify depguard still rejects upward imports — linter not installed in this env; deferred
- [x] 12.3 Manually run `conductor harness validate ./HARNESS.md` against a sample file and confirm both success and error paths produce the expected stdout/stderr
- [x] 12.4 Manually run `conductor start --dry-run` with a valid HARNESS.md and confirm the dry-run summary now reports template count and resolved source
- [x] 12.5 Coverage check: `go test -cover ./internal/harness/...` ≥ 75% for the new package (achieved 84.4%)
