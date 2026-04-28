// Package validation is the Validation Pipeline (SPEC §15). After each
// agent turn it runs configurable shell checks in the workspace, captures
// stdout/stderr/exit, classifies severity, persists per-turn results, and
// formats output for injection into the next turn's prompt. It is the
// "shift feedback left" mechanism described in Harness Engineering.
//
// Tier 2 (domain engine). Imports Tier 0 + Tier 1.
package validation
