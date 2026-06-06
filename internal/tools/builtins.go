package tools

// Engines bundles the consumer-side engine handles the built-in tools depend
// on. The wiring layer (cmd/conductor) fills the fields it has wired; a nil
// field means that tool is still registered but returns an
// ErrEngineUnavailable result, so the tool surface advertised to the model is
// stable regardless of which engines are enabled.
type Engines struct {
	Tracker    TrackerExecutor
	Knowledge  KnowledgeSearcher
	Docs       DocSearcher
	Memory     MemoryStore
	Harness    HarnessChecker
	Validation ValidationRunner
}

// RegisterBuiltins registers the eight SPEC §7.3 built-in tools on r, backed by
// the supplied engines. It panics on a duplicate name (a wiring error), matching
// the construction-time panic convention.
func (r *Registry) RegisterBuiltins(e Engines) {
	r.MustRegister(NewTrackerQueryTool(e.Tracker))
	r.MustRegister(NewTrackerMutateTool(e.Tracker))
	r.MustRegister(NewKnowledgeSearchTool(e.Knowledge))
	r.MustRegister(NewDocSearchTool(e.Docs))
	r.MustRegister(NewMemoryReadTool(e.Memory))
	r.MustRegister(NewMemoryWriteTool(e.Memory))
	r.MustRegister(NewHarnessCheckTool(e.Harness))
	r.MustRegister(NewValidationRunTool(e.Validation))
}

// destructiveTools is the set of tool names classified destructive for approval
// gating (SPEC §21.2): a destructive tool is denied under review_destructive.
var destructiveTools = map[string]struct{}{
	ToolTrackerMutate: {},
}

// IsDestructive reports whether the named tool performs a destructive action
// (state changes, deletions) and is therefore gated under review_destructive.
func IsDestructive(name string) bool {
	_, ok := destructiveTools[name]
	return ok
}
