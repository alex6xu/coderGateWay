package provider

import (
	"context"
	"fmt"
)

// CustomProvider implements the Provider interface for custom endpoints
type CustomProvider struct {
	config *ProviderConfig
}

// NewCustomProvider creates a new custom provider
func NewCustomProvider(config *ProviderConfig) *CustomProvider {
	return &CustomProvider{config: config}
}

// Name returns the provider name
func (p *CustomProvider) Name() string {
	return p.config.Name
}

// ChatCompletion sends a chat completion request
func (p *CustomProvider) ChatCompletion(ctx context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
	// TODO: Implement custom API (OpenAI compatible)
	return nil, fmt.Errorf("custom provider not implemented yet")
}

// ChatCompletionStream sends a streaming chat completion request
func (p *CustomProvider) ChatCompletionStream(ctx context.Context, req *ChatCompletionRequest) (<-chan *ChatCompletionChunk, error) {
	// TODO: Implement custom streaming API
	return nil, fmt.Errorf("custom provider not implemented yet")
}

// ListModels returns available models
func (p *CustomProvider) ListModels(ctx context.Context) ([]string, error) {
	return p.config.Models, nil
}

// ValidateModel checks if a model is available
func (p *CustomProvider) ValidateModel(model string) bool {
	if len(p.config.Models) == 0 {
		return true
	}
	for _, m := range p.config.Models {
		if m == model {
			return true
		}
	}
	return false
}
