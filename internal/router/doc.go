// Package router is the Agent Router (SPEC §12). For each issue it
// classifies the task type, evaluates routing rules to select a pipeline,
// then drives the pipeline turn-by-turn — invoking each agent role, passing
// outputs forward as "Output from Previous Role" sections, running the
// Validation Pipeline after each turn, and applying continuation prompts.
//
// Tier 3 (pipeline execution). Imports Tier 0 through Tier 2.
package router
