package provider

import (
	"context"
	"fmt"
)

// GeminiProvider implements the Provider interface for Google Gemini
type GeminiProvider struct {
	config *ProviderConfig
}

// NewGeminiProvider creates a new Gemini provider
func NewGeminiProvider(config *ProviderConfig) *GeminiProvider {
	return &GeminiProvider{config: config}
}

// Name returns the provider name
func (p *GeminiProvider) Name() string {
	return p.config.Name
}

// ChatCompletion sends a chat completion request
func (p *GeminiProvider) ChatCompletion(ctx context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
	// TODO: Implement Gemini API
	return nil, fmt.Errorf("gemini provider not implemented yet")
}

// ChatCompletionStream sends a streaming chat completion request
func (p *GeminiProvider) ChatCompletionStream(ctx context.Context, req *ChatCompletionRequest) (<-chan *ChatCompletionChunk, error) {
	// TODO: Implement Gemini streaming API
	return nil, fmt.Errorf("gemini provider not implemented yet")
}

// ListModels returns available models
func (p *GeminiProvider) ListModels(ctx context.Context) ([]string, error) {
	return []string{
		"gemini-pro",
		"gemini-pro-vision",
		"gemini-ultra",
	}, nil
}

// ValidateModel checks if a model is available
func (p *GeminiProvider) ValidateModel(model string) bool {
	for _, m := range []string{
		"gemini-pro",
		"gemini-pro-vision",
		"gemini-ultra",
	} {
		if m == model {
			return true
		}
	}
	return false
}
