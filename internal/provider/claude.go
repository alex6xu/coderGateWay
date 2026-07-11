package provider

import (
	"context"
	"fmt"
)

// ClaudeProvider implements the Provider interface for Anthropic Claude
type ClaudeProvider struct {
	config *ProviderConfig
}

// NewClaudeProvider creates a new Claude provider
func NewClaudeProvider(config *ProviderConfig) *ClaudeProvider {
	return &ClaudeProvider{config: config}
}

// Name returns the provider name
func (p *ClaudeProvider) Name() string {
	return p.config.Name
}

// ChatCompletion sends a chat completion request
func (p *ClaudeProvider) ChatCompletion(ctx context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
	// TODO: Implement Claude API
	return nil, fmt.Errorf("claude provider not implemented yet")
}

// ChatCompletionStream sends a streaming chat completion request
func (p *ClaudeProvider) ChatCompletionStream(ctx context.Context, req *ChatCompletionRequest) (<-chan *ChatCompletionChunk, error) {
	// TODO: Implement Claude streaming API
	return nil, fmt.Errorf("claude provider not implemented yet")
}

// ListModels returns available models
func (p *ClaudeProvider) ListModels(ctx context.Context) ([]string, error) {
	return []string{
		"claude-3-opus-20240229",
		"claude-3-sonnet-20240229",
		"claude-3-haiku-20240307",
		"claude-3-5-sonnet-20241022",
	}, nil
}

// ValidateModel checks if a model is available
func (p *ClaudeProvider) ValidateModel(model string) bool {
	for _, m := range []string{
		"claude-3-opus-20240229",
		"claude-3-sonnet-20240229",
		"claude-3-haiku-20240307",
		"claude-3-5-sonnet-20241022",
	} {
		if m == model {
			return true
		}
	}
	return false
}
