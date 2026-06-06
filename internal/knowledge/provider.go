package knowledge

import (
	"context"
	"fmt"

	"github.com/conductor-sh/conductor/internal/config"
	"github.com/conductor-sh/conductor/internal/provider"
)

// Summarizer produces a 1–3 sentence natural-language summary of a parsed unit
// (SPEC §8.2 Stage 3). Defined at the consumer; tests inject a deterministic
// fake, production wraps a provider.Adapter.
type Summarizer interface {
	Summarize(ctx context.Context, path, content string) (string, error)
}

// Embedder produces a dense vector for a text (SPEC §8.2 Stage 4). Defined at
// the consumer; tests inject a deterministic fake. A provider failure here is
// classified as ErrEmbeddingRequestFailed by the pipeline.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// summarizationPrompt is the internal, non-user-configurable Stage 3 prompt
// template from SPEC §8.2. It is rendered with the unit path and content.
const summarizationPrompt = `Summarize the following code in 1-3 sentences. Focus on what it does, not how.
Do not repeat the code. Be specific about inputs, outputs, and side effects.

Path: %s
Content:
%s`

// providerSummarizer summarizes via a provider.Adapter turn using the
// configured embedding_provider role config.
type providerSummarizer struct {
	adapter provider.Adapter
	cfg     config.ProviderConfig
}

// NewProviderSummarizer wraps a provider.Adapter as a Summarizer. The cfg is
// the resolved embedding_provider role config.
func NewProviderSummarizer(adapter provider.Adapter, cfg config.ProviderConfig) Summarizer {
	return &providerSummarizer{adapter: adapter, cfg: cfg}
}

// Summarize opens a one-shot session, runs a single turn with the Stage 3
// prompt, and returns the aggregated text.
func (s *providerSummarizer) Summarize(ctx context.Context, path, content string) (string, error) {
	sess, err := s.adapter.CreateSession(ctx, s.cfg, "")
	if err != nil {
		return "", fmt.Errorf("knowledge: summarize create session: %w", err)
	}
	defer func() { _ = s.adapter.EndSession(ctx, sess) }()

	stream, err := s.adapter.StartTurn(ctx, sess, fmt.Sprintf(summarizationPrompt, path, content), nil)
	if err != nil {
		return "", fmt.Errorf("knowledge: summarize start turn: %w", err)
	}
	res := stream.Wait()
	if res.Err != nil {
		return "", fmt.Errorf("knowledge: summarize turn: %w", res.Err)
	}
	return res.Text, nil
}

var _ Summarizer = (*providerSummarizer)(nil)
