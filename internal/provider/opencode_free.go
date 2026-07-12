package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// OpenCodeFreeProvider implements the Provider interface for OpenCode free tier
// This is the actual provider behind MiMoCode's "mimo-auto" model
// API: https://opencode.ai/zen/v1 (OpenAI compatible)
type OpenCodeFreeProvider struct {
	config     *ProviderConfig
	client     *http.Client
	freeModels map[string]string // model alias -> actual model name on opencode.ai
}

// NewOpenCodeFreeProvider creates a new OpenCode free provider
func NewOpenCodeFreeProvider(config *ProviderConfig) *OpenCodeFreeProvider {
	return &OpenCodeFreeProvider{
		config: config,
		client: &http.Client{Timeout: 120 * time.Second},
		freeModels: map[string]string{
			// mimo-auto is a routing alias - maps to the best available free model
			"mimo-auto":          "mimo-v2-flash-free",
			"mimo-free":          "mimo-v2-flash-free",
			// Direct model names (as they appear in opencode.ai)
			"mimo-v2-flash-free": "mimo-v2-flash-free",
			"mimo-v2.5-free":     "mimo-v2.5-free",
			"mimo-v2-pro-free":   "mimo-v2-pro-free",
			"mimo-v2-omni-free":  "mimo-v2-omni-free",
		},
	}
}

// Name returns the provider name
func (p *OpenCodeFreeProvider) Name() string {
	return p.config.Name
}

// resolveModel resolves model alias to actual model name on opencode.ai
func (p *OpenCodeFreeProvider) resolveModel(model string) string {
	if actual, ok := p.freeModels[model]; ok {
		log.Printf("[OpenCodeFree] Resolved model: %s -> %s", model, actual)
		return actual
	}
	// Default to mimo-v2-flash-free for unknown models
	log.Printf("[OpenCodeFree] Unknown model %s, defaulting to mimo-v2-flash-free", model)
	return "mimo-v2-flash-free"
}

// ChatCompletion sends a chat completion request
func (p *OpenCodeFreeProvider) ChatCompletion(ctx context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
	// Resolve model alias to actual model name
	actualModel := p.resolveModel(req.Model)
	req.Model = actualModel
	req.Stream = false

	// Make request
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Build URL - opencode.ai uses /v1/chat/completions
	baseURL := strings.TrimSuffix(p.config.BaseURL, "/")
	url := fmt.Sprintf("%s/chat/completions", baseURL)

	log.Printf("[OpenCodeFree] ========== REQUEST ==========")
	log.Printf("[OpenCodeFree] URL: %s", url)
	log.Printf("[OpenCodeFree] Original Model: %s -> Actual: %s", "mimo-auto", actualModel)
	log.Printf("[OpenCodeFree] Request Body: %s", string(body))

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers - OpenCode free tier doesn't require auth
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "mimocode-cli/1.0")

	// Only set auth if API key is provided (optional for free tier)
	if p.config.APIKey != "" {
		httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.config.APIKey))
	}

	log.Printf("[OpenCodeFree] Request Headers:")
	for key, values := range httpReq.Header {
		log.Printf("[OpenCodeFree]   %s: %s", key, strings.Join(values, ", "))
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		log.Printf("[OpenCodeFree] Request Error: %v", err)
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	log.Printf("[OpenCodeFree] ========== RESPONSE ==========")
	log.Printf("[OpenCodeFree] Status: %d %s", resp.StatusCode, resp.Status)

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	log.Printf("[OpenCodeFree] Response Body: %s", string(bodyBytes))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var result ChatCompletionResponse
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	log.Printf("[OpenCodeFree] ========== SUCCESS ==========")
	return &result, nil
}

// ChatCompletionStream sends a streaming chat completion request
func (p *OpenCodeFreeProvider) ChatCompletionStream(ctx context.Context, req *ChatCompletionRequest) (<-chan *ChatCompletionChunk, error) {
	// Resolve model alias to actual model name
	actualModel := p.resolveModel(req.Model)
	req.Model = actualModel
	req.Stream = true

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	baseURL := strings.TrimSuffix(p.config.BaseURL, "/")
	url := fmt.Sprintf("%s/chat/completions", baseURL)

	log.Printf("[OpenCodeFree Stream] ========== REQUEST ==========")
	log.Printf("[OpenCodeFree Stream] URL: %s", url)
	log.Printf("[OpenCodeFree Stream] Model: %s", actualModel)
	log.Printf("[OpenCodeFree Stream] Request Body: %s", string(body))

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("User-Agent", "mimocode-cli/1.0")

	if p.config.APIKey != "" {
		httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.config.APIKey))
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	log.Printf("[OpenCodeFree Stream] Status: %d", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	chunks := make(chan *ChatCompletionChunk, 100)
	go func() {
		defer resp.Body.Close()
		defer close(chunks)

		buf := make([]byte, 0, 4096)
		tmp := make([]byte, 1024)
		for {
			n, err := resp.Body.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)
				for {
					idx := bytes.IndexByte(buf, '\n')
					if idx < 0 {
						break
					}
					line := strings.TrimSpace(string(buf[:idx]))
					buf = buf[idx+1:]

					if line == "" {
						continue
					}
					if strings.HasPrefix(line, "data: ") {
						data := strings.TrimPrefix(line, "data: ")
						if data == "[DONE]" {
							return
						}
						var chunk ChatCompletionChunk
						if err := json.Unmarshal([]byte(data), &chunk); err != nil {
							continue
						}
						select {
						case chunks <- &chunk:
						case <-ctx.Done():
							return
						}
					}
				}
			}
			if err != nil {
				break
			}
		}
	}()

	return chunks, nil
}

// ListModels returns available models
func (p *OpenCodeFreeProvider) ListModels(ctx context.Context) ([]string, error) {
	models := make([]string, 0, len(p.freeModels))
	for alias := range p.freeModels {
		models = append(models, alias)
	}
	return models, nil
}

// ValidateModel checks if a model is available
func (p *OpenCodeFreeProvider) ValidateModel(model string) bool {
	_, ok := p.freeModels[model]
	return ok
}
