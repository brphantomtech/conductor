package tools

import (
	"context"
	"errors"
)

// errEngine is a sentinel an engine fake returns to exercise error mapping.
var errEngine = errors.New("engine boom")

type fakeTracker struct {
	queryArgs    string
	queryVars    map[string]any
	mutationArgs string
	mutationVars map[string]any
	queryOut     map[string]any
	mutateOut    map[string]any
	err          error
}

func (f *fakeTracker) ExecuteQuery(_ context.Context, q string, v map[string]any) (map[string]any, error) {
	f.queryArgs, f.queryVars = q, v
	if f.err != nil {
		return nil, f.err
	}
	if f.queryOut == nil {
		return map[string]any{"ok": true}, nil
	}
	return f.queryOut, nil
}

func (f *fakeTracker) ExecuteMutation(_ context.Context, m string, v map[string]any) (map[string]any, error) {
	f.mutationArgs, f.mutationVars = m, v
	if f.err != nil {
		return nil, f.err
	}
	if f.mutateOut == nil {
		return map[string]any{"updated": true}, nil
	}
	return f.mutateOut, nil
}

type fakeKnowledge struct {
	gotQuery KnowledgeQuery
	results  []KnowledgeResult
	err      error
}

func (f *fakeKnowledge) SearchKnowledge(_ context.Context, q KnowledgeQuery) ([]KnowledgeResult, error) {
	f.gotQuery = q
	return f.results, f.err
}

type fakeDocs struct {
	gotQuery string
	gotTopK  int
	results  []DocResult
	err      error
}

func (f *fakeDocs) SearchDocs(_ context.Context, q string, topK int) ([]DocResult, error) {
	f.gotQuery, f.gotTopK = q, topK
	return f.results, f.err
}

type fakeMemory struct {
	gotRead  MemoryReadQuery
	gotWrite MemoryWriteEntry
	readOut  []MemoryResult
	writeID  string
	err      error
}

func (f *fakeMemory) ReadMemory(_ context.Context, q MemoryReadQuery) ([]MemoryResult, error) {
	f.gotRead = q
	return f.readOut, f.err
}

func (f *fakeMemory) WriteMemory(_ context.Context, e MemoryWriteEntry) (string, error) {
	f.gotWrite = e
	if f.err != nil {
		return "", f.err
	}
	if f.writeID == "" {
		return "mem-1", nil
	}
	return f.writeID, nil
}

type fakeHarness struct {
	gotRuleID  string
	violations []HarnessViolation
	err        error
}

func (f *fakeHarness) CheckHarness(_ context.Context, ruleID string) ([]HarnessViolation, error) {
	f.gotRuleID = ruleID
	return f.violations, f.err
}

type fakeValidation struct {
	gotWorkspace string
	outcome      ValidationOutcome
	err          error
}

func (f *fakeValidation) RunValidation(_ context.Context, ws string) (ValidationOutcome, error) {
	f.gotWorkspace = ws
	return f.outcome, f.err
}
