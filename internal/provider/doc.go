// Package provider hosts the LLM provider adapters and the abstract
// ProviderAdapter interface (SPEC §7) that every adapter implements.
// Adapters covered: openrouter, anthropic, openai, ollama, lm_studio,
// custom (any OpenAI-compatible endpoint). The package handles streaming,
// tool injection, token accounting, and context-budget compaction.
//
// Tier 1 (external adapters). Imports config, db, audit. Does not import
// any other Tier 1 sibling.
package provider
