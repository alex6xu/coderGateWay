package provider

import (
	"context"
	"fmt"
)

// OllamaProvider implements the Provider interface for Ollama
type OllamaProvider struct {
	config *ProviderConfig
}

// NewOllamaProvider creates a new Ollama provider
func NewOllamaProvider(config *ProviderConfig) *OllamaProvider {
	return &OllamaProvider{config: config}
}

// Name returns the provider name
func (p *OllamaProvider) Name() string {
	return p.config.Name
}

// ChatCompletion sends a chat completion request
func (p *OllamaProvider) ChatCompletion(ctx context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
	// TODO: Implement Ollama API
	return nil, fmt.Errorf("ollama provider not implemented yet")
}

// ChatCompletionStream sends a streaming chat completion request
func (p *OllamaProvider) ChatCompletionStream(ctx context.Context, req *ChatCompletionRequest) (<-chan *ChatCompletionChunk, error) {
	// TODO: Implement Ollama streaming API
	return nil, fmt.Errorf("ollama provider not implemented yet")
}

// ListModels returns available models
func (p *OllamaProvider) ListModels(ctx context.Context) ([]string, error) {
	// TODO: Query Ollama for available models
	return []string{}, nil
}

// ValidateModel checks if a model is available
func (p *OllamaProvider) ValidateModel(model string) bool {
	return true
}
