package provider

import (
	"context"
)

// CustomProvider implements the Provider interface for custom OpenAI-compatible endpoints.
type CustomProvider struct {
	delegate *OpenAIProvider
}

// NewCustomProvider creates a new custom provider backed by the OpenAI adapter.
func NewCustomProvider(config *ProviderConfig) *CustomProvider {
	return &CustomProvider{delegate: NewOpenAIProvider(config)}
}

func (p *CustomProvider) Name() string {
	return p.delegate.Name()
}

func (p *CustomProvider) ChatCompletion(ctx context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
	return p.delegate.ChatCompletion(ctx, req)
}

func (p *CustomProvider) ChatCompletionStream(ctx context.Context, req *ChatCompletionRequest) (<-chan *ChatCompletionChunk, error) {
	return p.delegate.ChatCompletionStream(ctx, req)
}

func (p *CustomProvider) ListModels(ctx context.Context) ([]string, error) {
	return p.delegate.ListModels(ctx)
}

func (p *CustomProvider) ValidateModel(model string) bool {
	return p.delegate.ValidateModel(model)
}
