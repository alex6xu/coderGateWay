package agentruntime

import "github.com/alex/codegateway/internal/agentcore"

// eventEnvelope maps an AgentEvent onto a JSON-serializable object with a
// "type" discriminant plus the event's observable payload.
func eventEnvelope(ev agentcore.AgentEvent) map[string]any {
	env := map[string]any{"type": ev.EventType()}
	switch e := ev.(type) {
	case agentcore.AgentStartEvent:
		if e.SessionID != "" {
			env["sessionId"] = e.SessionID
		}
	case agentcore.AgentEndEvent:
		env["messageCount"] = len(e.Messages)
	case agentcore.TurnEndEvent:
		env["stopReason"] = e.Message.StopReason
		if text := agentcore.ContentToText(e.Message.Content); text != "" {
			env["text"] = text
		}
		if calls := e.Message.ToolCalls(); len(calls) > 0 {
			names := make([]string, len(calls))
			for i, c := range calls {
				names[i] = c.Name
			}
			env["toolCalls"] = names
		}
	case agentcore.MessageUpdateEvent:
		if a, ok := e.Message.(agentcore.AssistantMessage); ok {
			if text := agentcore.ContentToText(a.Content); text != "" {
				env["text"] = text
			}
		}
	case agentcore.ToolExecutionStartEvent:
		env["toolCallId"] = e.ToolCallID
		env["toolName"] = e.ToolName
	case agentcore.ToolExecutionEndEvent:
		env["toolCallId"] = e.ToolCallID
		env["toolName"] = e.ToolName
		env["isError"] = e.IsError
	case agentcore.CompactionEvent:
		env["reason"] = e.Reason
		env["tokensBefore"] = e.TokensBefore
		env["tokensAfter"] = e.TokensAfter
		env["summarizedCount"] = e.SummarizedCount
		env["keptCount"] = e.KeptCount
		if e.ErrorMessage != "" {
			env["error"] = e.ErrorMessage
		}
	case agentcore.TelemetryEvent:
		env["turns"] = e.Turns
		env["truncationCount"] = e.TruncationCount
		env["compactionCount"] = e.CompactionCount
		env["contextUtilization"] = e.ContextUtilization
		env["contextTokens"] = e.ContextTokens
		env["contextWindow"] = e.ContextWindow
		tools := make(map[string]map[string]any, len(e.ToolDurationsMs))
		for name, t := range e.ToolDurationsMs {
			tools[name] = map[string]any{"count": t.Count, "totalMs": t.TotalMs}
		}
		env["toolDurationsMs"] = tools
	}
	return env
}