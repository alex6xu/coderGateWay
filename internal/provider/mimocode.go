package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// MiMoCodeProvider implements the Provider interface for MiMoCode session API
type MiMoCodeProvider struct {
	config     *ProviderConfig
	client     *http.Client
	sessionID  string
}

// NewMiMoCodeProvider creates a new MiMoCode provider
func NewMiMoCodeProvider(config *ProviderConfig) *MiMoCodeProvider {
	return &MiMoCodeProvider{
		config: config,
		client: &http.Client{Timeout: 300 * time.Second},
	}
}

// Name returns the provider name
func (p *MiMoCodeProvider) Name() string {
	return p.config.Name
}

// ChatCompletion sends a chat completion request
func (p *MiMoCodeProvider) ChatCompletion(ctx context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
	// Create session
	sessionID, err := p.createSession(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}
	defer p.deleteSession(ctx, sessionID)

	// Build prompt parts from messages
	parts := p.buildPromptParts(req.Messages)
	systemPrompt := p.extractSystemPrompt(req.Messages)

	// Send prompt
	if err := p.sendPrompt(ctx, sessionID, req.Model, systemPrompt, parts); err != nil {
		return nil, fmt.Errorf("failed to send prompt: %w", err)
	}

	// Poll for response
	content, err := p.pollForResponse(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get response: %w", err)
	}

	// Build response
	promptTokens := estimateTokens(parts)
	completionTokens := estimateTokens([]map[string]interface{}{{"text": content}})

	return &ChatCompletionResponse{
		ID:      fmt.Sprintf("chatcmpl-%d", time.Now().UnixMilli()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []Choice{
			{
				Index: 0,
				Message: Message{
					Role:    "assistant",
					Content: content,
				},
				FinishReason: "stop",
			},
		},
		Usage: Usage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
		},
	}, nil
}

// ChatCompletionStream sends a streaming chat completion request
func (p *MiMoCodeProvider) ChatCompletionStream(ctx context.Context, req *ChatCompletionRequest) (<-chan *ChatCompletionChunk, error) {
	// For now, implement non-streaming and return as single chunk
	resp, err := p.ChatCompletion(ctx, req)
	if err != nil {
		return nil, err
	}

	chunks := make(chan *ChatCompletionChunk, 1)
	go func() {
		defer close(chunks)
		
		content := ""
		if len(resp.Choices) > 0 {
			content = resp.Choices[0].Message.Content
		}

		chunks <- &ChatCompletionChunk{
			ID:      resp.ID,
			Object:  "chat.completion.chunk",
			Created: resp.Created,
			Model:   resp.Model,
			Choices: []ChunkChoice{
				{
					Index: 0,
					Delta: MessageDelta{
						Role:    "assistant",
						Content: content,
					},
					FinishReason: stringPtr("stop"),
				},
			},
		}
	}()

	return chunks, nil
}

// ListModels returns available models
func (p *MiMoCodeProvider) ListModels(ctx context.Context) ([]string, error) {
	return []string{
		"mimo/mimo-auto",
		"mimo/mimo-v2.5-pro",
		"mimo/mimo-v2.5",
	}, nil
}

// ValidateModel checks if a model is available
func (p *MiMoCodeProvider) ValidateModel(model string) bool {
	validModels := []string{
		"mimo/mimo-auto",
		"mimo/mimo-v2.5-pro",
		"mimo/mimo-v2.5",
		"mimo-auto",
		"mimo-v2.5-pro",
		"mimo-v2.5",
	}
	for _, m := range validModels {
		if m == model {
			return true
		}
	}
	return true // Allow any model
}

// createSession creates a new MiMoCode session
func (p *MiMoCodeProvider) createSession(ctx context.Context) (string, error) {
	url := fmt.Sprintf("%s/session/create", p.config.BaseURL)
	
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader([]byte("{}")))
	if err != nil {
		return "", err
	}
	
	req.Header.Set("Content-Type", "application/json")
	if p.config.APIKey != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.config.APIKey))
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("create session failed (status %d): %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	if result.Data.ID == "" {
		return "", fmt.Errorf("no session id returned")
	}

	return result.Data.ID, nil
}

// deleteSession deletes a MiMoCode session
func (p *MiMoCodeProvider) deleteSession(ctx context.Context, sessionID string) {
	url := fmt.Sprintf("%s/session/%s", p.config.BaseURL, sessionID)
	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return
	}
	if p.config.APIKey != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.config.APIKey))
	}
	p.client.Do(req)
}

// sendPrompt sends a prompt to a MiMoCode session
func (p *MiMoCodeProvider) sendPrompt(ctx context.Context, sessionID, model, systemPrompt string, parts []map[string]interface{}) error {
	url := fmt.Sprintf("%s/session/%s/prompt", p.config.BaseURL, sessionID)

	// Parse model
	providerID := "mimo"
	modelID := "mimo-auto"
	if model != "" {
		// Try to parse provider/model format
		for i, c := range model {
			if c == '/' {
				providerID = model[:i]
				modelID = model[i+1:]
				break
			}
		}
	}

	body := map[string]interface{}{
		"model": map[string]string{
			"providerID": providerID,
			"modelID":    modelID,
		},
		"parts": parts,
	}

	if systemPrompt != "" {
		body["system"] = systemPrompt
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	if p.config.APIKey != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.config.APIKey))
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("send prompt failed (status %d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// pollForResponse polls for the response from a MiMoCode session
func (p *MiMoCodeProvider) pollForResponse(ctx context.Context, sessionID string) (string, error) {
	url := fmt.Sprintf("%s/session/%s/messages", p.config.BaseURL, sessionID)
	
	deadline := time.Now().Add(180 * time.Second)
	
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return "", err
		}
		
		if p.config.APIKey != "" {
			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.config.APIKey))
		}

		resp, err := p.client.Do(req)
		if err != nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			time.Sleep(500 * time.Millisecond)
			continue
		}

		var result struct {
			Data []struct {
				Info struct {
					Role   string `json:"role"`
					Finish bool   `json:"finish"`
				} `json:"info"`
				Parts []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"data"`
		}
		
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			time.Sleep(500 * time.Millisecond)
			continue
		}
		resp.Body.Close()

		// Look for assistant response
		for i := len(result.Data) - 1; i >= 0; i-- {
			msg := result.Data[i]
			if msg.Info.Role == "assistant" && msg.Info.Finish {
				content := ""
				for _, part := range msg.Parts {
					if part.Type == "text" {
						content += part.Text
					}
				}
				if content != "" {
					return content, nil
				}
			}
		}

		time.Sleep(500 * time.Millisecond)
	}

	return "", fmt.Errorf("timeout waiting for response")
}

// buildPromptParts builds prompt parts from messages
func (p *MiMoCodeProvider) buildPromptParts(messages []Message) []map[string]interface{} {
	parts := make([]map[string]interface{}, 0)
	
	for _, msg := range messages {
		if msg.Role == "system" {
			continue // Handle system separately
		}
		
		if msg.Content != "" {
			parts = append(parts, map[string]interface{}{
				"type": "text",
				"text": fmt.Sprintf("%s: %s", msg.Role, msg.Content),
			})
		}
	}
	
	return parts
}

// extractSystemPrompt extracts system prompt from messages
func (p *MiMoCodeProvider) extractSystemPrompt(messages []Message) string {
	for _, msg := range messages {
		if msg.Role == "system" {
			return msg.Content
		}
	}
	return ""
}

// estimateTokens estimates token count
func estimateTokens(parts []map[string]interface{}) int {
	total := 0
	for _, part := range parts {
		if text, ok := part["text"].(string); ok {
			total += len(text) / 4 // Rough estimate
		}
	}
	return total
}

func stringPtr(s string) *string {
	return &s
}
