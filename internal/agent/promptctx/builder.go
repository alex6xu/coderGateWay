package promptctx

import (
	"database/sql"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/alex/codegateway/internal/agent/memory"
	"github.com/alex/codegateway/internal/config"
	"github.com/alex/codegateway/internal/provider"
)

// Options controls context assembly.
type Options struct {
	System          string
	UserMessage     string
	SessionID       string
	ExcludeMsgID    string
	ExtraUserPrefix string // e.g. workspace tree hint
	Cfg             config.AgentConfig
	Memory          *memory.MemoryService
}

type storedMsg struct {
	ID      string
	Role    string
	Content string
}

// EstimateTokens roughly estimates tokens from text.
func EstimateTokens(s string) int {
	n := utf8.RuneCountInString(s)
	if n == 0 {
		return 0
	}
	// Blend Latin (~4 chars/token) and CJK (~1.5–2 chars/token)
	return (n + 2) / 3
}

// Build assembles LLM messages in a prefix-cache-friendly layout:
//
//	stable prefix: fixed system → session checkpoint → append-only history
//	variable suffix: current user (workspace hint + FTS retrieval + request)
//
// FTS hits are never inserted between system and history (that would bust the
// entire cached prefix every turn). History after a checkpoint cutoff only grows
// by appending until the next infrequent fold (MaybeCheckpoint).
func Build(db *sql.DB, opt Options) []provider.Message {
	budget := opt.Cfg.ContextBudgetTokens
	if budget <= 0 {
		budget = 8000
	}
	maxTurns := opt.Cfg.HistoryMaxTurns
	if maxTurns <= 0 {
		maxTurns = 8
	}

	used := 0
	out := make([]provider.Message, 0, maxTurns+4)

	// --- Stable prefix -------------------------------------------------------
	system := strings.TrimSpace(opt.System)
	if system != "" {
		out = append(out, provider.Message{Role: "system", Content: system})
		used += EstimateTokens(system)
	}

	var cutoffID string
	if opt.Cfg.MemoryConfig.Enabled && opt.Memory != nil && opt.SessionID != "" {
		if cp, err := opt.Memory.GetSessionCheckpoint(opt.SessionID); err == nil && cp != nil {
			cutoffID = cp.AfterMsgID
			text := strings.TrimSpace(cp.Content)
			if text != "" {
				msg := "Session checkpoint (stable summary of earlier turns):\n" + text
				cost := EstimateTokens(msg)
				if used+cost < budget-800 {
					out = append(out, provider.Message{Role: "system", Content: msg})
					used += cost
				}
			}
		}
	}

	history := loadHistory(db, opt.SessionID, opt.ExcludeMsgID)
	if cutoffID != "" {
		history = historyAfter(history, cutoffID)
	}

	// Soft cap: keep a contiguous suffix. Prefer not rewriting message bodies so
	// previously cached history tokens stay byte-identical across turns.
	keep := maxTurns * 2
	if keep > 0 && len(history) > keep {
		history = history[len(history)-keep:]
	}

	reserve := EstimateTokens(opt.UserMessage) + EstimateTokens(opt.ExtraUserPrefix) + 400
	for len(history) > 0 {
		cost := 0
		for _, m := range history {
			cost += EstimateTokens(m.Content) + 4
		}
		if used+cost+reserve <= budget {
			break
		}
		// Drop oldest whole messages only (never rewrite remaining bodies).
		history = history[1:]
	}

	for _, m := range history {
		role := m.Role
		if role != "user" && role != "assistant" && role != "system" {
			continue
		}
		out = append(out, provider.Message{Role: role, Content: m.Content})
		used += EstimateTokens(m.Content) + 4
	}

	// --- Variable suffix (current user turn) ---------------------------------
	userContent := buildUserSuffix(opt)
	out = append(out, provider.Message{Role: "user", Content: userContent})
	return out
}

func buildUserSuffix(opt Options) string {
	var parts []string
	if p := strings.TrimSpace(opt.ExtraUserPrefix); p != "" {
		parts = append(parts, p)
	}

	// Query-dependent retrieval belongs here so it does not bust the stable prefix.
	if opt.Cfg.MemoryConfig.Enabled && opt.Memory != nil && opt.SessionID != "" {
		limit := opt.Cfg.MemoryConfig.MaxSnippets
		if limit <= 0 {
			limit = 5
		}
		hits, _ := opt.Memory.SearchSessionRelevant(opt.SessionID, opt.UserMessage, limit)
		var lines []string
		for i, h := range hits {
			if h == nil || h.Type == "checkpoint" {
				continue // checkpoint already in stable prefix
			}
			snippet := strings.TrimSpace(h.Content)
			if snippet == "" {
				continue
			}
			if EstimateTokens(snippet) > 400 {
				snippet = truncateRunes(snippet, 800)
			}
			lines = append(lines, fmt.Sprintf("%d. (%s) %s", i+1, h.Type, snippet))
		}
		if len(lines) > 0 {
			parts = append(parts, "Relevant memory (retrieved for this turn):\n"+strings.Join(lines, "\n"))
		}
	}

	parts = append(parts, "User request:\n"+opt.UserMessage)
	return strings.Join(parts, "\n\n")
}

func historyAfter(history []storedMsg, afterID string) []storedMsg {
	if afterID == "" || len(history) == 0 {
		return history
	}
	for i, m := range history {
		if m.ID == afterID {
			if i+1 >= len(history) {
				return nil
			}
			return history[i+1:]
		}
	}
	return history
}

func loadHistory(db *sql.DB, sessionID, excludeID string) []storedMsg {
	if db == nil || sessionID == "" {
		return nil
	}
	rows, err := db.Query(`
		SELECT id, role, content FROM messages
		WHERE session_id = ?
		ORDER BY created_at ASC, rowid ASC
	`, sessionID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	out := make([]storedMsg, 0)
	for rows.Next() {
		var m storedMsg
		if err := rows.Scan(&m.ID, &m.Role, &m.Content); err != nil {
			continue
		}
		if excludeID != "" && m.ID == excludeID {
			continue
		}
		out = append(out, m)
	}
	return out
}

// MaybeCheckpoint writes an extractive session summary and advances the history
// cutoff so subsequent Builds keep an append-only suffix (prefix-cache friendly).
func MaybeCheckpoint(db *sql.DB, mem *memory.MemoryService, sessionID string, every int) {
	if mem == nil || sessionID == "" || db == nil {
		return
	}
	if every <= 0 {
		every = 10
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM messages WHERE session_id = ?`, sessionID).Scan(&count); err != nil {
		return
	}
	if count < every || count%every > 1 {
		// Trigger near multiples of `every` (after a completed user+assistant pair)
		return
	}

	history := loadHistory(db, sessionID, "")
	if len(history) < 4 {
		return
	}

	// Respect existing cutoff: only fold messages still in the live suffix.
	var start int
	if cp, err := mem.GetSessionCheckpoint(sessionID); err == nil && cp != nil && cp.AfterMsgID != "" {
		for i, m := range history {
			if m.ID == cp.AfterMsgID {
				start = i + 1
				break
			}
		}
	}
	live := history[start:]
	if len(live) < 4 {
		return
	}

	// Fold older half of the live suffix; keep a small recent tail append-only.
	cut := len(live) - 4
	if cut < 2 {
		cut = len(live) / 2
	}
	old := live[:cut]
	afterID := old[len(old)-1].ID

	var b strings.Builder
	if cp, err := mem.GetSessionCheckpoint(sessionID); err == nil && cp != nil {
		prev := strings.TrimSpace(cp.Content)
		if prev != "" {
			b.WriteString(prev)
			b.WriteString("\n")
		}
	}
	b.WriteString("Session checkpoint (extractive):\n")
	for _, m := range old {
		line := strings.ReplaceAll(strings.TrimSpace(m.Content), "\n", " ")
		if line == "" {
			continue
		}
		if utf8.RuneCountInString(line) > 160 {
			line = truncateRunes(line, 160) + "…"
		}
		b.WriteString("- [")
		b.WriteString(m.Role)
		b.WriteString("] ")
		b.WriteString(line)
		b.WriteString("\n")
		if EstimateTokens(b.String()) > 800 {
			break
		}
	}
	_ = mem.UpsertSessionCheckpoint(sessionID, b.String(), afterID)
}

// TruncateToolResult caps a single new tool result without rewriting prior messages.
func TruncateToolResult(content string, maxChars int) string {
	if maxChars <= 0 {
		maxChars = 4000
	}
	if len(content) <= maxChars {
		return content
	}
	return content[:maxChars] + "\n…[truncated tool result]"
}

// CompactToolMessages truncates older tool results in-place.
// Prefer TruncateToolResult for new results; avoid calling this mid agent-loop
// (rewriting earlier tool messages busts prompt prefix cache).
func CompactToolMessages(messages []provider.Message, keepRecent, maxChars int) {
	if keepRecent <= 0 {
		keepRecent = 2
	}
	if maxChars <= 0 {
		maxChars = 4000
	}

	toolIdx := make([]int, 0)
	for i, m := range messages {
		if m.Role == "tool" {
			toolIdx = append(toolIdx, i)
		}
	}
	if len(toolIdx) <= keepRecent {
		for _, i := range toolIdx {
			if len(messages[i].Content) > maxChars*2 {
				messages[i].Content = TruncateToolResult(messages[i].Content, maxChars)
			}
		}
		return
	}

	cutoff := len(toolIdx) - keepRecent
	for n, i := range toolIdx {
		content := messages[i].Content
		if n < cutoff {
			if len(content) > 240 {
				messages[i].Content = content[:240] + "\n…[older tool result compacted]"
			}
			continue
		}
		if len(content) > maxChars {
			messages[i].Content = TruncateToolResult(content, maxChars)
		}
	}
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}
