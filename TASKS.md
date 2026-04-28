# Phase 1 — Atomic Tasks

Phase 1 goal (per [docs/phases.md](docs/phases.md)): produce a buildable Conductor binary
that boots, loads config, writes audit events, and exits cleanly. No agent execution yet.

Each task below is sized to be completed in one focused work session. Complexity:

- **S** — small (≤ 2h focused work)
- **M** — medium (≤ 1 day)
- **L** — large (≤ 2–3 days)

| ID    | Title                                      | Spec section(s)        | Complexity | Depends on  |
| ----- | ------------------------------------------ | ---------------------- | ---------- | ----------- |
| P1-01 | Define `internal/config` typed schema      | §5.3, §6.4             | M          | —           |
| P1-02 | Wire Viper precedence chain                | §6.1, §6.2             | M          | P1-01       |
| P1-03 | Implement `$VAR` expansion helper          | §6.2                   | S          | P1-01       |
| P1-04 | Implement `~`/path/separator normalization | §6.2                   | S          | P1-01       |
| P1-05 | Define error sentinels per SPEC §23        | §23.1–§23.5            | S          | —           |
| P1-06 | Build SQLite-backed `internal/db` core     | §3.1, §3.2 Profile A   | M          | —           |
| P1-07 | Author migration runner + initial schema   | §17.3 (audit\_events)  | M          | P1-06       |
| P1-08 | Define `AuditEvent` type + event registry  | §4.1.10, §17.2         | S          | P1-05       |
| P1-09 | Implement `internal/audit` writer (DB)     | §17.3                  | M          | P1-07,P1-08 |
| P1-10 | Implement `.conductor/audit.jsonl` sink    | §14.1, §17.3           | S          | P1-08       |
| P1-11 | Wire zerolog with `--log-level/--log-format` | §19.2               | S          | —           |
| P1-12 | Cobra root cmd + `conductor version`       | §19.1, §19.2           | S          | P1-11       |
| P1-13 | `conductor harness validate <path>` (no-op stub) | §19.1            | S          | P1-12       |
| P1-14 | `conductor start --dry-run` config-load path | §6.4, §19.2          | M          | P1-02,P1-09 |
| P1-15 | Unit tests: config precedence + audit writes | §6.1, §17.3          | M          | P1-02,P1-09 |

## Task detail

### P1-01 — Define `internal/config` typed schema (M)

Create `internal/config/types.go` with Go structs mirroring SPEC §5.3 front-matter sections
(`project`, `tracker`, `polling`, `workspace`, `hooks`, `providers`, `routing`, `knowledge`,
`memory`, `docs`, `harness_rules`, `enforcement`, `validation`, `agent`, `server`). Use
`yaml:"..."` tags. No parsing logic yet — just types and zero-value defaults documented.

### P1-02 — Wire Viper precedence chain (M)

Implement `config.Load(flags *pflag.FlagSet, harnessPath string) (Config, error)` honoring the
SPEC §6.1 precedence: CLI flags → env vars → YAML front matter → built-in defaults. Front-matter
parsing is not yet wired (P1-13 stub returns empty); for now, test with env vars and flags.

### P1-03 — Implement `$VAR` expansion helper (S)

`internal/config/expand.go`: a function that walks string fields and expands `$VAR` and
`${VAR}` references against a `LookupFunc` (defaulting to `os.LookupEnv`). Must distinguish
"unset" from "set but empty" so SPEC §6.4 can fail on missing tracker keys. Unit tested.

### P1-04 — Path normalization helper (S)

`internal/config/paths.go`: `~` expansion + `$VAR` expansion + OS separator normalization for
fields tagged as path. Unit tests cover Windows `\\` and POSIX `/` cases.

### P1-05 — Error sentinels (S)

Create `errors.go` in each package that owns errors from SPEC §23. The string value of each
sentinel matches the spec's identifier exactly (`"missing_harness_file"`, `"harness_parse_error"`,
…). Add a `errors.go` test that asserts every spec identifier maps to exactly one sentinel.

### P1-06 — `internal/db` core (M)

Define `DB` struct wrapping a `*sql.DB`, with constructor for SQLite (modernc) and a stub for
Postgres (returns "not implemented" — Phase 18 hardens it). Connection pool sizing per config;
context-aware `Exec`/`Query`/`QueryRow`. No domain queries here.

### P1-07 — Migration runner + initial schema (M)

`internal/db/migrate.go`: forward-only migration runner reading SQL files from
`internal/db/migrations/*.sql` (embedded via `embed.FS`). First migration `0001_init.sql`
creates `audit_events` per SPEC §17.3 and a `schema_migrations` tracking table.

### P1-08 — `AuditEvent` type + event registry (S)

`internal/audit/event.go`: the `AuditEvent` struct (fields per SPEC §4.1.10) and an
`EventType` enum-string typed list mirroring SPEC §17.2. Exported constants for every event
type. JSON marshalling round-trip tested.

### P1-09 — Audit writer (M)

`internal/audit/writer.go`: `Writer` type with `Write(ctx, evt)` method. Persists to
`audit_events` table (P1-07 schema) and computes UUIDs. Future-proofed for the workspace JSONL
sink (P1-10) and webhook sink (Phase 14) — both are exposed as additional `Sink` interfaces.

### P1-10 — Workspace JSONL sink (S)

`internal/audit/jsonl_sink.go`: a `Sink` implementation that appends NDJSON to a configured
file path. The orchestrator will inject one of these per workspace in Phase 5.

### P1-11 — zerolog setup (S)

`internal/log/log.go` (or kept inside `cmd/conductor/log.go` until needed elsewhere): build a
root `zerolog.Logger` from `--log-level` and `--log-format` flags. Add a test ensuring an
unknown level returns an error rather than silently defaulting.

### P1-12 — Cobra root + `version` (S)

`cmd/conductor/main.go` + `cmd/conductor/cmd/root.go` + `cmd/conductor/cmd/version.go`. Version
is read from build-info `runtime/debug.ReadBuildInfo()` (no LDFLAGS magic needed yet).

### P1-13 — `harness validate` stub (S)

`cmd/conductor/cmd/harness_validate.go`: subcommand that reads `--harness <path>`, opens the
file, and prints `"OK (parser stub — full validation lands in Phase 2)"`. Exits non-zero only
if the file is missing. The full implementation is Phase 2.

### P1-14 — `conductor start --dry-run` (M)

`cmd/conductor/cmd/start.go`: the `start` command, with `--dry-run` exiting after config load.
On full start, currently prints "orchestrator not yet implemented (Phase 6)" and exits with
status 0. Wires the audit writer end-to-end: writes one `RunAttemptStarted`-style placeholder
event to confirm the pipeline works, then exits.

### P1-15 — Phase-1 unit tests (M)

Two test files:

- `internal/config/load_test.go`: covers precedence, `$VAR` expansion, path normalization,
  missing-required-field detection.
- `internal/audit/writer_test.go`: covers DB writes, JSONL sink, parent-event-id chaining.

Coverage target for Phase 1: ≥ 70% across `internal/config`, `internal/db`, `internal/audit`.

## Out of scope for Phase 1

- HARNESS.md parsing (Phase 2)
- Provider adapters (Phase 3)
- Tracker adapters (Phase 4)
- Workspaces, orchestrator, router, validation, memory, knowledge, docstore, harness enforcer,
  HTTP API, dashboard, container isolation, plugins (Phases 5–18 per [docs/phases.md](docs/phases.md))
