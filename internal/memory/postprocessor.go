package memory

import (
	"context"
	"fmt"

	"github.com/conductor-sh/conductor/internal/orchestrator"
)

// PostProcessor implements the orchestrator's MemoryPostProcessor seam
// (reconciliation Part C, SPEC §13.5). On every terminal run attempt it
// writes a session-end episodic memory summarizing the attempt outcome.
type PostProcessor struct {
	mgr *Manager
}

// NewPostProcessor wraps a Manager as the reconciliation Part C seam. When
// the manager is disabled the post-processor is a no-op, so it is safe to
// wire unconditionally.
func NewPostProcessor(m *Manager) *PostProcessor {
	return &PostProcessor{mgr: m}
}

// PostProcess satisfies orchestrator.MemoryPostProcessor. It writes one
// issue-scoped episodic memory per terminal attempt. A nil attempt or a
// disabled manager is a no-op.
func (p *PostProcessor) PostProcess(ctx context.Context, attempt *orchestrator.RunAttempt) error {
	if p == nil || p.mgr == nil || !p.mgr.Enabled() || attempt == nil {
		return nil
	}

	content := fmt.Sprintf(
		"Session end for %s (attempt %d): outcome=%s",
		attempt.Identifier, attempt.Attempt, attempt.Outcome,
	)
	if attempt.FailureReason != "" {
		content += "; reason=" + attempt.FailureReason
	}

	_, err := p.mgr.Write(ctx, WriteInput{
		Layer:   LayerEpisodic,
		IssueID: attempt.IssueID,
		Content: content,
		Tags:    []string{"session_end", string(attempt.Outcome)},
		Source:  SourceAutoExtracted,
	})
	return err
}

// compile-time assertion that PostProcessor satisfies the orchestrator seam.
var _ orchestrator.MemoryPostProcessor = (*PostProcessor)(nil)
