package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ClaudeProvider implements Anthropic Messages API with optional prompt caching.
type ClaudeProvider struct {
	config *ProviderConfig
	client *http.Client
}

// NewClaudeProvider creates a new Claude provider
func NewClaudeProvider(config *ProviderConfig) *ClaudeProvider {
	if config.BaseURL == "" {
		config.BaseURL = "https://api.anthropic.com"
	}
	return &ClaudeProvider{config: config, client: &http.Client{}}
}

func (p *ClaudeProvider) Name() string { return p.config.Name }

type claudeRequest struct {
	Model       string          `json:"model"`
	MaxTokens   int             `json:"max_tokens"`
	System      []claudeContent `json:"system,omitempty"`
	Messages    []claudeMessage `json:"messages"`
	Temperature *float64        `json:"temperature,omitempty"`
	Tools       []claudeTool    `json:"tools,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
}

type claudeContent struct {
	Type         string        `json:"type"`
	Text         string        `json:"text,omitempty"`
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

type claudeMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

type claudeTool struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	InputSchema interface{} `json:"input_schema"`
}

type claudeResponse struct {
	ID           string `json:"id"`
	Type         string `json:"type"`
	Role         string `json:"role"`
	Model        string `json:"model"`
	StopReason   string `json:"stop_reason"`
	Content      []struct {
		Type  string `json:"type"`
		Text  string `json:"text,omitempty"`
		ID    string `json:"id,omitempty"`
		Name  string `json:"name,omitempty"`
		Input json.RawMessage `json:"input,omitempty"`
	} `json:"content"`
	Usage struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	} `json:"usage"`
}

func (p *ClaudeProvider) ChatCompletion(ctx context.Context, req *ChatCompletionRequest) (*ChatCompletionResponse, error) {
	body, err := p.buildBody(req, false)
	if err != nil {
		return nil, err
	}
	httpReq, err := p.newHTTPRequest(ctx, body)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(raw))
	}
	var cr claudeResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return convertClaudeResponse(&cr), nil
}

func (p *ClaudeProvider) ChatCompletionStream(ctx context.Context, req *ChatCompletionRequest) (<-chan *ChatCompletionChunk, error) {
	// Anthropic streaming is event-based; fall back to non-stream then emit one chunk.
	resp, err := p.ChatCompletion(ctx, req)
	if err != nil {
		return nil, err
	}
	ch := make(chan *ChatCompletionChunk, 2)
	go func() {
		defer close(ch)
		content := ""
		if len(resp.Choices) > 0 {
			content = resp.Choices[0].Message.Content
		}
		finish := "stop"
		ch <- &ChatCompletionChunk{
			ID:    resp.ID,
			Model: resp.Model,
			Choices: []ChunkChoice{{
				Delta:        MessageDelta{Role: "assistant", Content: content},
				FinishReason: &finish,
			}},
			Usage: &resp.Usage,
		}
	}()
	return ch, nil
}

func (p *ClaudeProvider) buildBody(req *ChatCompletionRequest, stream bool) ([]byte, error) {
	maxTokens := 4096
	if req.MaxTokens != nil && *req.MaxTokens > 0 {
		maxTokens = *req.MaxTokens
	}
	cr := claudeRequest{
		Model:       req.Model,
		MaxTokens:   maxTokens,
		Temperature: req.Temperature,
		Stream:      stream,
	}

	var msgs []claudeMessage
	for _, m := range req.Messages {
		switch m.Role {
		case "system":
			block := claudeContent{Type: "text", Text: m.Content}
			if req.EnablePromptCache && m.CacheControl != nil {
				block.CacheControl = m.CacheControl
			}
			cr.System = append(cr.System, block)
		case "assistant":
			if len(m.ToolCalls) > 0 {
				parts := make([]map[string]interface{}, 0)
				if strings.TrimSpace(m.Content) != "" {
					parts = append(parts, map[string]interface{}{"type": "text", "text": m.Content})
				}
				for _, tc := range m.ToolCalls {
					var input interface{}
					_ = json.Unmarshal([]byte(tc.Function.Arguments), &input)
					if input == nil {
						input = map[string]interface{}{}
					}
					parts = append(parts, map[string]interface{}{
						"type":  "tool_use",
						"id":    tc.ID,
						"name":  tc.Function.Name,
						"input": input,
					})
				}
				msgs = append(msgs, claudeMessage{Role: "assistant", Content: parts})
			} else {
				msgs = append(msgs, claudeMessage{Role: "assistant", Content: m.Content})
			}
		case "tool":
			msgs = append(msgs, claudeMessage{
				Role: "user",
				Content: []map[string]interface{}{{
					"type":        "tool_result",
					"tool_use_id": m.ToolCallID,
					"content":     m.Content,
				}},
			})
		default: // user
			msgs = append(msgs, claudeMessage{Role: "user", Content: m.Content})
		}
	}
	cr.Messages = msgs

	for _, t := range req.Tools {
		cr.Tools = append(cr.Tools, claudeTool{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			InputSchema: t.Function.Parameters,
		})
	}
	return json.Marshal(cr)
}

func (p *ClaudeProvider) newHTTPRequest(ctx context.Context, body []byte) (*http.Request, error) {
	base := strings.TrimRight(p.config.BaseURL, "/")
	url := base + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("anthropic-beta", "prompt-caching-2024-07-31")
	if p.config.APIKey != "" {
		httpReq.Header.Set("x-api-key", p.config.APIKey)
	}
	return httpReq, nil
}

func convertClaudeResponse(cr *claudeResponse) *ChatCompletionResponse {
	msg := Message{Role: "assistant"}
	var textParts []string
	for _, c := range cr.Content {
		switch c.Type {
		case "text":
			textParts = append(textParts, c.Text)
		case "tool_use":
			args := string(c.Input)
			if args == "" {
				args = "{}"
			}
			msg.ToolCalls = append(msg.ToolCalls, ToolCall{
				ID:   c.ID,
				Type: "function",
				Function: ToolFunction{
					Name:      c.Name,
					Arguments: args,
				},
			})
		}
	}
	msg.Content = strings.Join(textParts, "\n")
	finish := cr.StopReason
	if finish == "tool_use" {
		finish = "tool_calls"
	} else if finish == "end_turn" {
		finish = "stop"
	}
	usage := Usage{
		PromptTokens:     cr.Usage.InputTokens,
		CompletionTokens: cr.Usage.OutputTokens,
		TotalTokens:      cr.Usage.InputTokens + cr.Usage.OutputTokens,
		CachedTokens:     cr.Usage.CacheReadInputTokens,
	}
	return &ChatCompletionResponse{
		ID:      cr.ID,
		Object:  "chat.completion",
		Model:   cr.Model,
		Choices: []Choice{{Index: 0, Message: msg, FinishReason: finish}},
		Usage:   usage,
	}
}

func (p *ClaudeProvider) ListModels(ctx context.Context) ([]string, error) {
	return []string{
		"claude-3-opus-20240229",
		"claude-3-sonnet-20240229",
		"claude-3-haiku-20240307",
		"claude-3-5-sonnet-20241022",
		"claude-sonnet-4-20250514",
	}, nil
}

func (p *ClaudeProvider) ValidateModel(model string) bool {
	models, _ := p.ListModels(context.Background())
	for _, m := range models {
		if m == model {
			return true
		}
	}
	return strings.HasPrefix(model, "claude-")
}
