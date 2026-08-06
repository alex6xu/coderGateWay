package provider

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ChatCompletionStream streams Anthropic SSE and converts to OpenAI-compatible chunks.
func (p *ClaudeProvider) ChatCompletionStream(ctx context.Context, req *ChatCompletionRequest) (<-chan *ChatCompletionChunk, error) {
	body, err := p.buildBody(req, true)
	if err != nil {
		return nil, err
	}
	httpReq, err := p.newHTTPRequest(ctx, body, req)
	if err != nil {
		return nil, err
	}
	// OAuth cloak keeps Accept: application/json (OmniRoute / Stainless); API-key
	// streaming matches OpenAI providers with text/event-stream.
	if !p.oauthMode() {
		httpReq.Header.Set("Accept", "text/event-stream")
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, NewProviderError(resp.StatusCode, resp.Header, raw)
	}

	ch := make(chan *ChatCompletionChunk, 64)
	go func() {
		defer resp.Body.Close()
		defer close(ch)
		p.readClaudeSSE(ctx, resp.Body, req.Model, ch)
	}()
	return ch, nil
}

type claudeSSEEvent struct {
	Type    string `json:"type"`
	Index   int    `json:"index"`
	Message *struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage *struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
	ContentBlock *struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
		Text string `json:"text"`
	} `json:"content_block"`
	Delta *struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		Thinking    string `json:"thinking"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	Usage *struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	} `json:"usage"`
}

func (p *ClaudeProvider) readClaudeSSE(ctx context.Context, r io.Reader, fallbackModel string, out chan<- *ChatCompletionChunk) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var (
		msgID    string
		model    = fallbackModel
		usage    Usage
		toolIdx  = map[int]int{}
		nextTC   int
		sentRole bool
	)

	emit := func(chunk *ChatCompletionChunk) bool {
		select {
		case out <- chunk:
			return true
		case <-ctx.Done():
			return false
		}
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "event:") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}

		var ev claudeSSEEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue
		}

		switch ev.Type {
		case "message_start":
			if ev.Message != nil {
				msgID = ev.Message.ID
				if ev.Message.Model != "" {
					model = ev.Message.Model
				}
				if ev.Message.Usage != nil {
					usage.PromptTokens = ev.Message.Usage.InputTokens
					usage.CachedTokens = ev.Message.Usage.CacheReadInputTokens
				}
			}
			if !sentRole {
				sentRole = true
				if !emit(&ChatCompletionChunk{
					ID:     msgID,
					Object: "chat.completion.chunk",
					Model:  model,
					Choices: []ChunkChoice{{
						Index: 0,
						Delta: MessageDelta{Role: "assistant"},
					}},
				}) {
					return
				}
			}

		case "content_block_start":
			if ev.ContentBlock == nil || ev.ContentBlock.Type != "tool_use" {
				continue
			}
			idx := nextTC
			toolIdx[ev.Index] = idx
			nextTC++
			i := idx
			tc := ToolCall{
				Index: &i,
				ID:    ev.ContentBlock.ID,
				Type:  "function",
				Function: ToolFunction{
					Name:      ev.ContentBlock.Name,
					Arguments: "",
				},
			}
			if !emit(&ChatCompletionChunk{
				ID:     msgID,
				Object: "chat.completion.chunk",
				Model:  model,
				Choices: []ChunkChoice{{
					Index: 0,
					Delta: MessageDelta{ToolCalls: []ToolCall{tc}},
				}},
			}) {
				return
			}

		case "content_block_delta":
			if ev.Delta == nil {
				continue
			}
			switch ev.Delta.Type {
			case "text_delta":
				if ev.Delta.Text == "" {
					continue
				}
				if !emit(&ChatCompletionChunk{
					ID:     msgID,
					Object: "chat.completion.chunk",
					Model:  model,
					Choices: []ChunkChoice{{
						Index: 0,
						Delta: MessageDelta{Content: ev.Delta.Text},
					}},
				}) {
					return
				}
			case "thinking_delta":
				piece := ev.Delta.Thinking
				if piece == "" {
					piece = ev.Delta.Text
				}
				if piece == "" {
					continue
				}
				if !emit(&ChatCompletionChunk{
					ID:     msgID,
					Object: "chat.completion.chunk",
					Model:  model,
					Choices: []ChunkChoice{{
						Index: 0,
						Delta: MessageDelta{Content: piece, ReasoningContent: piece},
					}},
				}) {
					return
				}
			case "input_json_delta":
				idx, ok := toolIdx[ev.Index]
				if !ok {
					continue
				}
				i := idx
				tc := ToolCall{
					Index: &i,
					Type:  "function",
					Function: ToolFunction{
						Arguments: ev.Delta.PartialJSON,
					},
				}
				if !emit(&ChatCompletionChunk{
					ID:     msgID,
					Object: "chat.completion.chunk",
					Model:  model,
					Choices: []ChunkChoice{{
						Index: 0,
						Delta: MessageDelta{ToolCalls: []ToolCall{tc}},
					}},
				}) {
					return
				}
			}

		case "message_delta":
			if ev.Usage != nil {
				if ev.Usage.OutputTokens > 0 {
					usage.CompletionTokens = ev.Usage.OutputTokens
				}
				if ev.Usage.InputTokens > 0 {
					usage.PromptTokens = ev.Usage.InputTokens
				}
				if ev.Usage.CacheReadInputTokens > 0 {
					usage.CachedTokens = ev.Usage.CacheReadInputTokens
				}
			}
			finish := "stop"
			if ev.Delta != nil && ev.Delta.StopReason != "" {
				finish = mapClaudeStopReason(ev.Delta.StopReason)
			}
			usage.Normalize()
			u := usage
			if !emit(&ChatCompletionChunk{
				ID:     msgID,
				Object: "chat.completion.chunk",
				Model:  model,
				Choices: []ChunkChoice{{
					Index:        0,
					Delta:        MessageDelta{},
					FinishReason: &finish,
				}},
				Usage: &u,
			}) {
				return
			}

		case "message_stop", "error":
			return
		}
	}
}

func mapClaudeStopReason(reason string) string {
	switch reason {
	case "tool_use":
		return "tool_calls"
	case "end_turn", "stop_sequence":
		return "stop"
	case "max_tokens":
		return "length"
	default:
		if reason == "" {
			return "stop"
		}
		return reason
	}
}

// ListModels tries Anthropic GET /v1/models (like OpenAI providers), then falls back.
func (p *ClaudeProvider) ListModels(ctx context.Context) ([]string, error) {
	base := strings.TrimRight(p.config.BaseURL, "/")
	url := base + "/v1/models"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return defaultClaudeModels(), nil
	}
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	if p.oauthMode() {
		httpReq.Header.Set("Authorization", "Bearer "+p.config.APIKey)
		httpReq.Header.Set("anthropic-beta", "oauth-2025-04-20")
		httpReq.Header.Set("User-Agent", claudeCodeUserAgent)
	} else if p.config.APIKey != "" {
		httpReq.Header.Set("x-api-key", p.config.APIKey)
	}
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return defaultClaudeModels(), nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return defaultClaudeModels(), nil
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil || len(payload.Data) == 0 {
		return defaultClaudeModels(), nil
	}
	out := make([]string, 0, len(payload.Data))
	for _, m := range payload.Data {
		if m.ID != "" {
			out = append(out, m.ID)
		}
	}
	return out, nil
}

func defaultClaudeModels() []string {
	return []string{
		"claude-opus-4-6",
		"claude-opus-4-5-20251101",
		"claude-sonnet-4-6",
		"claude-sonnet-4-5-20250929",
		"claude-sonnet-4-20250514",
		"claude-haiku-4-5-20251001",
		"claude-3-5-sonnet-20241022",
	}
}

func (p *ClaudeProvider) ValidateModel(model string) bool {
	if strings.HasPrefix(model, "claude-") {
		return true
	}
	for _, m := range defaultClaudeModels() {
		if m == model {
			return true
		}
	}
	return false
}
