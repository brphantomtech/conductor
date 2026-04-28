// Package knowledge is the Knowledge Engine (SPEC §8): a long-running
// background service that indexes the codebase semantically and structurally,
// builds a dependency graph via tree-sitter, and serves hybrid RAG queries
// for context injection into agent turns. It also exposes
// CheckLayerViolations for the Harness Enforcer.
//
// Tier 2 (domain engine). Imports Tier 0 + Tier 1.
package knowledge
