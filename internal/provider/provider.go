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
	Model            string         `json:"model"`
	Messages         []Message      `json:"messages"`
	Temperature      *float64       `json:"temperature,omitempty"`
	MaxTokens        *int           `json:"max_tokens,omitempty"`
	TopP             *float64       `json:"top_p,omitempty"`
	Stream           bool           `json:"stream"`
	Tools            []Tool         `json:"tools,omitempty"`
	PromptCacheKey   string         `json:"prompt_cache_key,omitempty"`
	StreamOptions    *StreamOptions `json:"stream_options,omitempty"`
	// EnablePromptCache hints providers that support explicit cache markers (e.g. Anthropic).
	EnablePromptCache bool `json:"-"`
	// SessionID carries an upstream conversation identifier (from the X-Session-Id
	// header) used to derive a stable per-session X-Session-Affinity for the free
	// mimo-auto endpoint. Not serialized to the upstream request body.
	SessionID string `json:"-"`
}

// StreamOptions controls streaming extras (OpenAI-compatible).
type StreamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

// Message represents a chat message
type Message struct {
	Role         string        `json:"role"`
	Content      string        `json:"content"`
	Name         string        `json:"name,omitempty"`
	ToolCalls    []ToolCall    `json:"tool_calls,omitempty"`
	ToolCallID   string        `json:"tool_call_id,omitempty"`
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

// CacheControl marks a message/block for provider-side prompt caching (Anthropic-style).
type CacheControl struct {
	Type string `json:"type"` // "ephemeral"
}

// Tool represents a tool definition
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolFunction represents a tool function
type ToolFunction struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Parameters  interface{} `json:"parameters,omitempty"`
	Arguments   string      `json:"arguments,omitempty"` // present on tool_call responses
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
	PromptTokens        int                 `json:"prompt_tokens"`
	CompletionTokens    int                 `json:"completion_tokens"`
	TotalTokens         int                 `json:"total_tokens"`
	CachedTokens        int                 `json:"cached_tokens,omitempty"`
	PromptTokensDetails *PromptTokenDetails `json:"prompt_tokens_details,omitempty"`
}

// PromptTokenDetails carries cache hit info from OpenAI-compatible APIs.
type PromptTokenDetails struct {
	CachedTokens int `json:"cached_tokens,omitempty"`
}

// Normalize fills CachedTokens from nested details when present.
func (u *Usage) Normalize() {
	if u == nil {
		return
	}
	if u.CachedTokens == 0 && u.PromptTokensDetails != nil {
		u.CachedTokens = u.PromptTokensDetails.CachedTokens
	}
	if u.TotalTokens == 0 && (u.PromptTokens > 0 || u.CompletionTokens > 0) {
		u.TotalTokens = u.PromptTokens + u.CompletionTokens
	}
}

// Add accumulates usage from another response turn.
func (u *Usage) Add(other Usage) {
	other.Normalize()
	u.PromptTokens += other.PromptTokens
	u.CompletionTokens += other.CompletionTokens
	u.TotalTokens += other.TotalTokens
	u.CachedTokens += other.CachedTokens
}

// ChatCompletionChunk represents a streaming chunk
type ChatCompletionChunk struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Created int64         `json:"created"`
	Model   string        `json:"model"`
	Choices []ChunkChoice `json:"choices"`
	Usage   *Usage        `json:"usage,omitempty"`
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

// ApplyPromptCache sets OpenAI-compatible cache key and Anthropic-style markers on
// the stable prefix (first system message, optional second system/checkpoint).
func ApplyPromptCache(req *ChatCompletionRequest, cacheKey string) {
	if req == nil {
		return
	}
	req.EnablePromptCache = true
	if cacheKey != "" {
		req.PromptCacheKey = cacheKey
	}
	// Mark the last stable system message for Anthropic-style caching.
	lastSys := -1
	for i, m := range req.Messages {
		if m.Role == "system" {
			lastSys = i
		} else {
			break // systems only at the front in our assembly
		}
	}
	if lastSys >= 0 {
		req.Messages[lastSys].CacheControl = &CacheControl{Type: "ephemeral"}
	}
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
	ProviderTypeMiMoFree ProviderType = "mimo-free"
	ProviderTypeAgnes    ProviderType = "agnes"
	ProviderTypeGLM      ProviderType = "glm"
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

// NewProvider creates a provider from configuration.
func NewProvider(config *ProviderConfig) (Provider, error) {
	switch config.Type {
	case ProviderTypeOpenAI:
		return NewOpenAIProvider(config), nil
	case ProviderTypeClaude:
		return NewClaudeProvider(config), nil
	case ProviderTypeGemini:
		return NewGeminiProvider(config), nil
	case ProviderTypeDeepSeek:
		return NewDeepSeekProvider(config), nil
	case ProviderTypeOllama:
		return NewOllamaProvider(config), nil
	case ProviderTypeMiMo:
		return NewOpenAIProvider(config), nil
	case ProviderTypeMiMoFree:
		return NewMiMoFreeProvider(config), nil
	case ProviderTypeAgnes, ProviderTypeGLM:
		return NewOpenAIProvider(config), nil
	case ProviderTypeCustom:
		return NewCustomProvider(config), nil
	default:
		return nil, fmt.Errorf("unsupported provider type: %s", config.Type)
	}
}

// Register registers a provider
func (r *Registry) Register(config *ProviderConfig) error {
	provider, err := NewProvider(config)
	if err != nil {
		return err
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
	for name, config := range r.configs {
		for _, m := range config.Models {
			if strings.EqualFold(m, model) {
				return r.providers[name], nil
			}
		}
	}

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
	if strings.HasPrefix(model, "agnes-") {
		return r.Get("agnes")
	}
	if strings.HasPrefix(model, "glm-") {
		return r.Get("glm")
	}

	return nil, fmt.Errorf("no provider found for model: %s", model)
}
