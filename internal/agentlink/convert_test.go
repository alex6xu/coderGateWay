package agentlink

import (
	"testing"

	"github.com/alex/codegateway/internal/agentcore"
	"github.com/alex/codegateway/internal/provider"
)

func TestMessagesFromProviderSkipsSystem(t *testing.T) {
	msgs := MessagesFromProvider([]provider.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "yo", ToolCalls: []provider.ToolCall{
			{ID: "1", Type: "function", Function: provider.ToolFunction{Name: "read_file", Arguments: `{"path":"a"}`}},
		}},
		{Role: "tool", Content: "ok", ToolCallID: "1", Name: "read_file"},
	})
	if len(msgs) != 3 {
		t.Fatalf("got %d messages", len(msgs))
	}
	if ExtractSystemPrompt([]provider.Message{{Role: "system", Content: "sys"}, {Role: "user", Content: "hi"}}) != "sys" {
		t.Fatal("system prompt")
	}
	a := msgs[1].(agentcore.AssistantMessage)
	if len(a.ToolCalls()) != 1 || a.StopReason != agentcore.StopReasonToolUse {
		t.Fatalf("assistant tool calls: %+v", a)
	}
}

func TestEventBridgeIncremental(t *testing.T) {
	var b EventBridge
	evs := b.Handle(agentcore.MessageUpdateEvent{
		Message: agentcore.AssistantMessage{
			RoleField: agentcore.RoleAssistant,
			Content:   agentcore.ContentList{agentcore.NewTextContent("hel")},
		},
	})
	if len(evs) != 1 || evs[0].Content != "hel" {
		t.Fatalf("first: %+v", evs)
	}
	evs = b.Handle(agentcore.MessageUpdateEvent{
		Message: agentcore.AssistantMessage{
			RoleField: agentcore.RoleAssistant,
			Content:   agentcore.ContentList{agentcore.NewTextContent("hello")},
		},
	})
	if len(evs) != 1 || evs[0].Content != "lo" {
		t.Fatalf("second: %+v", evs)
	}
}
