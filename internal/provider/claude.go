package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
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

func (p *ClaudeProvider) oauthMode() bool {
	return strings.EqualFold(p.config.AuthMode, "oauth")
}

type claudeRequest struct {
	Model         string                 `json:"model"`
	MaxTokens     int                    `json:"max_tokens"`
	System        []claudeContent        `json:"system,omitempty"`
	Messages      []claudeMessage        `json:"messages"`
	Temperature   *float64               `json:"temperature,omitempty"`
	TopP          *float64               `json:"top_p,omitempty"`
	StopSequences []string               `json:"stop_sequences,omitempty"`
	ToolChoice    interface{}            `json:"tool_choice,omitempty"`
	Tools         []claudeTool           `json:"tools,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
	Stream        bool                   `json:"stream,omitempty"`
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
	ID         string `json:"id"`
	Type       string `json:"type"`
	Role       string `json:"role"`
	Model      string `json:"model"`
	StopReason string `json:"stop_reason"`
	Content    []struct {
		Type  string          `json:"type"`
		Text  string          `json:"text,omitempty"`
		ID    string          `json:"id,omitempty"`
		Name  string          `json:"name,omitempty"`
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
	httpReq, err := p.newHTTPRequest(ctx, body, req)
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

func (p *ClaudeProvider) buildBody(req *ChatCompletionRequest, stream bool) ([]byte, error) {
	maxTokens := 4096
	if req.MaxTokens != nil && *req.MaxTokens > 0 {
		maxTokens = *req.MaxTokens
	}
	cr := claudeRequest{
		Model:         req.Model,
		MaxTokens:     maxTokens,
		Temperature:   req.Temperature,
		TopP:          req.TopP,
		StopSequences: normalizeStopSequences(req.Stop),
		ToolChoice:    convertToolChoiceToClaude(req.ToolChoice),
		Stream:        stream,
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

	if p.oauthMode() {
		return p.buildOAuthBody(&cr)
	}
	return json.Marshal(cr)
}

// buildOAuthBody runs the OmniRoute native Claude OAuth cloak pipeline, then
// serializes with CLI body field order.
func (p *ClaudeProvider) buildOAuthBody(cr *claudeRequest) ([]byte, error) {
	raw, err := json.Marshal(cr)
	if err != nil {
		return nil, err
	}
	var body map[string]interface{}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}

	// OmniRoute executors/base.ts native claude OAuth path (order matters).
	stripProxyToolPrefix(body)
	remapClaudeToolNames(body)
	cloakThirdPartyToolNames(body)
	sanitizeClaudeToolSchemas(body)
	obfuscateInBody(body, defaultObfuscateWords)
	stripToolCacheControl(body)
	stripVersionedToolModelPrefix(body)

	// Billing + sentinel (before system transforms so cosmetic ops run on caller text).
	sys := normalizeSystemBlocks(body["system"])
	sys = stripIdentitySystemBlocks(sys)
	prefix := []interface{}{
		map[string]interface{}{"type": "text", "text": claudeBillingLine()},
		map[string]interface{}{"type": "text", "text": claudeCodeSentinel},
	}
	body["system"] = append(prefix, sys...)

	applySystemTransforms(body)

	deviceID := ensureDeviceID(p.config.ClaudeDeviceID)
	accountUUID := p.config.ClaudeAccountUUID
	if accountUUID == "" {
		accountUUID = uuid.NewString()
	}
	sessionID := oauthSessionID(deviceID)
	body["metadata"] = map[string]interface{}{
		"user_id": buildClaudeUserIDJSON(deviceID, accountUUID, sessionID),
	}

	fixClaudeToolMessagePairs(body)
	return marshalClaudeFingerprintBody(body)
}

func stripIdentitySystemBlocks(blocks []interface{}) []interface{} {
	out := make([]interface{}, 0, len(blocks))
	for _, raw := range blocks {
		block, ok := raw.(map[string]interface{})
		if !ok {
			out = append(out, raw)
			continue
		}
		t, _ := block["text"].(string)
		if strings.HasPrefix(t, claudeCodeBillingPrefix) || strings.HasPrefix(t, claudeCodeSentinel) {
			continue
		}
		out = append(out, block)
	}
	return out
}

func (p *ClaudeProvider) newHTTPRequest(ctx context.Context, body []byte, req *ChatCompletionRequest) (*http.Request, error) {
	base := strings.TrimRight(p.config.BaseURL, "/")
	url := base + "/v1/messages"
	if p.oauthMode() {
		url += "?beta=true"
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	if p.oauthMode() {
		p.setOAuthHeaders(httpReq, req, body)
	} else {
		httpReq.Header.Set("anthropic-beta", "prompt-caching-2024-07-31")
		if p.config.APIKey != "" {
			httpReq.Header.Set("x-api-key", p.config.APIKey)
		}
	}
	return httpReq, nil
}

func (p *ClaudeProvider) setOAuthHeaders(httpReq *http.Request, req *ChatCompletionRequest, body []byte) {
	hasTools := len(req.Tools) > 0
	hasSystem := true // cloak always prepends billing+sentinel
	// Prefer shape from final body when available.
	var parsed map[string]interface{}
	if json.Unmarshal(body, &parsed) == nil {
		if tools, ok := parsed["tools"].([]interface{}); ok {
			hasTools = len(tools) > 0
		}
	}
	betas := selectClaudeOAuthBetas(req.Model, hasSystem, hasTools, false)
	deviceID := ensureDeviceID(p.config.ClaudeDeviceID)
	sessionID := oauthSessionID(deviceID)

	// Apply in OmniRoute CLI header order (Go's net/http may re-sort on the wire;
	// values and casing still match the fingerprint).
	ordered := [][2]string{
		{"Accept", "application/json"},
		{"Authorization", "Bearer " + p.config.APIKey},
		{"Content-Type", "application/json"},
		{"User-Agent", claudeCodeUserAgent},
		{"X-Claude-Code-Session-Id", sessionID},
		{"X-Stainless-Arch", stainlessArch()},
		{"X-Stainless-Lang", "js"},
		{"X-Stainless-OS", stainlessOS()},
		{"X-Stainless-Package-Version", claudeCodeStainlessPkgVersion},
		{"X-Stainless-Retry-Count", "0"},
		{"X-Stainless-Runtime", "node"},
		{"X-Stainless-Runtime-Version", claudeCodeStainlessRuntimeVer},
		{"X-Stainless-Timeout", "600"},
		{"anthropic-beta", betas},
		{"anthropic-dangerous-direct-browser-access", "true"},
		{"anthropic-version", "2023-06-01"},
		{"x-app", "cli"},
		{"x-client-request-id", uuid.NewString()},
	}
	_ = claudeHeaderOrder
	for _, kv := range ordered {
		httpReq.Header.Set(kv[0], kv[1])
	}
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
	finish := mapClaudeStopReason(cr.StopReason)
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

func normalizeStopSequences(stop interface{}) []string {
	if stop == nil {
		return nil
	}
	switch v := stop.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return []string{v}
	case []string:
		out := make([]string, 0, len(v))
		for _, s := range v {
			if strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// convertToolChoiceToClaude maps OpenAI tool_choice to Anthropic Messages format.
func convertToolChoiceToClaude(choice interface{}) interface{} {
	if choice == nil {
		return nil
	}
	switch v := choice.(type) {
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "auto":
			return map[string]string{"type": "auto"}
		case "none":
			return map[string]string{"type": "none"}
		case "required", "any":
			return map[string]string{"type": "any"}
		default:
			return nil
		}
	case map[string]interface{}:
		typ, _ := v["type"].(string)
		switch typ {
		case "function":
			name := ""
			if fn, ok := v["function"].(map[string]interface{}); ok {
				name, _ = fn["name"].(string)
			}
			if name == "" {
				name, _ = v["name"].(string)
			}
			if name == "" {
				return nil
			}
			return map[string]string{"type": "tool", "name": name}
		case "tool":
			return v
		case "auto", "none", "any":
			return map[string]string{"type": typ}
		default:
			return v
		}
	default:
		return nil
	}
}
