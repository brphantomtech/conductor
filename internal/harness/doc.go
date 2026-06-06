// Package harness loads, validates, hot-reloads, and enforces HARNESS.md
// (SPEC §5, §6, §11). It owns the front-matter parser, the per-role Liquid
// prompt-template registry, the HarnessRule runner, the pre-dispatch drift
// check, and the scheduled GC enforcement worker.
//
// Phase 2 ships the loader half of that contract:
//
//   - discovery.go — SPEC §5.1 path resolution (flag → env → cwd → doc store).
//   - parser.go    — SPEC §5.2 front-matter + `## <role>` body parser.
//   - renderer.go  — SPEC §16.2 strict Liquid renderer (unknown variable /
//     unknown filter → template_render_error).
//   - validator.go — SPEC §5.3 + §6.4 schema validation as a single
//     errors.Join, plus role/template coverage and compile-time syntax
//     checks for every prompt template.
//   - loader.go    — orchestrates Resolve → Parse → config.Load → Validate
//     and surfaces a Result with the path + Source for audit context.
//   - watcher.go   — fsnotify-backed reload pump with a 250 ms debounce,
//     atomic last-known-good swap, and ConfigReloaded /
//     ConfigReloadFailed audit emission.
//   - errors.go    — SPEC §23.1 sentinels plus the validator sub-class
//     wrappers the CLI uses to categorize multi-error output.
//
// Phase 12 adds the enforcer half (SPEC §11):
//
//   - violation.go  — the Violation record + Severity classification.
//   - runner.go     — the HarnessRule runner: each `check` runs in the
//     workspace root under a timeout; non-zero exit → a severity-tagged
//     violation; emits HarnessViolationDetected.
//   - enforcer.go   — the Enforcer: PreDispatch implements the orchestrator's
//     EnforcerCheck seam (SPEC §11.2) with the three severity behaviors incl.
//     blocking-halt + cached debt/architectural prompt sections.
//   - gc.go         — scheduled GC (SPEC §11.3): a robfig/cron job creates
//     deduplicated tracker issues for auto_fix violations (GCTaskCreated).
//   - layers.go     — Knowledge-Engine layer-violation translation into
//     dependency-category violations (SPEC §8.6, §11.4).
//   - formatters.go — the pure FormatTechnicalDebt / FormatArchitecturalIssues
//     prompt-section formatters (SPEC §16.1 steps 7–8).
//
// Tier 2 (domain engine). Imports Tier 0 + Tier 1. Other Tier 2 packages
// (knowledge, memory, validation, docstore) are wired in by Tier 3 (router)
// or Tier 4 (orchestrator), not consumed directly here.
package harness
