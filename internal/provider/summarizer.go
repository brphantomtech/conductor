package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/conductor-sh/conductor/internal/config"
)

// openaiSummarizer is the production summarizer for OpenAI-compatible
// adapters (openai, openrouter). It issues a single non-streaming
// chat-completions request carrying the transcript plus the SPEC §7.4
// summarization prompt. Tests inject a fake via WithSummarizer instead.
type openaiSummarizer struct {
	http         *http.Client
	baseURL      string
	authHeader   func(*http.Request, string)
	extraHeaders map[string]string
}

func (s openaiSummarizer) summarize(ctx context.Context, cfg config.ProviderConfig, transcript string) (string, error) {
	body, err := json.Marshal(map[string]any{
		"model":  cfg.Model,
		"stream": false,
		"messages": []map[string]string{
			{"role": "system", "content": summarizationPrompt},
			{"role": "user", "content": transcript},
		},
	})
	if err != nil {
		return "", fmt.Errorf("provider: summarize: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", wrapRequest("summarize", "build request", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if s.authHeader != nil {
		s.authHeader(req, cfg.APIKey)
	}
	for k, v := range s.extraHeaders {
		req.Header.Set(k, v)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return "", wrapRequest("summarize", "do request", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", readErrorBody("summarize", "do request", resp)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", wrapRequest("summarize", "read body", err)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", wrapStream("summarize", "decode", err)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("provider: summarize: empty response: %w", ErrStreamError)
	}
	return out.Choices[0].Message.Content, nil
}

// anthropicSummarizer is the production summarizer for the Anthropic adapter.
type anthropicSummarizer struct {
	http    *http.Client
	baseURL string
}

func (s anthropicSummarizer) summarize(
	ctx context.Context, cfg config.ProviderConfig, transcript string,
) (string, error) {
	maxTokens := cfg.MaxTokens
	if maxTokens == 0 {
		maxTokens = 1024
	}
	body, err := json.Marshal(map[string]any{
		"model":      cfg.Model,
		"max_tokens": maxTokens,
		"system":     summarizationPrompt,
		"messages": []map[string]any{
			{"role": "user", "content": transcript},
		},
	})
	if err != nil {
		return "", fmt.Errorf("provider: summarize: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return "", wrapRequest("summarize", "build request", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", cfg.APIKey)
	req.Header.Set("anthropic-version", anthropicAPIVersion)
	resp, err := s.http.Do(req)
	if err != nil {
		return "", wrapRequest("summarize", "do request", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", readErrorBody("summarize", "do request", resp)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", wrapRequest("summarize", "read body", err)
	}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", wrapStream("summarize", "decode", err)
	}
	for _, c := range out.Content {
		if c.Type == "text" {
			return c.Text, nil
		}
	}
	return "", fmt.Errorf("provider: summarize: empty response: %w", ErrStreamError)
}
