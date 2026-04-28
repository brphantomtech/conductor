// Package harness loads, validates, hot-reloads, and enforces HARNESS.md
// (SPEC §5, §6, §11). It owns the front-matter parser, the per-role Liquid
// prompt-template registry, the HarnessRule runner, the pre-dispatch drift
// check, and the scheduled GC enforcement worker.
//
// Tier 2 (domain engine). Imports Tier 0 + Tier 1. Other Tier 2 packages
// (knowledge, memory, validation, docstore) are wired in by Tier 3 (router)
// or Tier 4 (orchestrator), not consumed directly here.
package harness
