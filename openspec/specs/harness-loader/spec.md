# harness-loader Specification

## Purpose
TBD - created by archiving change phase-2-harness-loader. Update Purpose after archive.
## Requirements
### Requirement: HARNESS.md path discovery
The system SHALL resolve the HARNESS.md path using the following precedence, returning the first source whose file exists: (1) the `--harness <path>` CLI flag, (2) the `CONDUCTOR_HARNESS_PATH` environment variable, (3) `HARNESS.md` in the current working directory, (4) a Doc Store reference (deferred — currently a no-op that returns `missing_harness_file`).

#### Scenario: CLI flag wins over environment
- **WHEN** `--harness /etc/conductor/team.md` is passed and `CONDUCTOR_HARNESS_PATH=/other/path.md` is set
- **THEN** the resolver returns `/etc/conductor/team.md`

#### Scenario: Environment wins over cwd file
- **WHEN** no CLI flag is passed, `CONDUCTOR_HARNESS_PATH=/etc/foo.md` is set, and `./HARNESS.md` exists in the working directory
- **THEN** the resolver returns `/etc/foo.md`

#### Scenario: Cwd fallback when neither flag nor env is set
- **WHEN** no CLI flag is passed, no env var is set, and `./HARNESS.md` exists in the working directory
- **THEN** the resolver returns the cwd path

#### Scenario: No discoverable file returns missing_harness_file
- **WHEN** no CLI flag, no env var, no cwd file, and the doc-store resolver is the stub
- **THEN** the resolver returns an error that satisfies `errors.Is(err, harness.ErrMissingHarnessFile)`

### Requirement: Front matter + prompt template parsing
The system SHALL parse a HARNESS.md file by extracting optional YAML front matter delimited by `---` lines at the top, then splitting the remaining Markdown body into prompt template sections keyed by their top-level `## <role>` headings. If no `##` heading is present in the body, the entire body SHALL be assigned to the `coder` role.

#### Scenario: Full HARNESS.md with front matter and four roles
- **WHEN** parsing a file that starts with `---\nproject:\n  id: foo\n---\n\n## planner\n…\n## coder\n…\n## verifier\n…\n## reviewer\n…`
- **THEN** the returned `Definition` exposes `FrontMatter["project"]["id"] == "foo"` and `PromptTemplates` contains keys `planner`, `coder`, `verifier`, `reviewer`

#### Scenario: File without front matter
- **WHEN** parsing a file whose first line is `## coder` (no `---` block)
- **THEN** `Definition.FrontMatter` is empty and `Definition.PromptTemplates["coder"]` contains the trimmed body

#### Scenario: File body without role headings
- **WHEN** parsing a file body that contains no `## <role>` heading
- **THEN** the entire body is assigned to `Definition.PromptTemplates["coder"]`

#### Scenario: Non-map front matter
- **WHEN** the front matter parses to a YAML list or scalar instead of a map
- **THEN** parsing returns an error that satisfies `errors.Is(err, harness.ErrHarnessFrontMatterShape)`

#### Scenario: Malformed front matter delimiter
- **WHEN** the file starts with `---` but no closing `---` is found before EOF
- **THEN** parsing returns an error that satisfies `errors.Is(err, harness.ErrHarnessParse)`

#### Scenario: Unknown front-matter keys are ignored
- **WHEN** the front matter contains `experimental_setting: true` alongside the known keys
- **THEN** parsing succeeds, the unknown key is preserved in `Definition.FrontMatter`, and validation does not flag it

### Requirement: Startup validation
The system SHALL validate a parsed Definition + merged Config and return all detected problems in a single error (via `errors.Join`). Validation SHALL enforce: `project.id` is non-empty; `tracker.kind` is one of the supported kinds; `tracker.api_key` is non-empty after `$VAR` expansion; `tracker.project_slug` or `tracker.project_id` is present when the tracker kind requires it; `providers.default.provider` is supported and its `api_key` is non-empty when required; `knowledge.store_backend` and `memory.store_backend` are supported; every `docs.stores[].backend` is supported; every role referenced in `routing.pipeline` and `routing.rules[].pipeline` has a corresponding entry in `Definition.PromptTemplates`.

#### Scenario: All checks pass
- **WHEN** validating a Definition+Config with non-empty project.id, valid linear tracker config, an Anthropic provider with an api_key, and a pipeline whose every role has a template
- **THEN** validation returns nil

#### Scenario: Missing project.id is reported
- **WHEN** validating a Config whose `project.id` is an empty string
- **THEN** the returned error includes a "project.id is required" entry

#### Scenario: Multiple problems are reported together
- **WHEN** validating a Config that lacks `tracker.api_key` AND uses an unsupported `providers.default.provider`
- **THEN** the returned error wraps both problems and `errors.Is(err, harness.ErrTrackerAPIKeyMissing)` is true and `errors.Is(err, harness.ErrProviderUnsupported)` is true

#### Scenario: Pipeline role without a template is reported
- **WHEN** validating a Definition whose `routing.pipeline` contains "reviewer" but whose `PromptTemplates` does not include a "reviewer" key
- **THEN** the returned error names the missing role

### Requirement: Strict Liquid template rendering
The system SHALL compile each prompt template as a Liquid template at load time and render it with a variable map containing the SPEC §16.2 standard variables. Unknown variables and unknown filters SHALL produce a render-time error that satisfies `errors.Is(err, harness.ErrTemplateRender)`. A template that fails to compile SHALL produce `errors.Is(err, harness.ErrTemplateParse)`.

#### Scenario: Render with known variables succeeds
- **WHEN** rendering `"Issue {{ issue.identifier }}: {{ issue.title }}"` with `issue.identifier="ENG-1"` and `issue.title="Hello"`
- **THEN** the result is `"Issue ENG-1: Hello"` and no error is returned

#### Scenario: Unknown variable fails rendering
- **WHEN** rendering `"{{ unknown_variable }}"` with an empty variable map
- **THEN** the error satisfies `errors.Is(err, harness.ErrTemplateRender)`

#### Scenario: Unknown filter fails rendering
- **WHEN** rendering `"{{ issue.title | not_a_real_filter }}"`
- **THEN** the error satisfies `errors.Is(err, harness.ErrTemplateRender)` or `errors.Is(err, harness.ErrTemplateParse)` depending on whether the engine catches it at compile or at render time

#### Scenario: Compile-time syntax error is classified as template_parse_error
- **WHEN** compiling a template whose body is `"{{ issue.title"`(missing `}}`)
- **THEN** compilation returns an error satisfying `errors.Is(err, harness.ErrTemplateParse)`

#### Scenario: Standard filters are available
- **WHEN** rendering `"{{ issue.identifier | downcase }}"` with `issue.identifier="ENG-42"`
- **THEN** the result is `"eng-42"`

### Requirement: Hot reload with last-known-good fallback
The system SHALL watch the resolved HARNESS.md file for changes using fsnotify and re-parse + re-validate the file when it changes. Reload events SHALL be debounced for 250 ms. On a successful reload, the system SHALL atomically swap the live Definition and emit a `ConfigReloaded` audit event. On a failed reload, the system SHALL retain the last known good Definition, leave the live pointer unchanged, and emit a `ConfigReloadFailed` audit event.

#### Scenario: Successful edit triggers ConfigReloaded
- **WHEN** the watcher is running over a valid HARNESS.md and the file is rewritten with a different valid Definition
- **THEN** after the debounce window the live Definition contains the new content and an audit event with `event_type == "ConfigReloaded"` was written

#### Scenario: Parse failure preserves last known good
- **WHEN** the watcher is running over a valid HARNESS.md and the file is rewritten with malformed YAML
- **THEN** after the debounce window the live Definition is unchanged and an audit event with `event_type == "ConfigReloadFailed"` was written with the underlying error code in its payload

#### Scenario: Validation failure preserves last known good
- **WHEN** the watcher is running over a valid HARNESS.md and the file is rewritten in a way that parses but fails schema validation (e.g., empty `project.id`)
- **THEN** the live Definition is unchanged and `ConfigReloadFailed` is emitted

#### Scenario: Bursty writes coalesce
- **WHEN** five write events fire within 100 ms of each other on the watched file
- **THEN** at most one reload is attempted and at most one `ConfigReloaded` (or `ConfigReloadFailed`) audit event is written

### Requirement: `conductor harness validate` CLI surface
The CLI SHALL expose `conductor harness validate [path]` which resolves the harness path (defaulting to `./HARNESS.md`), parses it, runs schema validation against a Config loaded with no environment-supplied secrets, and prints structured errors. The command SHALL exit with code 0 on success and non-zero on any validation failure.

#### Scenario: Valid HARNESS.md prints OK and exits 0
- **WHEN** invoking `conductor harness validate path/to/good.md` against a file that parses and validates cleanly
- **THEN** stdout contains a single line beginning with `OK` and the process exits with code 0

#### Scenario: Missing file exits non-zero with missing_harness_file
- **WHEN** invoking `conductor harness validate /does/not/exist.md`
- **THEN** stderr contains `missing_harness_file` and the process exits non-zero

#### Scenario: Multiple validation errors are all printed
- **WHEN** invoking `conductor harness validate` against a file that has both a missing `project.id` AND an unsupported provider
- **THEN** stderr contains a line for each underlying error sorted by category (file → parse → schema → templates) and the process exits non-zero

### Requirement: Config loader integration
The system SHALL hand the parsed front matter to `config.Load(LoadOptions{FrontMatter: def.FrontMatter})` so that YAML values participate in the SPEC §6.1 precedence chain alongside CLI flags, environment variables, and built-in defaults.

#### Scenario: YAML value flows into Config
- **WHEN** the parsed front matter contains `polling: { interval_ms: 5000 }` and no CLI flag or env var overrides it
- **THEN** the resulting `Config.Polling.IntervalMS == 5000`

#### Scenario: CLI flag still overrides front matter
- **WHEN** the front matter sets `polling.interval_ms: 5000` and `--polling-interval=1000` is passed on the CLI
- **THEN** `Config.Polling.IntervalMS == 1000`

#### Scenario: Environment variable overrides front matter but not CLI
- **WHEN** the front matter sets `server.port: 8080`, the environment sets `CONDUCTOR_SERVER_PORT=9090`, and the CLI passes `--port=7070`
- **THEN** `Config.Server.Port == 7070`

