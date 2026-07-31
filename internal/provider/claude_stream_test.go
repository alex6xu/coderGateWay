package provider

import (
	"context"
	"strings"
	"testing"
)

func TestNormalizeStopSequences(t *testing.T) {
	if got := normalizeStopSequences("END"); len(got) != 1 || got[0] != "END" {
		t.Fatalf("string stop: %#v", got)
	}
	if got := normalizeStopSequences([]interface{}{"a", "b"}); len(got) != 2 {
		t.Fatalf("array stop: %#v", got)
	}
}

func TestConvertToolChoiceToClaude(t *testing.T) {
	auto := convertToolChoiceToClaude("auto").(map[string]string)
	if auto["type"] != "auto" {
		t.Fatalf("%#v", auto)
	}
	fn := convertToolChoiceToClaude(map[string]interface{}{
		"type":     "function",
		"function": map[string]interface{}{"name": "Bash"},
	}).(map[string]string)
	if fn["type"] != "tool" || fn["name"] != "Bash" {
		t.Fatalf("%#v", fn)
	}
}

func TestClaudeSSEToOpenAIChunks(t *testing.T) {
	p := NewClaudeProvider(&ProviderConfig{Name: "c", Type: ProviderTypeClaude})
	payload := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_1","model":"claude-sonnet-4","usage":{"input_tokens":10,"output_tokens":0}}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")

	ch := make(chan *ChatCompletionChunk, 8)
	go func() {
		defer close(ch)
		p.readClaudeSSE(context.Background(), strings.NewReader(payload), "fallback", ch)
	}()

	var texts []string
	var finish string
	for c := range ch {
		if c.Choices[0].Delta.Content != "" {
			texts = append(texts, c.Choices[0].Delta.Content)
		}
		if c.Choices[0].FinishReason != nil {
			finish = *c.Choices[0].FinishReason
		}
	}
	if strings.Join(texts, "") != "hi" {
		t.Fatalf("texts=%v", texts)
	}
	if finish != "stop" {
		t.Fatalf("finish=%q", finish)
	}
}

func TestBuildBodyPassesTopPAndStop(t *testing.T) {
	p := NewClaudeProvider(&ProviderConfig{Name: "c", Type: ProviderTypeClaude, AuthMode: "api_key"})
	topP := 0.9
	body, err := p.buildBody(&ChatCompletionRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []Message{
			{Role: "user", Content: "hi"},
		},
		TopP: &topP,
		Stop: []string{"END"},
		ToolChoice: "auto",
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	if !strings.Contains(s, `"top_p":0.9`) {
		t.Fatalf("missing top_p: %s", s)
	}
	if !strings.Contains(s, `"stop_sequences":["END"]`) {
		t.Fatalf("missing stop_sequences: %s", s)
	}
	if !strings.Contains(s, `"tool_choice":{"type":"auto"}`) {
		t.Fatalf("missing tool_choice: %s", s)
	}
}
