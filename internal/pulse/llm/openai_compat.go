package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// apiProvider implements Provider for any OpenAI-compatible chat completions
// API, selected by URL rather than by a hardcoded vendor list. Any model name
// the endpoint accepts works, so a new model needs a config edit, not a release.
type apiProvider struct {
	baseURL string
	apiKey  string
	model   string
}

func (p *apiProvider) Name() string { return "api:" + p.model }

type oaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type oaiRequest struct {
	Model     string       `json:"model"`
	MaxTokens int          `json:"max_tokens"`
	Messages  []oaiMessage `json:"messages"`
}

type oaiResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (p *apiProvider) Call(ctx context.Context, systemPrompt, userMessage string) (Response, error) {
	body, err := json.Marshal(oaiRequest{
		Model: p.model,
		// A nudge is one sentence; a large ceiling only buys latency here.
		MaxTokens: 512,
		Messages: []oaiMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userMessage},
		},
	})
	if err != nil {
		return Response{}, fmt.Errorf("marshal request: %w", err)
	}

	// Accept a base URL with or without the path, so both
	// "https://host/v1" and "https://host/v1/chat/completions" work.
	url := strings.TrimRight(p.baseURL, "/")
	if !strings.HasSuffix(url, "/chat/completions") {
		url += "/chat/completions"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	// The caller's context carries the deadline; a nudge that arrives late is
	// worse than one that arrives plain.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Response{}, fmt.Errorf("http call: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Response{}, fmt.Errorf("read response: %w", err)
	}

	var parsed oaiResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Response{}, fmt.Errorf("unmarshal response (status %d): %w", resp.StatusCode, err)
	}
	if parsed.Error != nil {
		return Response{}, fmt.Errorf("provider error: %s", parsed.Error.Message)
	}
	// A non-2xx with no parseable error field still has to fail loudly.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Response{}, fmt.Errorf("provider returned %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	if len(parsed.Choices) == 0 {
		return Response{}, fmt.Errorf("provider returned no choices: %s", truncate(string(raw), 200))
	}

	return Response{
		Text:      parsed.Choices[0].Message.Content,
		TokensIn:  parsed.Usage.PromptTokens,
		TokensOut: parsed.Usage.CompletionTokens,
	}, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
