package tools

// ApprovalPolicy is the per-role tool-call gating posture (SPEC §21.2). The
// string values match config.ProviderConfig.ApprovalPolicy verbatim.
type ApprovalPolicy string

// Approval policies per SPEC §21.2.
const (
	// PolicyAuto auto-approves every tool call (trusted environments only).
	PolicyAuto ApprovalPolicy = "auto"
	// PolicyReviewDestructive gates destructive tools (tracker mutations).
	PolicyReviewDestructive ApprovalPolicy = "review_destructive"
	// PolicyManual gates every tool call.
	PolicyManual ApprovalPolicy = "manual"
)

// Decision is the outcome of the approval gate for one tool call.
type Decision int

// Decision values. Allow executes the call; Deny returns a structured
// approval-required result without executing. (A future Await path — Phase
// 14/15's operator channel — slots in here without changing tool code.)
const (
	// DecisionAllow permits the call to execute.
	DecisionAllow Decision = iota
	// DecisionDeny blocks the call and returns an approval-required result.
	DecisionDeny
)

// NormalizePolicy maps a raw config string to a known ApprovalPolicy, defaulting
// to PolicyAuto for empty or unrecognized values so a misconfigured role fails
// open to the documented default rather than blocking silently.
func NormalizePolicy(raw string) ApprovalPolicy {
	switch ApprovalPolicy(raw) {
	case PolicyReviewDestructive:
		return PolicyReviewDestructive
	case PolicyManual:
		return PolicyManual
	default:
		return PolicyAuto
	}
}

// approve is the pure approval-gate decision for a tool call under a policy
// (SPEC §21.2): auto allows all; review_destructive denies destructive tools;
// manual denies all. It has no side effects — the dispatcher turns a Deny into a
// structured approval-required ToolResult.
func approve(toolName string, policy ApprovalPolicy) Decision {
	switch NormalizePolicy(string(policy)) {
	case PolicyManual:
		return DecisionDeny
	case PolicyReviewDestructive:
		if IsDestructive(toolName) {
			return DecisionDeny
		}
		return DecisionAllow
	default: // PolicyAuto
		return DecisionAllow
	}
}

// approvalRequiredResult is the structured ToolResult returned for a gated call.
// It is honest and visible to both the model and the audit log: no operator
// approval channel exists yet (Phases 14/15), so a gated call is denied rather
// than silently auto-approved or skipped.
func approvalRequiredResult(toolName string, policy ApprovalPolicy) ToolResult {
	return ToolResult{
		Content: map[string]any{
			"error":           ErrApprovalRequired.Error(),
			"approval_required": true,
			"tool":            toolName,
			"policy":          string(policy),
			"message": "This tool call requires operator approval under the active " +
				"approval policy and no approval channel is available; the call was not executed.",
		},
		IsError: true,
	}
}
