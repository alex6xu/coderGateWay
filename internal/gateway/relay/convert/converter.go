package convert

import (
	"fmt"
	"strings"

	"github.com/alex/codegateway/internal/provider"
)

// FormatConverter handles format conversion between different API formats
type FormatConverter struct{}

// NewFormatConverter creates a new format converter
func NewFormatConverter() *FormatConverter {
	return &FormatConverter{}
}

// ConvertRequest converts a request from one format to another
func (c *FormatConverter) ConvertRequest(req *provider.ChatCompletionRequest, from, to string) (*provider.ChatCompletionRequest, error) {
	if from == to {
		return req, nil
	}

	switch {
	case from == "openai" && to == "claude":
		return c.openAIToClaude(req)
	case from == "openai" && to == "gemini":
		return c.openAIToGemini(req)
	case from == "claude" && to == "openai":
		return c.claudeToOpenAI(req)
	case from == "gemini" && to == "openai":
		return c.geminiToOpenAI(req)
	default:
		return nil, fmt.Errorf("unsupported conversion: %s -> %s", from, to)
	}
}

// ConvertResponse converts a response from one format to another
func (c *FormatConverter) ConvertResponse(resp *provider.ChatCompletionResponse, from, to string) (*provider.ChatCompletionResponse, error) {
	if from == to {
		return resp, nil
	}

	// For now, responses are already in OpenAI format
	// TODO: Implement response format conversion
	return resp, nil
}

// openAIToClaude converts OpenAI format to Claude format
func (c *FormatConverter) openAIToClaude(req *provider.ChatCompletionRequest) (*provider.ChatCompletionRequest, error) {
	claudeReq := &provider.ChatCompletionRequest{
		Model:       c.mapModelName(req.Model, "openai", "claude"),
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		TopP:        req.TopP,
		Stream:      req.Stream,
	}

	// Convert messages
	claudeMessages := make([]provider.Message, 0)
	systemMessage := ""

	for _, msg := range req.Messages {
		switch msg.Role {
		case "system":
			systemMessage = msg.Content
		case "user":
			claudeMessages = append(claudeMessages, provider.Message{
				Role:    "user",
				Content: msg.Content,
			})
		case "assistant":
			claudeMessages = append(claudeMessages, provider.Message{
				Role:    "assistant",
				Content: msg.Content,
			})
		case "tool":
			// Convert tool results to user messages for Claude
			claudeMessages = append(claudeMessages, provider.Message{
				Role:    "user",
				Content: fmt.Sprintf("[Tool Result]\n%s", msg.Content),
			})
		}
	}

	// Add system message as first user message if present
	if systemMessage != "" {
		claudeMessages = append([]provider.Message{
			{Role: "user", Content: systemMessage},
		}, claudeMessages...)
	}

	claudeReq.Messages = claudeMessages
	return claudeReq, nil
}

// claudeToOpenAI converts Claude format to OpenAI format
func (c *FormatConverter) claudeToOpenAI(req *provider.ChatCompletionRequest) (*provider.ChatCompletionRequest, error) {
	openaiReq := &provider.ChatCompletionRequest{
		Model:       c.mapModelName(req.Model, "claude", "openai"),
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		TopP:        req.TopP,
		Stream:      req.Stream,
		Tools:       req.Tools,
	}

	// Convert messages
	openaiMessages := make([]provider.Message, 0)

	for _, msg := range req.Messages {
		switch msg.Role {
		case "user":
			openaiMessages = append(openaiMessages, provider.Message{
				Role:    "user",
				Content: msg.Content,
			})
		case "assistant":
			openaiMessages = append(openaiMessages, provider.Message{
				Role:    "assistant",
				Content: msg.Content,
			})
		}
	}

	openaiReq.Messages = openaiMessages
	return openaiReq, nil
}

// openAIToGemini converts OpenAI format to Gemini format
func (c *FormatConverter) openAIToGemini(req *provider.ChatCompletionRequest) (*provider.ChatCompletionRequest, error) {
	geminiReq := &provider.ChatCompletionRequest{
		Model:       c.mapModelName(req.Model, "openai", "gemini"),
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		TopP:        req.TopP,
		Stream:      req.Stream,
	}

	// Convert messages
	geminiMessages := make([]provider.Message, 0)
	systemMessage := ""

	for _, msg := range req.Messages {
		switch msg.Role {
		case "system":
			systemMessage = msg.Content
		case "user":
			geminiMessages = append(geminiMessages, provider.Message{
				Role:    "user",
				Content: msg.Content,
			})
		case "assistant":
			geminiMessages = append(geminiMessages, provider.Message{
				Role:    "model",
				Content: msg.Content,
			})
		}
	}

	// Add system message as first user message if present
	if systemMessage != "" {
		geminiMessages = append([]provider.Message{
			{Role: "user", Content: systemMessage},
		}, geminiMessages...)
	}

	geminiReq.Messages = geminiMessages
	return geminiReq, nil
}

// geminiToOpenAI converts Gemini format to OpenAI format
func (c *FormatConverter) geminiToOpenAI(req *provider.ChatCompletionRequest) (*provider.ChatCompletionRequest, error) {
	openaiReq := &provider.ChatCompletionRequest{
		Model:       c.mapModelName(req.Model, "gemini", "openai"),
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		TopP:        req.TopP,
		Stream:      req.Stream,
		Tools:       req.Tools,
	}

	// Convert messages
	openaiMessages := make([]provider.Message, 0)

	for _, msg := range req.Messages {
		switch msg.Role {
		case "user":
			openaiMessages = append(openaiMessages, provider.Message{
				Role:    "user",
				Content: msg.Content,
			})
		case "model":
			openaiMessages = append(openaiMessages, provider.Message{
				Role:    "assistant",
				Content: msg.Content,
			})
		}
	}

	openaiReq.Messages = openaiMessages
	return openaiReq, nil
}

// mapModelName maps model names between providers
func (c *FormatConverter) mapModelName(model, from, to string) string {
	// Model mapping table
	modelMap := map[string]map[string]string{
		"openai": {
			"claude": "claude-3-5-sonnet-20241022",
			"gemini": "gemini-pro",
		},
		"claude": {
			"openai": "gpt-4o",
			"gemini": "gemini-pro",
		},
		"gemini": {
			"openai": "gpt-4o",
			"claude": "claude-3-5-sonnet-20241022",
		},
	}

	if fromMap, ok := modelMap[from]; ok {
		if toModel, ok := fromMap[to]; ok {
			return toModel
		}
	}

	return model
}

// DetectFormat detects the API format from the request
func (c *FormatConverter) DetectFormat(req *provider.ChatCompletionRequest) string {
	// Check message roles to detect format
	for _, msg := range req.Messages {
		if msg.Role == "model" {
			return "gemini"
		}
	}

	// Default to OpenAI format
	return "openai"
}

// DetectProviderFromModel detects the provider from model name
func DetectProviderFromModel(model string) string {
	model = strings.ToLower(model)

	if strings.HasPrefix(model, "gpt-") || strings.HasPrefix(model, "o1-") {
		return "openai"
	}
	if strings.HasPrefix(model, "claude-") {
		return "claude"
	}
	if strings.HasPrefix(model, "gemini-") {
		return "gemini"
	}
	if strings.HasPrefix(model, "deepseek-") {
		return "deepseek"
	}

	return "openai"
}
