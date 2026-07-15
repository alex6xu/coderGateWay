package provider

import (
	"context"
	"strings"
)

// DeepSeekProvider is OpenAI-compatible (including automatic prompt caching on DeepSeek).
type DeepSeekProvider struct {
	*OpenAIProvider
}

// NewDeepSeekProvider creates a DeepSeek provider backed by the OpenAI-compatible client.
func NewDeepSeekProvider(config *ProviderConfig) *DeepSeekProvider {
	if config.BaseURL == "" {
		config.BaseURL = "https://api.deepseek.com/v1"
	}
	return &DeepSeekProvider{OpenAIProvider: NewOpenAIProvider(config)}
}

func (p *DeepSeekProvider) ListModels(ctx context.Context) ([]string, error) {
	return []string{
		"deepseek-chat",
		"deepseek-coder",
		"deepseek-reasoner",
	}, nil
}

func (p *DeepSeekProvider) ValidateModel(model string) bool {
	for _, m := range []string{"deepseek-chat", "deepseek-coder", "deepseek-reasoner"} {
		if strings.EqualFold(m, model) {
			return true
		}
	}
	return strings.HasPrefix(strings.ToLower(model), "deepseek-")
}
