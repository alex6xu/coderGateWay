package agentlink

import (
	"strings"
	"testing"

	"github.com/alex/codegateway/internal/agent/agentcore"
)

func TestCompactToolResultsKeepRecent(t *testing.T) {
	msgs := agentcore.MessageList{
		agentcore.UserMessage{RoleField: agentcore.RoleUser, Content: agentcore.ContentList{agentcore.NewTextContent("u")}},
		toolResult("t1", strings.Repeat("a", 500)),
		toolResult("t2", strings.Repeat("b", 500)),
		toolResult("t3", strings.Repeat("c", 500)),
	}
	CompactToolResults(msgs, 1, 100)
	t1 := agentcore.ContentToText(msgs[1].(agentcore.ToolResultMessage).Content)
	t3 := agentcore.ContentToText(msgs[3].(agentcore.ToolResultMessage).Content)
	if !strings.Contains(t1, "older tool result compacted") {
		t.Fatalf("older tool should be compacted, got %q", t1)
	}
	if len(t3) > 100 && !strings.Contains(t3, "truncated") {
		t.Fatalf("recent tool should be truncated, got len=%d %q", len(t3), t3[:min(80, len(t3))])
	}
	if len(t3) > 160 {
		t.Fatalf("recent tool still too long: %d", len(t3))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestCompactionSettingsFromBudget(t *testing.T) {
	window, s := CompactionSettingsFromBudget(8000, 0.75, 0.55)
	if window != 8000 || !s.Enabled {
		t.Fatalf("window/settings: %d %+v", window, s)
	}
	if s.ReserveTokens != 2000 {
		t.Fatalf("reserve=%d want 2000", s.ReserveTokens)
	}
	if s.KeepRecentTokens != 4400 {
		t.Fatalf("keep=%d want 4400", s.KeepRecentTokens)
	}
}

func toolResult(id, body string) agentcore.ToolResultMessage {
	return agentcore.ToolResultMessage{
		RoleField:  agentcore.RoleToolResult,
		ToolCallID: id,
		ToolName:   "read_file",
		Content:    agentcore.ContentList{agentcore.NewTextContent(body)},
	}
}
