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

// MiMoFreeProvider implements the Provider interface for MiMo free tier
type MiMoFreeProvider struct {
	config *ProviderConfig
	client *http.Client
}

// NewMiMoFreeProvider creates a new MiMo free provider
func NewMiMoFreeProvider(config *ProviderConfig) *MiMoFreeProvider {
	return &MiMoFreeProvider{
		config: config,
		client: &http.Client{Timeout: 120 * time.Second},
	}
}

// Name returns the provider name
func (p *MiMoFreeProvider) Name() string {
	return p.config.Name
}

// ChatCompletion sends a chat completion request
func (p *MiMoFreeProvider) ChatCompletion(ctx context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
	// Check if API key is set
	if p.config.APIKey == "" {
		return nil, fmt.Errorf("MiMo API Key is required. Get your free key at: https://platform.xiaomimimo.com")
	}

	// Set stream to false
	req.Stream = false

	// Make request
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/chat/completions", p.config.BaseURL)
	
	// Log request details
	log.Printf("[MiMoFree] ========== REQUEST ==========")
	log.Printf("[MiMoFree] URL: %s", url)
	log.Printf("[MiMoFree] Method: POST")
	log.Printf("[MiMoFree] Request Body: %s", string(body))
	
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.config.APIKey))
	httpReq.Header.Set("X-Mimo-Source", "mimocode-cli")
	httpReq.Header.Set("User-Agent", "mimocode-cli/1.0")

	// Log headers
	log.Printf("[MiMoFree] Request Headers:")
	for key, values := range httpReq.Header {
		log.Printf("[MiMoFree]   %s: %s", key, strings.Join(values, ", "))
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		log.Printf("[MiMoFree] Request Error: %v", err)
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Log response details
	log.Printf("[MiMoFree] ========== RESPONSE ==========")
	log.Printf("[MiMoFree] Status: %d %s", resp.StatusCode, resp.Status)
	log.Printf("[MiMoFree] Response Headers:")
	for key, values := range resp.Header {
		log.Printf("[MiMoFree]   %s: %s", key, strings.Join(values, ", "))
	}

	// Read response body
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[MiMoFree] Failed to read response body: %v", err)
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	log.Printf("[MiMoFree] Response Body: %s", string(bodyBytes))

	if resp.StatusCode != http.StatusOK {
		// Provide helpful error message for 401
		if resp.StatusCode == 401 {
			return nil, fmt.Errorf("MiMo API Key is invalid or expired. Get a new key at: https://platform.xiaomimimo.com")
		}
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(bodyBytes))
	}

	var result ChatCompletionResponse
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		log.Printf("[MiMoFree] Failed to decode response: %v", err)
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	log.Printf("[MiMoFree] ========== SUCCESS ==========")
	return &result, nil
}

// ChatCompletionStream sends a streaming chat completion request
func (p *MiMoFreeProvider) ChatCompletionStream(ctx context.Context, req *ChatCompletionRequest) (<-chan *ChatCompletionChunk, error) {
	// Check if API key is set
	if p.config.APIKey == "" {
		return nil, fmt.Errorf("MiMo API Key is required. Get your free key at: https://platform.xiaomimimo.com")
	}

	// Set stream to true
	req.Stream = true

	// Make request
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/chat/completions", p.config.BaseURL)
	
	// Log request details
	log.Printf("[MiMoFree Stream] ========== REQUEST ==========")
	log.Printf("[MiMoFree Stream] URL: %s", url)
	log.Printf("[MiMoFree Stream] Method: POST")
	log.Printf("[MiMoFree Stream] Request Body: %s", string(body))
	
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.config.APIKey))
	httpReq.Header.Set("X-Mimo-Source", "mimocode-cli")
	httpReq.Header.Set("User-Agent", "mimocode-cli/1.0")

	// Log headers
	log.Printf("[MiMoFree Stream] Request Headers:")
	for key, values := range httpReq.Header {
		log.Printf("[MiMoFree Stream]   %s: %s", key, strings.Join(values, ", "))
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		log.Printf("[MiMoFree Stream] Request Error: %v", err)
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	// Log response details
	log.Printf("[MiMoFree Stream] ========== RESPONSE ==========")
	log.Printf("[MiMoFree Stream] Status: %d %s", resp.StatusCode, resp.Status)
	log.Printf("[MiMoFree Stream] Response Headers:")
	for key, values := range resp.Header {
		log.Printf("[MiMoFree Stream]   %s: %s", key, strings.Join(values, ", "))
	}

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("[MiMoFree Stream] Error Response Body: %s", string(bodyBytes))
		resp.Body.Close()
		// Provide helpful error message for 401
		if resp.StatusCode == 401 {
			return nil, fmt.Errorf("MiMo API Key is invalid or expired. Get a new key at: https://platform.xiaomimimo.com")
		}
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
				// Process complete lines
				for {
					idx := bytes.IndexByte(buf, '\n')
					if idx < 0 {
						break
					}
					line := string(buf[:idx])
					buf = buf[idx+1:]
					
					line = strings.TrimSpace(line)
					if line == "" {
						continue
					}
					if strings.HasPrefix(line, "data: ") {
						data := strings.TrimPrefix(line, "data: ")
						if data == "[DONE]" {
							log.Printf("[MiMoFree Stream] Received [DONE]")
							return
						}
						var chunk ChatCompletionChunk
						if err := json.Unmarshal([]byte(data), &chunk); err != nil {
							log.Printf("[MiMoFree Stream] Failed to parse chunk: %v, data: %s", err, data)
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
				if err != io.EOF {
					log.Printf("[MiMoFree Stream] Read error: %v", err)
				}
				break
			}
		}
		log.Printf("[MiMoFree Stream] Stream ended")
	}()

	return chunks, nil
}

// ListModels returns available models
func (p *MiMoFreeProvider) ListModels(ctx context.Context) ([]string, error) {
	return []string{
		"mimo-auto",
		"mimo-v2.5-pro",
		"mimo-v2.5",
	}, nil
}

// ValidateModel checks if a model is available
func (p *MiMoFreeProvider) ValidateModel(model string) bool {
	validModels := []string{
		"mimo-auto",
		"mimo-v2.5-pro",
		"mimo-v2.5",
	}
	for _, m := range validModels {
		if strings.EqualFold(m, model) {
			return true
		}
	}
	return true // Allow any model
}
