// Package agentlink adapts CodeGateway's provider/tool types to the pigo-derived
// agentcore / agentruntime loop.
package agentlink

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/alex/codegateway/internal/agent/agentcore"
	"github.com/alex/codegateway/internal/provider"
)

// MessagesFromProvider converts OpenAI-style messages into agentcore messages.
// Leading system messages are skipped (system lives on AgentContext.SystemPrompt).
func MessagesFromProvider(msgs []provider.Message) agentcore.MessageList {
	out := make(agentcore.MessageList, 0, len(msgs))
	now := time.Now().UnixMilli()
	for _, m := range msgs {
		switch strings.ToLower(strings.TrimSpace(m.Role)) {
		case "system":
			continue
		case "user":
			out = append(out, agentcore.UserMessage{
				RoleField: agentcore.RoleUser,
				Content:   agentcore.ContentList{agentcore.NewTextContent(m.Content)},
				Timestamp: now,
			})
		case "assistant":
			out = append(out, assistantFromProvider(m, now))
		case "tool":
			out = append(out, agentcore.ToolResultMessage{
				RoleField:  agentcore.RoleToolResult,
				ToolCallID: m.ToolCallID,
				ToolName:   m.Name,
				Content:    agentcore.ContentList{agentcore.NewTextContent(m.Content)},
				Timestamp:  now,
			})
		}
	}
	return out
}

func assistantFromProvider(m provider.Message, ts int64) agentcore.AssistantMessage {
	var content agentcore.ContentList
	if rc := strings.TrimSpace(m.ReasoningContent); rc != "" {
		content = append(content, agentcore.ThinkingContent{Type: agentcore.ContentTypeThinking, Thinking: rc})
	}
	if text := strings.TrimSpace(m.Content); text != "" {
		content = append(content, agentcore.NewTextContent(text))
	}
	for _, tc := range m.ToolCalls {
		args := json.RawMessage(tc.Function.Arguments)
		if len(args) == 0 && tc.Function.Parameters != nil {
			b, _ := json.Marshal(tc.Function.Parameters)
			args = b
		}
		if len(args) == 0 {
			args = json.RawMessage(`{}`)
		}
		content = append(content, agentcore.NewToolCallContent(tc.ID, tc.Function.Name, args))
	}
	stop := agentcore.StopReasonEndTurn
	if len(m.ToolCalls) > 0 {
		stop = agentcore.StopReasonToolUse
	}
	return agentcore.AssistantMessage{
		RoleField:  agentcore.RoleAssistant,
		Content:    content,
		StopReason: stop,
		Timestamp:  ts,
	}
}

// ExtractSystemPrompt returns the concatenated leading system message contents.
func ExtractSystemPrompt(msgs []provider.Message) string {
	var parts []string
	for _, m := range msgs {
		if strings.ToLower(strings.TrimSpace(m.Role)) != "system" {
			break
		}
		if s := strings.TrimSpace(m.Content); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n\n")
}

// ConvertToLlm maps agentcore messages for provider requests: compaction
// checkpoints become user text; other roles pass through.
func ConvertToLlm(msgs agentcore.MessageList) agentcore.MessageList {
	out := make(agentcore.MessageList, 0, len(msgs))
	for _, m := range msgs {
		switch v := m.(type) {
		case agentcore.CompactionMessage:
			out = append(out, v.AsUserMessage())
		default:
			out = append(out, m)
		}
	}
	return out
}

// ToProviderMessages converts agentcore LLM-bound messages into provider messages.
func ToProviderMessages(msgs agentcore.MessageList) []provider.Message {
	out := make([]provider.Message, 0, len(msgs))
	for _, m := range msgs {
		switch v := m.(type) {
		case agentcore.UserMessage:
			out = append(out, provider.Message{Role: "user", Content: agentcore.ContentToText(v.Content)})
		case agentcore.AssistantMessage:
			out = append(out, toProviderAssistant(v))
		case agentcore.ToolResultMessage:
			out = append(out, provider.Message{
				Role:       "tool",
				Content:    agentcore.ContentToText(v.Content),
				ToolCallID: v.ToolCallID,
				Name:       v.ToolName,
			})
		case agentcore.CompactionMessage:
			u := v.AsUserMessage()
			out = append(out, provider.Message{Role: "user", Content: agentcore.ContentToText(u.Content)})
		}
	}
	return out
}

func toProviderAssistant(m agentcore.AssistantMessage) provider.Message {
	msg := provider.Message{Role: "assistant"}
	var text strings.Builder
	var thinking strings.Builder
	for _, c := range m.Content {
		switch b := c.(type) {
		case agentcore.TextContent:
			text.WriteString(b.Text)
		case agentcore.ThinkingContent:
			thinking.WriteString(b.Thinking)
		case agentcore.ToolCallContent:
			msg.ToolCalls = append(msg.ToolCalls, provider.ToolCall{
				ID:   b.ID,
				Type: "function",
				Function: provider.ToolFunction{
					Name:      b.Name,
					Arguments: string(b.Arguments),
				},
			})
		}
	}
	msg.Content = text.String()
	msg.ReasoningContent = thinking.String()
	return msg
}

// ToolsFromAgent converts agentcore tools to provider tool definitions.
func ToolsFromAgent(tools []agentcore.AgentTool) []provider.Tool {
	out := make([]provider.Tool, 0, len(tools))
	for _, t := range tools {
		var params interface{}
		if raw := t.Schema(); len(raw) > 0 {
			_ = json.Unmarshal(raw, &params)
		}
		if params == nil {
			params = map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
		}
		out = append(out, provider.Tool{
			Type: "function",
			Function: provider.ToolFunction{
				Name:        t.Name(),
				Description: t.Description(),
				Parameters:  params,
			},
		})
	}
	return out
}

// FinishReasonToStop maps OpenAI finish_reason to agentcore stop reasons.
func FinishReasonToStop(reason string, hasTools bool) string {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "tool_calls", "tool_use":
		return agentcore.StopReasonToolUse
	case "length":
		return agentcore.StopReasonLength
	case "error":
		return agentcore.StopReasonError
	case "content_filter", "cancelled", "canceled":
		return agentcore.StopReasonAborted
	default:
		if hasTools {
			return agentcore.StopReasonToolUse
		}
		return agentcore.StopReasonEndTurn
	}
}
