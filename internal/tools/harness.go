package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// Tool names for the harness/validation tools (SPEC §7.3).
const (
	ToolHarnessCheck  = "conductor_harness_check"
	ToolValidationRun = "conductor_validation_run"
)

// harnessCheckTool implements conductor_harness_check over a HarnessChecker
// (Harness Enforcer rule run, SPEC §11.2).
type harnessCheckTool struct{ checker HarnessChecker }

// NewHarnessCheckTool constructs the conductor_harness_check tool.
func NewHarnessCheckTool(checker HarnessChecker) Tool { return &harnessCheckTool{checker: checker} }

func (t *harnessCheckTool) Name() string { return ToolHarnessCheck }

func (t *harnessCheckTool) Description() string {
	return "Run the project's HarnessRule checks (optionally one named rule) and return any violations."
}

func (t *harnessCheckTool) ParametersSchema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "rule_id": {"type": "string", "description": "Optional rule id to run; empty runs all rules."}
  }
}`)
}

func (t *harnessCheckTool) Execute(ctx context.Context, params map[string]any, _ ExecutionContext) (ToolResult, error) {
	if t.checker == nil {
		return ToolResult{}, fmt.Errorf("tools: %s: %w", t.Name(), ErrEngineUnavailable)
	}
	ruleID, _ := stringParam(params, "rule_id")
	violations, err := t.checker.CheckHarness(ctx, ruleID)
	if err != nil {
		return ToolResult{}, fmt.Errorf("tools: %s: %w", t.Name(), err)
	}
	return ToolResult{Content: map[string]any{
		"violations": violations,
		"count":      len(violations),
		"clear":      len(violations) == 0,
	}}, nil
}

// validationRunTool implements conductor_validation_run over a ValidationRunner
// (Validation Pipeline on demand, SPEC §15). The workspace path is injected via
// the ExecutionContext, not agent-supplied.
type validationRunTool struct{ runner ValidationRunner }

// NewValidationRunTool constructs the conductor_validation_run tool.
func NewValidationRunTool(runner ValidationRunner) Tool { return &validationRunTool{runner: runner} }

func (t *validationRunTool) Name() string { return ToolValidationRun }

func (t *validationRunTool) Description() string {
	return "Trigger the Validation Pipeline on the current workspace and return the check results."
}

func (t *validationRunTool) ParametersSchema() json.RawMessage {
	return json.RawMessage(`{"type": "object", "properties": {}}`)
}

func (t *validationRunTool) Execute(ctx context.Context, _ map[string]any, ec ExecutionContext) (ToolResult, error) {
	if t.runner == nil {
		return ToolResult{}, fmt.Errorf("tools: %s: %w", t.Name(), ErrEngineUnavailable)
	}
	outcome, err := t.runner.RunValidation(ctx, ec.WorkspacePath)
	if err != nil {
		return ToolResult{}, fmt.Errorf("tools: %s: %w", t.Name(), err)
	}
	return ToolResult{Content: map[string]any{
		"passed": outcome.Passed,
		"checks": outcome.Checks,
	}}, nil
}
