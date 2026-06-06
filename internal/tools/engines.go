package tools

import "context"

// This file declares the narrow consumer-side interfaces each built-in tool
// depends on (docs/conventions.md §3: define interfaces at the consumer). The
// real engines satisfy them at wiring time; tests inject fakes. Keeping them
// here means internal/tools never imports a concrete engine package.

// TrackerExecutor is the tracker subset the tracker_query / tracker_mutate
// tools use. The tracker.Adapter satisfies it directly.
type TrackerExecutor interface {
	// ExecuteQuery runs a read-only request (GraphQL doc + variables for
	// Linear; URL path for GitHub).
	ExecuteQuery(ctx context.Context, query string, variables map[string]any) (map[string]any, error)
	// ExecuteMutation runs a write request with the same conventions.
	ExecuteMutation(ctx context.Context, mutation string, variables map[string]any) (map[string]any, error)
}

// KnowledgeResult is one ranked node returned by KnowledgeSearcher, flattened
// so internal/tools does not import the knowledge package's node types.
type KnowledgeResult struct {
	Path    string  `json:"path"`
	Name    string  `json:"name"`
	Type    string  `json:"type"`
	Summary string  `json:"summary"`
	Score   float64 `json:"score"`
}

// KnowledgeQuery is the knowledge_search parameter set forwarded to the engine.
type KnowledgeQuery struct {
	Query       string
	Types       []string
	PathPattern string
	TopK        int
}

// KnowledgeSearcher is the Knowledge Engine subset the knowledge_search tool
// uses. The wiring layer adapts knowledge.Engine.Search onto it.
type KnowledgeSearcher interface {
	SearchKnowledge(ctx context.Context, q KnowledgeQuery) ([]KnowledgeResult, error)
}

// DocResult is one ranked document hit returned by DocSearcher.
type DocResult struct {
	Path    string  `json:"path"`
	Title   string  `json:"title"`
	Summary string  `json:"summary"`
	Score   float64 `json:"score"`
}

// DocSearcher is the Doc Store subset the doc_search tool uses. The wiring layer
// adapts a doc-node search (knowledge nodes of type "doc") onto it.
type DocSearcher interface {
	SearchDocs(ctx context.Context, query string, topK int) ([]DocResult, error)
}

// MemoryResult is one retrieved memory entry returned by MemoryStore.Read.
type MemoryResult struct {
	ID      string  `json:"id"`
	Layer   string  `json:"layer"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
}

// MemoryReadQuery is the memory_read parameter set.
type MemoryReadQuery struct {
	ProjectID string
	IssueID   string
	TaskType  string
	Intent    string
}

// MemoryWriteEntry is the memory_write parameter set.
type MemoryWriteEntry struct {
	Layer     string
	ProjectID string
	IssueID   string
	TaskType  string
	Content   string
	Tags      []string
}

// MemoryStore is the Memory Manager subset the memory_read / memory_write tools
// use. The wiring layer adapts memory.Manager onto it.
type MemoryStore interface {
	ReadMemory(ctx context.Context, q MemoryReadQuery) ([]MemoryResult, error)
	WriteMemory(ctx context.Context, e MemoryWriteEntry) (string, error)
}

// HarnessViolation is one violation returned by HarnessChecker.
type HarnessViolation struct {
	RuleID   string `json:"rule_id"`
	Name     string `json:"name"`
	Severity string `json:"severity"`
	Summary  string `json:"summary"`
	FixHint  string `json:"fix_hint,omitempty"`
}

// HarnessChecker is the Harness Enforcer subset the harness_check tool uses. It
// runs the configured rules (optionally one named rule) and returns the
// violations. The wiring layer adapts harness.Runner/Enforcer onto it.
type HarnessChecker interface {
	CheckHarness(ctx context.Context, ruleID string) ([]HarnessViolation, error)
}

// ValidationCheckResult is one check outcome returned by ValidationRunner.
type ValidationCheckResult struct {
	CheckID  string `json:"check_id"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	Severity string `json:"severity"`
}

// ValidationOutcome is the validation_run result.
type ValidationOutcome struct {
	Passed bool                    `json:"passed"`
	Checks []ValidationCheckResult `json:"checks"`
}

// ValidationRunner is the Validation Pipeline subset the validation_run tool
// uses. The wiring layer adapts validation.Pipeline onto it.
type ValidationRunner interface {
	RunValidation(ctx context.Context, workspacePath string) (ValidationOutcome, error)
}
