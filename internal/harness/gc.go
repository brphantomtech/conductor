package harness

import (
	"context"
	"fmt"
	"strings"

	"github.com/robfig/cron/v3"

	"github.com/conductor-sh/conductor/internal/audit"
	"github.com/conductor-sh/conductor/internal/config"
)

// gcRuleMarker is the hidden body marker the GC writer embeds in each created
// issue so a later GC run can dedup against an existing open issue for the same
// rule_id (design.md "GC dedup marker": a body marker plus the configured
// label). The wiring layer's TrackerIssuer search matches on it.
const gcRuleMarker = "conductor-gc-rule-id:"

// GCIssue is the issue the enforcer asks the tracker to create for an auto-fix
// violation (SPEC §11.3). The wiring layer maps it onto the concrete tracker
// adapter's create-issue call.
type GCIssue struct {
	// RuleID is the originating HarnessRule.id; dedup keys on it.
	RuleID string
	// Title is "[GC] <name>: <summary>" per SPEC §11.3.
	Title string
	// Description includes the fix_hint, affected files, and the dedup marker.
	Description string
	// Label is enforcement.gc_issue_label.
	Label string
	// State is enforcement.gc_issue_state.
	State string
}

// TrackerIssuer is the consumer-side subset of the tracker the GC worker needs:
// query open GC issues for dedup and create a new one. Declared here so harness
// (Tier 2) does not import a concrete tracker adapter; the wiring layer supplies
// an implementation. A nil TrackerIssuer disables GC issue creation.
type TrackerIssuer interface {
	// OpenGCRuleIDs returns the rule ids that already have an open GC issue
	// (labeled the GC label). The GC worker skips creating duplicates for them.
	OpenGCRuleIDs(ctx context.Context, label string) (map[string]struct{}, error)
	// CreateGCIssue creates one GC issue and returns its identifier.
	CreateGCIssue(ctx context.Context, issue GCIssue) (string, error)
}

// RunGC runs every rule and, for each violation of a rule with auto_fix == true,
// creates a deduplicated tracker issue (SPEC §11.3). It returns the number of
// issues created (the pending_gc_tasks delta). When no tracker issuer is wired
// it is a no-op returning 0.
func (e *Enforcer) RunGC(ctx context.Context) (int, error) {
	cfg := e.configFn()
	if e.tracker == nil {
		return 0, nil
	}

	autoFix := autoFixRuleIDs(cfg.HarnessRules)
	violations := e.collect(ctx, cfg)

	label := cfg.Enforcement.GCIssueLabel
	existing, err := e.tracker.OpenGCRuleIDs(ctx, label)
	if err != nil {
		return 0, fmt.Errorf("harness: query open gc issues: %w", err)
	}

	created := 0
	for _, v := range violations {
		if !autoFix[v.RuleID] {
			continue
		}
		if _, dup := existing[v.RuleID]; dup {
			continue
		}
		issue := buildGCIssue(v, cfg.Enforcement)
		id, cerr := e.tracker.CreateGCIssue(ctx, issue)
		if cerr != nil {
			e.log.Warn().Err(cerr).Str("rule_id", v.RuleID).
				Msg("harness: create gc issue failed")
			continue
		}
		// Record locally so two violations of the same rule in one run do not
		// both create an issue.
		existing[v.RuleID] = struct{}{}
		created++
		e.emitGCCreated(ctx, v, id)
	}
	return created, nil
}

// StartGC schedules RunGC on enforcement.gc_schedule_cron using the supplied
// cron scheduler and runs until ctx is cancelled. An empty cron expression
// leaves GC unscheduled. The caller owns the scheduler lifecycle; StartGC only
// registers the job and starts/stops the scheduler with ctx.
func (e *Enforcer) StartGC(ctx context.Context, c *cron.Cron) error {
	spec := e.configFn().Enforcement.GCScheduleCron
	if strings.TrimSpace(spec) == "" {
		return nil
	}
	_, err := c.AddFunc(spec, func() {
		if _, gerr := e.RunGC(ctx); gerr != nil {
			e.log.Warn().Err(gerr).Msg("harness: scheduled gc run failed")
		}
	})
	if err != nil {
		return fmt.Errorf("harness: schedule gc cron %q: %w", spec, err)
	}
	c.Start()
	go func() {
		<-ctx.Done()
		c.Stop()
	}()
	return nil
}

// buildGCIssue assembles the GC issue per SPEC §11.3: title "[GC] <name>:
// <summary>", description with the fix hint, affected files, and the dedup
// marker, plus the configured label and state.
func buildGCIssue(v Violation, enf config.Enforcement) GCIssue {
	var b strings.Builder
	if v.Summary != "" {
		fmt.Fprintf(&b, "%s\n\n", v.Summary)
	}
	if v.FixHint != "" {
		fmt.Fprintf(&b, "Fix hint: %s\n", v.FixHint)
	}
	if len(v.AffectedFiles) > 0 {
		fmt.Fprintf(&b, "Affected files: %s\n", strings.Join(v.AffectedFiles, ", "))
	}
	fmt.Fprintf(&b, "\n<!-- %s%s -->", gcRuleMarker, v.RuleID)

	return GCIssue{
		RuleID:      v.RuleID,
		Title:       fmt.Sprintf("[GC] %s: %s", bulletLabel(v), v.Summary),
		Description: b.String(),
		Label:       enf.GCIssueLabel,
		State:       enf.GCIssueState,
	}
}

// autoFixRuleIDs returns the set of rule ids whose auto_fix flag is set.
func autoFixRuleIDs(rules []config.HarnessRule) map[string]bool {
	out := make(map[string]bool, len(rules))
	for _, r := range rules {
		if r.AutoFix {
			out[r.ID] = true
		}
	}
	return out
}

// emitGCCreated writes a GCTaskCreated audit event (SPEC §11.3, §17.2).
func (e *Enforcer) emitGCCreated(ctx context.Context, v Violation, issueID string) {
	if e.audit == nil {
		return
	}
	evt := audit.AuditEvent{
		ProjectID: e.projectID,
		IssueID:   issueID,
		EventType: audit.EventGCTaskCreated,
		Payload: map[string]any{
			"rule_id":  v.RuleID,
			"name":     v.Name,
			"category": v.Category,
		},
	}
	if err := e.audit.Write(ctx, evt); err != nil {
		e.log.Warn().Err(err).Str("rule_id", v.RuleID).
			Msg("harness: audit write failed")
	}
}
