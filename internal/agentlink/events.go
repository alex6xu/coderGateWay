package agentlink

import (
	"strings"

	"github.com/alex/codegateway/internal/agentcore"
)

// UIEvent is the slim event surface CodeGateway's Session Run / SSE layer expects.
type UIEvent struct {
	Type    string // delta|tool_step|error|done
	Content string
	Step    map[string]string
}

// EventBridge converts agentcore events into UI events with incremental text deltas.
type EventBridge struct {
	lastText string
	sawText  bool
}

// Handle maps one agentcore event into zero or more UI events.
func (b *EventBridge) Handle(ev agentcore.AgentEvent) []UIEvent {
	switch e := ev.(type) {
	case agentcore.MessageUpdateEvent:
		a, ok := e.Message.(agentcore.AssistantMessage)
		if !ok {
			return nil
		}
		text := agentcore.ContentToText(a.Content)
		if thinking := thinkingText(a); thinking != "" && text == "" {
			text = thinking
		}
		delta := incremental(b.lastText, text)
		b.lastText = text
		if delta == "" {
			return nil
		}
		b.sawText = true
		return []UIEvent{{Type: "delta", Content: delta}}

	case agentcore.MessageEndEvent:
		a, ok := e.Message.(agentcore.AssistantMessage)
		if !ok {
			return nil
		}
		if a.StopReason == agentcore.StopReasonError || a.StopReason == agentcore.StopReasonAborted {
			msg := a.ErrorMessage
			if msg == "" {
				msg = "agent stopped: " + a.StopReason
			}
			return []UIEvent{{Type: "error", Content: msg}}
		}
		text := agentcore.ContentToText(a.Content)
		delta := incremental(b.lastText, text)
		b.lastText = text
		// Reset per-assistant-message so the next turn starts clean.
		defer func() { b.lastText = ""; b.sawText = false }()
		if delta == "" {
			return nil
		}
		if !b.sawText && len(a.ToolCalls()) == 0 {
			return []UIEvent{{Type: "delta", Content: strings.TrimSpace(delta) + "\n\n"}}
		}
		if delta != "" {
			return []UIEvent{{Type: "delta", Content: delta}}
		}
		return nil

	case agentcore.ToolExecutionEndEvent:
		step := map[string]string{"tool": e.ToolName}
		if details, ok := e.Result.Details.(map[string]string); ok {
			if v := details["args"]; v != "" {
				step["args"] = v
			}
			if v := details["result"]; v != "" {
				step["result"] = v
			}
		}
		if step["result"] == "" {
			step["result"] = agentcore.ContentToText(e.Result.Content)
		}
		if step["args"] == "" {
			step["args"] = "{}"
		}
		return []UIEvent{{Type: "tool_step", Step: step}}

	case agentcore.AgentEndEvent:
		return []UIEvent{{Type: "done"}}
	}
	return nil
}

func thinkingText(a agentcore.AssistantMessage) string {
	var b strings.Builder
	for _, c := range a.Content {
		if t, ok := c.(agentcore.ThinkingContent); ok {
			b.WriteString(t.Thinking)
		}
	}
	return b.String()
}

func incremental(prev, next string) string {
	if next == "" {
		return ""
	}
	if prev == "" {
		return next
	}
	if strings.HasPrefix(next, prev) {
		return next[len(prev):]
	}
	// Provider rewrote the buffer; emit the remainder best-effort.
	return next
}
