// Package memory is the three-layer Memory Manager (SPEC §9). Episodic
// memory captures session-scoped facts; semantic memory captures
// project-wide patterns; procedural memory captures task-type recipes.
// The package owns retrieval, formatting for prompt injection, TTL
// enforcement, and the consolidation worker that synthesizes episodic
// clusters into semantic memories on a schedule.
//
// Tier 2 (domain engine). Imports Tier 0 + Tier 1.
package memory
