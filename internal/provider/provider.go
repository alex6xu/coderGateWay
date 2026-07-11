package provider

import (
	"context"
	"fmt"
	"strings"
)

// Provider represents an LLM provider interface
type Provider interface {
	// Name returns the provider name
	Name() string

	// ChatCompletion sends a chat completion request
	ChatCompletion(ctx context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error)

	// ChatCompletionStream sends a streaming chat completion request
	ChatCompletionStream(ctx context.Context, req *ChatCompletionRequest) (<-chan *ChatCompletionChunk, error)

	// ListModels returns available models
	ListModels(ctx context.Context) ([]string, error)

	// ValidateModel checks if a model is available
	ValidateModel(model string) bool
}

// ChatCompletionRequest represents a chat completion request
type ChatCompletionRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature *float64  `json:"temperature,omitempty"`
	MaxTokens   *int      `json:"max_tokens,omitempty"`
	TopP        *float64  `json:"top_p,omitempty"`
	Stream      bool      `json:"stream"`
	Tools       []Tool    `json:"tools,omitempty"`
}

// Message represents a chat message
type Message struct {
	Role       string      `json:"role"`
	Content    string      `json:"content"`
	Name       string      `json:"name,omitempty"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
}

// Tool represents a tool definition
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolFunction represents a tool function
type ToolFunction struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"`
}

// ToolCall represents a tool call
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ChatCompletionResponse represents a chat completion response
type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// Choice represents a choice in the response
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// Usage represents token usage
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatCompletionChunk represents a streaming chunk
type ChatCompletionChunk struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []ChunkChoice `json:"choices"`
}

// ChunkChoice represents a choice in a streaming chunk
type ChunkChoice struct {
	Index        int          `json:"index"`
	Delta        MessageDelta `json:"delta"`
	FinishReason *string      `json:"finish_reason"`
}

// MessageDelta represents a delta in a streaming message
type MessageDelta struct {
	Role      string     `json:"role,omitempty"`
	Content   string     `json:"content,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// ProviderType represents the type of provider
type ProviderType string

const (
	ProviderTypeOpenAI   ProviderType = "openai"
	ProviderTypeClaude   ProviderType = "claude"
	ProviderTypeGemini   ProviderType = "gemini"
	ProviderTypeDeepSeek ProviderType = "deepseek"
	ProviderTypeOllama   ProviderType = "ollama"
	ProviderTypeMiMo     ProviderType = "mimo"
	ProviderTypeCustom   ProviderType = "custom"
)

// ProviderConfig represents provider configuration
type ProviderConfig struct {
	Name    string       `json:"name"`
	Type    ProviderType `json:"type"`
	BaseURL string       `json:"base_url"`
	APIKey  string       `json:"api_key"`
	Models  []string     `json:"models"`
}

// Registry manages providers
type Registry struct {
	providers map[string]Provider
	configs   map[string]*ProviderConfig
}

// NewRegistry creates a new provider registry
func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]Provider),
		configs:   make(map[string]*ProviderConfig),
	}
}

// Register registers a provider
func (r *Registry) Register(config *ProviderConfig) error {
	var provider Provider

	switch config.Type {
	case ProviderTypeOpenAI:
		provider = NewOpenAIProvider(config)
	case ProviderTypeClaude:
		provider = NewClaudeProvider(config)
	case ProviderTypeGemini:
		provider = NewGeminiProvider(config)
	case ProviderTypeDeepSeek:
		provider = NewDeepSeekProvider(config)
	case ProviderTypeOllama:
		provider = NewOllamaProvider(config)
	case ProviderTypeMiMo:
		provider = NewOpenAIProvider(config) // MiMo uses OpenAI compatible API
	case ProviderTypeCustom:
		provider = NewCustomProvider(config)
	default:
		return fmt.Errorf("unsupported provider type: %s", config.Type)
	}

	r.providers[config.Name] = provider
	r.configs[config.Name] = config
	return nil
}

// Get returns a provider by name
func (r *Registry) Get(name string) (Provider, error) {
	provider, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider not found: %s", name)
	}
	return provider, nil
}

// List returns all registered providers
func (r *Registry) List() []string {
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	return names
}

// GetProviderForModel returns the best provider for a given model
func (r *Registry) GetProviderForModel(model string) (Provider, error) {
	// Try to find a provider that supports the model
	for name, config := range r.configs {
		for _, m := range config.Models {
			if strings.EqualFold(m, model) {
				return r.providers[name], nil
			}
		}
	}

	// If no specific provider found, try to infer from model name
	if strings.HasPrefix(model, "gpt-") || strings.HasPrefix(model, "o1-") {
		return r.Get("openai")
	}
	if strings.HasPrefix(model, "claude-") {
		return r.Get("claude")
	}
	if strings.HasPrefix(model, "gemini-") {
		return r.Get("gemini")
	}
	if strings.HasPrefix(model, "deepseek-") {
		return r.Get("deepseek")
	}

	return nil, fmt.Errorf("no provider found for model: %s", model)
}
