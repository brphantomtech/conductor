# harness-loader Specification

## Purpose

The harness-loader resolves, parses, validates, and renders the `HARNESS.md` definition that
configures Conductor, and keeps it up to date via debounced hot reload with last-known-good
fallback.

## Requirements

### Requirement: HARNESS.md discovery and path resolution

The system SHALL resolve the `HARNESS.md` path using the precedence order defined in SPEC §5.1:
`--harness <path>` flag, then `CONDUCTOR_HARNESS_PATH`, then `HARNESS.md` in the current working
directory. When no file is found, startup SHALL fail with a `missing_harness_file` error.

#### Scenario: Flag wins over environment and cwd
- **WHEN** `--harness ./a/HARNESS.md` is given and `CONDUCTOR_HARNESS_PATH` and a cwd
  `HARNESS.md` also exist
- **THEN** the loader reads `./a/HARNESS.md` and records it as the Definition source

#### Scenario: No harness file found
- **WHEN** no flag, no env var, and no `HARNESS.md` in the cwd
- **THEN** resolution fails with a `missing_harness_file` error

### Requirement: Front matter and body parsing

The parser SHALL split `HARNESS.md` into optional YAML front matter (delimited by leading `---`
lines) and a Markdown body. The front matter SHALL decode to a YAML map exposed as the
Definition's front matter object; unknown keys SHALL be ignored for forward compatibility.

#### Scenario: File with front matter
- **WHEN** the file begins with `---`, a YAML block, and a closing `---`
- **THEN** the YAML map is parsed into the Definition front matter and the remaining text is the body

#### Scenario: File without front matter
- **WHEN** the file does not begin with `---`
- **THEN** the Definition front matter is an empty map and the whole file is the body

#### Scenario: Front matter is not a map
- **WHEN** the front matter decodes to a YAML scalar or sequence rather than a map
- **THEN** parsing fails with a `harness_front_matter_not_a_map` error

#### Scenario: Malformed YAML front matter
- **WHEN** the front matter block is not valid YAML
- **THEN** parsing fails with a `harness_parse_error`

### Requirement: Role section extraction

The parser SHALL split the body into prompt templates keyed by top-level `## <role>` headings.
When the body contains no `## ` headings, the entire trimmed body SHALL be assigned to the
implicit `coder` role. Section bodies SHALL be trimmed of surrounding whitespace.

#### Scenario: Multiple role sections
- **WHEN** the body contains `## planner`, `## coder`, and `## reviewer` sections
- **THEN** `prompt_templates` has one trimmed entry per role keyed by the heading name

#### Scenario: Body with no headings
- **WHEN** the body has no `## ` heading
- **THEN** the entire trimmed body is assigned to the `coder` role

### Requirement: Startup schema validation

Before the scheduling loop starts, the validator SHALL enforce the checks in SPEC §6.4: a
non-empty `project.id`; a supported `tracker.kind` with a resolvable non-empty `tracker.api_key`
and the tracker's required project identifier; a present `providers.default` with a supported
`provider` and resolvable `api_key`; valid `knowledge.store_backend`, `memory.store_backend`, and
each `docs.stores[].backend`; and that every role referenced in `routing.pipeline` and
`routing.rules[].pipeline` has a template in `prompt_templates`. A validation failure SHALL be
returned as a structured error.

#### Scenario: Valid harness passes
- **WHEN** all required fields are present and every routed role has a template
- **THEN** validation succeeds

#### Scenario: Missing required field
- **WHEN** `project.id` is absent or empty
- **THEN** validation fails with a structured error identifying the missing field

#### Scenario: Routed role without a template
- **WHEN** `routing.pipeline` lists a role that has no `## <role>` section
- **THEN** validation fails identifying the role lacking a template

### Requirement: Liquid prompt rendering with error classification

The renderer SHALL render a role's prompt template against the standard Liquid variable set
defined in SPEC §16.2. A template that fails to parse SHALL produce a `template_parse_error`. A
render that references an unknown variable or an unknown filter SHALL produce a
`template_render_error` and fail the run attempt.

#### Scenario: Render with known variables
- **WHEN** a template references `{{ issue.identifier }}` and `{{ agent_role }}` with those
  variables supplied
- **THEN** the rendered output substitutes the provided values

#### Scenario: Unknown variable
- **WHEN** a template references a variable not in the standard set
- **THEN** rendering fails with a `template_render_error`

#### Scenario: Invalid template syntax
- **WHEN** a template contains a malformed Liquid tag
- **THEN** parsing fails with a `template_parse_error`

### Requirement: Debounced hot reload with last-known-good fallback

The loader SHALL watch `HARNESS.md` with fsnotify and reload on change after a debounce window,
without restarting the process. A successful reload SHALL replace the effective Definition and
emit a `config_reloaded` audit event. A reload that fails to parse or validate SHALL retain the
last-known-good Definition, emit a `config_reload_failed` audit event, and log a structured error.
In-flight agent sessions SHALL NOT be interrupted by a reload.

#### Scenario: Valid change reloads
- **WHEN** the watched file changes to a valid harness
- **THEN** the effective Definition is replaced and a `config_reloaded` event is emitted

#### Scenario: Invalid change keeps last-known-good
- **WHEN** the watched file changes to an invalid harness
- **THEN** the previous Definition remains effective and a `config_reload_failed` event is emitted

#### Scenario: Rapid successive changes are debounced
- **WHEN** the file changes several times within the debounce window
- **THEN** the loader performs a single reload after the window settles

### Requirement: `conductor harness validate` command

The `conductor harness validate <path>` subcommand SHALL parse and validate the given
`HARNESS.md`, print structured errors for any failure, and exit non-zero when parsing or
validation fails. On success it SHALL report the file as valid and exit zero.

#### Scenario: Valid file
- **WHEN** `conductor harness validate ./HARNESS.md` runs against a valid file
- **THEN** the command reports success and exits with status 0

#### Scenario: Invalid file
- **WHEN** the file fails parsing or schema validation
- **THEN** the command prints the structured error and exits non-zero
