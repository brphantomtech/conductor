package cmd

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/conductor-sh/conductor/internal/config"
	"github.com/conductor-sh/conductor/internal/harness"
	"github.com/conductor-sh/conductor/internal/knowledge"
	"github.com/conductor-sh/conductor/internal/memory"
	"github.com/conductor-sh/conductor/internal/tools"
	"github.com/conductor-sh/conductor/internal/tracker"
	"github.com/conductor-sh/conductor/internal/validation"
)

// toolEngines collects the engine handles the built-in tools dispatch to. A nil
// field leaves that tool registered but returning an unavailable result, so the
// advertised tool surface is stable regardless of which engines are enabled.
type toolEngines struct {
	tracker    tracker.Adapter
	knowledge  *knowledge.Engine
	memory     *memory.Manager
	validation config.Validation
}

// buildToolEngines assembles the tools.Engines from the wired engine handles,
// adapting each engine onto its narrow consumer-side interface. The tracker
// adapter satisfies tools.TrackerExecutor directly; the others are adapted here
// so internal/tools stays decoupled from the concrete engines.
func buildToolEngines(cfg config.Config, e toolEngines) tools.Engines {
	out := tools.Engines{}
	if e.tracker != nil {
		out.Tracker = e.tracker // tracker.Adapter satisfies tools.TrackerExecutor.
	}
	if e.knowledge != nil {
		out.Knowledge = knowledgeSearchAdapter{eng: e.knowledge}
		out.Docs = docSearchAdapter{eng: e.knowledge}
	}
	if e.memory != nil {
		out.Memory = memoryStoreAdapter{mgr: e.memory}
	}
	// Harness check runs the configured rules through a workspace-root runner.
	out.Harness = harnessCheckAdapter{configFn: func() config.Config { return cfg }, root: workspaceRootOrDot(cfg)}
	// Validation runs the configured pipeline on demand.
	out.Validation = validationRunAdapter{cfg: e.validation}
	return out
}

func workspaceRootOrDot(cfg config.Config) string {
	if cfg.Workspace.Root != "" {
		return cfg.Workspace.Root
	}
	return "."
}

// knowledgeSearchAdapter adapts knowledge.Engine.Search onto
// tools.KnowledgeSearcher.
type knowledgeSearchAdapter struct{ eng *knowledge.Engine }

func (a knowledgeSearchAdapter) SearchKnowledge(
	ctx context.Context, q tools.KnowledgeQuery,
) ([]tools.KnowledgeResult, error) {
	types := make([]knowledge.NodeType, 0, len(q.Types))
	for _, t := range q.Types {
		types = append(types, knowledge.NodeType(t))
	}
	nodes, err := a.eng.Search(ctx, knowledge.SearchParams{
		Query:       q.Query,
		Types:       types,
		PathPattern: q.PathPattern,
		TopK:        q.TopK,
	})
	if err != nil {
		return nil, fmt.Errorf("knowledge search: %w", err)
	}
	out := make([]tools.KnowledgeResult, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, tools.KnowledgeResult{
			Path:    n.Node.Path,
			Name:    n.Node.Name,
			Type:    string(n.Node.Type),
			Summary: n.Node.Summary,
			Score:   n.Score,
		})
	}
	return out, nil
}

// docSearchAdapter adapts knowledge.Engine search restricted to doc nodes onto
// tools.DocSearcher (Doc Store documents are indexed as `doc` knowledge nodes).
type docSearchAdapter struct{ eng *knowledge.Engine }

func (a docSearchAdapter) SearchDocs(ctx context.Context, query string, topK int) ([]tools.DocResult, error) {
	nodes, err := a.eng.Search(ctx, knowledge.SearchParams{
		Query: query,
		Types: []knowledge.NodeType{knowledge.NodeDoc},
		TopK:  topK,
	})
	if err != nil {
		return nil, fmt.Errorf("doc search: %w", err)
	}
	out := make([]tools.DocResult, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, tools.DocResult{
			Path:    n.Node.Path,
			Title:   n.Node.Name,
			Summary: n.Node.Summary,
			Score:   n.Score,
		})
	}
	return out, nil
}

// memoryStoreAdapter adapts memory.Manager onto tools.MemoryStore.
type memoryStoreAdapter struct{ mgr *memory.Manager }

func (a memoryStoreAdapter) ReadMemory(ctx context.Context, q tools.MemoryReadQuery) ([]tools.MemoryResult, error) {
	entries, err := a.mgr.Retrieve(ctx, memory.RetrieveRequest{
		ProjectID: q.ProjectID,
		IssueID:   q.IssueID,
		TaskType:  q.TaskType,
		Intent:    q.Intent,
	})
	if err != nil {
		return nil, fmt.Errorf("memory read: %w", err)
	}
	out := make([]tools.MemoryResult, 0, len(entries))
	for _, e := range entries {
		out = append(out, tools.MemoryResult{
			ID:      e.ID,
			Layer:   string(e.Layer),
			Content: e.Content,
			Score:   e.RelevanceScore,
		})
	}
	return out, nil
}

func (a memoryStoreAdapter) WriteMemory(ctx context.Context, e tools.MemoryWriteEntry) (string, error) {
	entry, err := a.mgr.Write(ctx, memory.WriteInput{
		Layer:     memory.Layer(e.Layer),
		ProjectID: e.ProjectID,
		IssueID:   e.IssueID,
		TaskType:  e.TaskType,
		Content:   e.Content,
		Tags:      e.Tags,
		Source:    memory.SourceAgentWritten,
	})
	if err != nil {
		return "", fmt.Errorf("memory write: %w", err)
	}
	if entry == nil {
		return "", nil
	}
	return entry.ID, nil
}

// harnessCheckAdapter adapts a harness Runner over the configured rules onto
// tools.HarnessChecker. An optional rule_id filters to one rule.
type harnessCheckAdapter struct {
	configFn func() config.Config
	root     string
}

func (a harnessCheckAdapter) CheckHarness(ctx context.Context, ruleID string) ([]tools.HarnessViolation, error) {
	cfg := a.configFn()
	rules := cfg.HarnessRules
	if ruleID != "" {
		filtered := make([]config.HarnessRule, 0, 1)
		for _, r := range rules {
			if r.ID == ruleID {
				filtered = append(filtered, r)
			}
		}
		rules = filtered
	}
	runner := harness.NewRunner(harness.NewDirCommandFactory(a.root))
	violations := runner.Run(ctx, rules)
	out := make([]tools.HarnessViolation, 0, len(violations))
	for _, v := range violations {
		out = append(out, tools.HarnessViolation{
			RuleID:   v.RuleID,
			Name:     v.Name,
			Severity: string(v.Severity),
			Summary:  v.Summary,
			FixHint:  v.FixHint,
		})
	}
	return out, nil
}

// validationRunAdapter adapts the Validation Pipeline onto tools.ValidationRunner,
// pinning each run to the call's workspace path.
type validationRunAdapter struct{ cfg config.Validation }

func (a validationRunAdapter) RunValidation(
	ctx context.Context, workspacePath string,
) (tools.ValidationOutcome, error) {
	if workspacePath == "" {
		workspacePath = "."
	}
	pipeline := validation.New(a.cfg,
		validation.WithCommandFactory(validation.NewDirCommandFactory(workspacePath)),
	)
	dir := filepath.Join(workspacePath, ".conductor", "validation")
	res, err := pipeline.Run(ctx, dir, 0)
	if err != nil {
		return tools.ValidationOutcome{}, fmt.Errorf("validation run: %w", err)
	}
	checks := make([]tools.ValidationCheckResult, 0, len(res.Checks))
	failed := false
	for _, c := range res.Checks {
		checks = append(checks, tools.ValidationCheckResult{
			CheckID:  c.CheckID,
			Name:     c.Name,
			Status:   string(c.Status),
			Severity: c.Severity,
		})
		if c.Status != validation.StatusPassed {
			failed = true
		}
	}
	return tools.ValidationOutcome{Passed: !failed, Checks: checks}, nil
}
