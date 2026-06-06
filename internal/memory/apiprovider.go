package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/conductor-sh/conductor/internal/config"
)

// synthesisPrompt is the instruction sent to the consolidation provider when
// collapsing an episodic cluster into a single semantic/procedural memory
// (SPEC §9.5).
const synthesisPrompt = "You are consolidating related project memories into a single, " +
	"durable lesson. Synthesize the following memories into one concise statement (under 100 " +
	"words) capturing the shared, reusable knowledge."

// APIProvider is a provider-backed Embedder and Synthesizer for the
// OpenAI-compatible embeddings and chat endpoints. It is used by the CLI and
// the orchestrator wiring so consolidation can call a real model. Tests use
// deterministic fakes instead; APIProvider makes no calls until invoked.
type APIProvider struct {
	cfg          config.ProviderConfig
	http         *http.Client
	embeddingURL string
	chatURL      string
}

// NewAPIProvider builds an APIProvider from a provider config. An empty
// BaseURL defaults to the OpenAI endpoint.
func NewAPIProvider(cfg config.ProviderConfig, httpClient *http.Client) *APIProvider {
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &APIProvider{
		cfg:          cfg,
		http:         httpClient,
		embeddingURL: base + "/embeddings",
		chatURL:      base + "/chat/completions",
	}
}

// Embed satisfies Embedder via the OpenAI-compatible embeddings endpoint.
func (p *APIProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	model := p.cfg.Model
	if model == "" {
		model = "text-embedding-3-small"
	}
	body, _ := json.Marshal(map[string]any{"model": model, "input": text})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.embeddingURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("memory: embed request: %w", err)
	}
	p.auth(req)
	resp, err := p.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("memory: embed do: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("memory: embed http %d: %s", resp.StatusCode, string(b))
	}
	var out struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("memory: embed decode: %w", err)
	}
	if len(out.Data) == 0 {
		return nil, fmt.Errorf("memory: embed: empty response")
	}
	return out.Data[0].Embedding, nil
}

// Synthesize satisfies Synthesizer via the OpenAI-compatible chat endpoint.
func (p *APIProvider) Synthesize(ctx context.Context, inputs []string) (string, error) {
	user := strings.Join(inputs, "\n- ")
	body, _ := json.Marshal(map[string]any{
		"model":  p.cfg.Model,
		"stream": false,
		"messages": []map[string]string{
			{"role": "system", "content": synthesisPrompt},
			{"role": "user", "content": "- " + user},
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.chatURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("memory: synthesize request: %w", err)
	}
	p.auth(req)
	resp, err := p.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("memory: synthesize do: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("memory: synthesize http %d: %s", resp.StatusCode, string(b))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("memory: synthesize decode: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("memory: synthesize: empty response")
	}
	return strings.TrimSpace(out.Choices[0].Message.Content), nil
}

func (p *APIProvider) auth(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	if p.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	}
}

// compile-time assertions.
var (
	_ Embedder    = (*APIProvider)(nil)
	_ Synthesizer = (*APIProvider)(nil)
)
