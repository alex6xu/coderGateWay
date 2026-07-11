package provider

import (
	"context"
	"fmt"
)

// DeepSeekProvider implements the Provider interface for DeepSeek
type DeepSeekProvider struct {
	config *ProviderConfig
}

// NewDeepSeekProvider creates a new DeepSeek provider
func NewDeepSeekProvider(config *ProviderConfig) *DeepSeekProvider {
	return &DeepSeekProvider{config: config}
}

// Name returns the provider name
func (p *DeepSeekProvider) Name() string {
	return p.config.Name
}

// ChatCompletion sends a chat completion request
func (p *DeepSeekProvider) ChatCompletion(ctx context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
	// TODO: Implement DeepSeek API
	return nil, fmt.Errorf("deepseek provider not implemented yet")
}

// ChatCompletionStream sends a streaming chat completion request
func (p *DeepSeekProvider) ChatCompletionStream(ctx context.Context, req *ChatCompletionRequest) (<-chan *ChatCompletionChunk, error) {
	// TODO: Implement DeepSeek streaming API
	return nil, fmt.Errorf("deepseek provider not implemented yet")
}

// ListModels returns available models
func (p *DeepSeekProvider) ListModels(ctx context.Context) ([]string, error) {
	return []string{
		"deepseek-chat",
		"deepseek-coder",
		"deepseek-reasoner",
	}, nil
}

// ValidateModel checks if a model is available
func (p *DeepSeekProvider) ValidateModel(model string) bool {
	for _, m := range []string{
		"deepseek-chat",
		"deepseek-coder",
		"deepseek-reasoner",
	} {
		if m == model {
			return true
		}
	}
	return false
}
