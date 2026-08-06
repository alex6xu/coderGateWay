package provider

import (
	"encoding/json"
	"testing"
)

func TestConvertClaudeResponseIncludesThinking(t *testing.T) {
	raw := []byte(`{
		"id":"msg_1",
		"type":"message",
		"role":"assistant",
		"model":"claude-sonnet-4",
		"stop_reason":"tool_use",
		"content":[
			{"type":"thinking","thinking":"I should read the file first."},
			{"type":"text","text":"Looking at main.go next."},
			{"type":"tool_use","id":"toolu_1","name":"read_file","input":{"path":"main.go"}}
		],
		"usage":{"input_tokens":10,"output_tokens":20}
	}`)
	var cr claudeResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		t.Fatal(err)
	}
	out := convertClaudeResponse(&cr)
	if len(out.Choices) != 1 {
		t.Fatalf("choices=%d", len(out.Choices))
	}
	msg := out.Choices[0].Message
	if msg.VisibleText() == "" {
		t.Fatal("expected visible text from thinking/text")
	}
	if !containsAll(msg.Content, "I should read the file first.", "Looking at main.go next.") {
		t.Fatalf("content=%q", msg.Content)
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].Function.Name != "read_file" {
		t.Fatalf("tool calls=%#v", msg.ToolCalls)
	}
}

func TestMessageVisibleText(t *testing.T) {
	m := Message{Content: "answer", ReasoningContent: "thought"}
	if got := m.VisibleText(); got != "thought\n\nanswer" {
		t.Fatalf("got %q", got)
	}
	m.MergeReasoningIntoContent()
	if m.Content != "thought\n\nanswer" {
		t.Fatalf("merged content=%q", m.Content)
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !containsStr(s, p) {
			return false
		}
	}
	return true
}

func containsStr(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})())
}
