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

// MiMoAutoProvider implements the Provider interface for MiMo free tier
// Endpoint: https://api.xiaomimimo.com/api/free-ai/openai
type MiMoAutoProvider struct {
	config *ProviderConfig
	client *http.Client
}

// NewMiMoAutoProvider creates a new MiMo Auto provider
func NewMiMoAutoProvider(config *ProviderConfig) *MiMoAutoProvider {
	return &MiMoAutoProvider{
		config: config,
		client: &http.Client{Timeout: 120 * time.Second},
	}
}

// Name returns the provider name
func (p *MiMoAutoProvider) Name() string {
	return p.config.Name
}

// ChatCompletion sends a chat completion request
func (p *MiMoAutoProvider) ChatCompletion(ctx context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
	req.Stream = false

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// MiMo free AI endpoint
	baseURL := strings.TrimSuffix(p.config.BaseURL, "/")
	url := fmt.Sprintf("%s/chat/completions", baseURL)

	log.Printf("[MiMoAuto] ========== REQUEST ==========")
	log.Printf("[MiMoAuto] URL: %s", url)
	log.Printf("[MiMoAuto] Model: %s", req.Model)
	log.Printf("[MiMoAuto] Request Body: %s", string(body))

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "mimocode-cli/1.0")

	if p.config.APIKey != "" {
		httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.config.APIKey))
	}

	log.Printf("[MiMoAuto] Request Headers:")
	for key, values := range httpReq.Header {
		log.Printf("[MiMoAuto]   %s: %s", key, strings.Join(values, ", "))
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		log.Printf("[MiMoAuto] Request Error: %v", err)
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	log.Printf("[MiMoAuto] ========== RESPONSE ==========")
	log.Printf("[MiMoAuto] Status: %d %s", resp.StatusCode, resp.Status)

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	log.Printf("[MiMoAuto] Response Body: %s", string(bodyBytes))

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var result ChatCompletionResponse
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	log.Printf("[MiMoAuto] ========== SUCCESS ==========")
	return &result, nil
}

// ChatCompletionStream sends a streaming chat completion request
func (p *MiMoAutoProvider) ChatCompletionStream(ctx context.Context, req *ChatCompletionRequest) (<-chan *ChatCompletionChunk, error) {
	req.Stream = true

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	baseURL := strings.TrimSuffix(p.config.BaseURL, "/")
	url := fmt.Sprintf("%s/chat/completions", baseURL)

	log.Printf("[MiMoAuto Stream] ========== REQUEST ==========")
	log.Printf("[MiMoAuto Stream] URL: %s", url)
	log.Printf("[MiMoAuto Stream] Model: %s", req.Model)
	log.Printf("[MiMoAuto Stream] Request Body: %s", string(body))

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

	log.Printf("[MiMoAuto Stream] Status: %d", resp.StatusCode)

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
func (p *MiMoAutoProvider) ListModels(ctx context.Context) ([]string, error) {
	return []string{
		"mimo-auto",
	}, nil
}

// ValidateModel checks if a model is available
func (p *MiMoAutoProvider) ValidateModel(model string) bool {
	return strings.EqualFold(model, "mimo-auto")
}
