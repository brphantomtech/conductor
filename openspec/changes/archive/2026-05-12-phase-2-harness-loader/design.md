## Context

Phase 1 left `internal/harness` with only error sentinels and a doc.go. The CLI's `harness validate` is a stub, and `config.Load(LoadOptions{FrontMatter: nil})` cannot read any HARNESS.md value yet. Phase 2 turns `internal/harness` into a real Tier-2 domain engine: it parses the file, validates the merged config, compiles per-role Liquid templates, and reloads on disk changes — all of which downstream tiers (router in Phase 7, prompt construction in Phase 7+, orchestrator in Phase 6) require.

The constraints are unusual for a parser: SPEC §5.2 mandates "front matter optional + body split by `## <role>`", and §16.2 mandates *strict* Liquid (unknown vars + unknown filters → `template_render_error`). SPEC §6.3 then demands that bad hot-reloads do NOT take down the running config — they fall back to last known good. The watcher therefore has to be transactional in the sense that the "live" Definition pointer is only swapped after parse + validate both succeed.

Stakeholders: the Phase 6 orchestrator (will read live Definition every dispatch tick), Phase 7 router (reads `prompt_templates`), Phase 12 enforcer (reads `harness_rules`). Phase 11 doc-store integration is anticipated but explicitly deferred.

## Goals / Non-Goals

**Goals:**
- Implement SPEC §5.1 discovery (CLI flag → env var → cwd → doc-store stub).
- Implement SPEC §5.2 parser: split front matter from body, split body by `## <role>`, fall back to all-body-as-`coder` when no headings exist.
- Implement SPEC §5.3 + §6.4 schema validation as collected errors (one pass surfaces every problem, never the first only).
- Wrap `osteele/liquid` with strict-mode rendering covering SPEC §16.2 variables and the canonical filter set (`downcase`, `upcase`, `default`, `date`, `truncate`, plus the standard set).
- Implement fsnotify-based reload (250 ms debounce); on success → emit `ConfigReloaded` + atomic swap; on failure → keep last known good + emit `ConfigReloadFailed`.
- Replace the Phase 1 `conductor harness validate` stub with real validation that prints structured errors and exits non-zero on any failure.
- Hand the parsed front matter into `config.Load(opts.FrontMatter)` so the precedence chain finally honors YAML values.

**Non-Goals:**
- Doc-store resolution of HARNESS.md (Phase 11). The discovery resolver exposes a hook but returns `ErrMissingHarnessFile` when only the doc-store path remains.
- Running `HarnessRule.check` scripts (Phase 12). Phase 2 parses and validates `harness_rules` shape only.
- The Harness Enforcer pre-dispatch hook (Phase 12).
- Rendering prompts in production turns (Phase 7). Phase 2 ships the renderer + tests; the router will wire it.
- `## continuation` handling at run-time (Phase 7). Phase 2 stores the section verbatim if present.

## Decisions

### D1. Library choice: `osteele/liquid` for templates

**Choice.** Use `github.com/osteele/liquid` as the Liquid implementation. Compile per-role templates once at load time; render with a per-turn variable map.

**Alternatives considered.** (a) Go `text/template` — wrong syntax for SPEC §16.2 (`{{ issue.identifier | downcase }}` is Liquid, not Go templates). (b) `flosch/pongo2` — Django syntax, also wrong dialect. (c) Hand-roll a Liquid subset — high risk for an external contract spec.

**Why osteele/liquid.** Pure Go (cgo-free), MIT licensed, actively maintained, ships the filter set we need including `downcase`, `upcase`, `default`, `date`, `truncate`, `strip`, `replace`. Strict-mode rendering is achievable by registering a custom undefined-handler that returns `ErrTemplateRender`.

**Strict mode trick.** `liquid.Engine` does not have a "strict" toggle by default — unknown vars render as empty strings. We will register a render hook that walks the AST before rendering, collecting all referenced variable paths, and pre-validate them against the union of SPEC §16.2 + any standard filter call. Unknown filters surface via `engine.RegisterFilter` introspection: any token of `{{ x | unknown }}` will produce a parse error from the engine itself in current versions.

### D2. Strict template validation lives in the renderer, not the parser

**Choice.** The parser only extracts raw template *strings* per role. Compilation happens in `renderer.Compile(name, source)`. Validation happens in two stages: (i) at compile time (`ErrTemplateParse`); (ii) at render time when an unknown variable is dereferenced (`ErrTemplateRender`).

**Why.** This lets the validator (which runs at startup against every role in the pipeline) compile-test every template without actually rendering. It also makes `conductor harness validate` cheap — it compiles and reports parse errors only; render-time errors surface only when real issue variables flow through.

### D3. Watcher debounce + atomic swap via `atomic.Pointer[Definition]`

**Choice.** `Watcher` holds a `*atomic.Pointer[Definition]` for the live definition. On a write event, the watcher coalesces events for 250 ms, then runs parse → validate → compile-all-templates → if all succeed, `ptr.Store(newDef)`. On any failure, ptr is unchanged and a `ConfigReloadFailed` audit event is written.

**Alternatives considered.** Mutex-guarded pointer (more overhead, no benefit), full restart on every change (violates SPEC §6.3 "Does not restart"), eventual-consistency (violates "future dispatch sees new config immediately after success").

**Trade-off.** 250 ms is somewhat arbitrary. Symphony uses ~200 ms; SPEC §5.3.8 mentions a 500 ms debounce for knowledge re-indexing. 250 ms keeps editor "save" coalescing while not making the operator wait too long.

### D4. `config.Load` integration: parse first, then load

**Choice.** The new flow in `cmd/conductor/cmd/start.go`:

```
resolved := harness.Resolve(flags.harness)   // SPEC §5.1
def, err := harness.Parse(resolved)          // SPEC §5.2
cfg, err := config.Load(LoadOptions{Flags: ..., FrontMatter: def.FrontMatter})
err = harness.Validate(def, cfg)             // SPEC §5.3 + §6.4 + role/pipeline coverage
```

**Why.** Front matter has to be a `map[string]any` to feed Viper's `MergeConfigMap` (already wired in Phase 1). Validation needs *both* the parsed templates (to check pipeline role coverage) and the merged Config (to check tracker keys, providers, etc.), so it runs after `config.Load`.

### D5. Discovery precedence as ordered candidate list

**Choice.** `Resolve(flags, env, cwd) []candidate` returns an ordered list of `(source, path)` tuples. The first existing file wins. The doc-store source produces a `candidate{kind: "doc_store", path: ""}` entry that today returns `ErrMissingHarnessFile`; Phase 11 swaps in the real resolver without touching this code.

**Why.** Keeps Phase 11 a one-line change. Also makes the resolver trivially testable: pass a fake env and a fake `fs.StatFS` and check which path wins.

### D6. `conductor harness validate` collects all errors via `errors.Join`

**Choice.** Validation returns `errors.Join(errs...)`. The CLI command unwraps the joined error and prints one line per underlying error, sorted by category (file → parse → schema → templates).

**Why.** Operators editing HARNESS.md want to see every problem at once, not fix one at a time. The structured-errors requirement in SPEC §5/§23 already maps every failure to a sentinel — the CLI just walks `errors.As` against each known sentinel.

### D7. Audit events emitted by the watcher

**Choice.** The watcher writes `ConfigReloaded` on success with payload `{path, applied_at, changes_summary}`. On failure it writes `ConfigReloadFailed` with `{path, error_code, error_message}`. Both go through the existing `audit.Writer`; no new sinks.

**Why.** Both event types are already declared in `internal/audit/event.go` (Phase 1 wired the registry). Phase 2 just produces them. This keeps the audit pipeline a single seam.

## Risks / Trade-offs

- **Risk:** strict-mode Liquid via `osteele/liquid` may not catch every unknown variable at compile time without help. → **Mitigation:** add a pre-render variable-resolver that walks the AST and rejects unknown paths against an allowlist built from SPEC §16.2 plus any locally declared vars from `assign`/`capture` tags. If a future SPEC release expands the variable set, the allowlist updates in one place.
- **Risk:** fsnotify on Windows reports rename-then-create rather than write for many editors (VS Code, JetBrains "atomic save"). → **Mitigation:** watch the *directory* containing HARNESS.md and filter events to `filepath.Base(path)`. Coalesce within 250 ms so a rename + create looks like one event.
- **Risk:** 250 ms debounce hides intentional rapid edits. → **Mitigation:** none needed — operators edit + save, not edit + save + save. If real edge cases appear, the constant moves to config under `harness.reload_debounce_ms`.
- **Risk:** Validation `errors.Join` returns a single error; callers losing structure would degrade UX. → **Mitigation:** the CLI uses `errors.As` against the sentinel set to classify each underlying error before printing.
- **Risk:** Doc-store stub conflicts with real Phase 11 implementation. → **Mitigation:** the candidate-list pattern (D5) and a clear interface boundary keep Phase 11 to a single swap.
- **Risk:** Renderer pulls a new dependency (`osteele/liquid` ~1.5 MB). → **Mitigation:** acceptable for a single-binary deploy; SPEC §3.1 already implicitly requires a Liquid engine.

## Open Questions

1. Should `harness.reload_debounce_ms` be configurable in HARNESS.md? Default 250 ms; deferred unless we hit a real case. *Tentative answer:* no, keep it constant in Phase 2.
2. Should the watcher's lifecycle be tied to `conductor start`'s `ctx`, or stand alone with `Watcher.Close()`? *Tentative answer:* tied to ctx — keeps cleanup uniform with the audit writer's `defer Close()`.
3. Where should the prompt-rendered "## continuation" template default live? In `harness.Definition` (already populated by parser) or in the router (Phase 7)? *Tentative answer:* parser stores it under `definition.PromptTemplates["continuation"]`. Router decides whether to use it or fall back to the SPEC §16.3 built-in default.
