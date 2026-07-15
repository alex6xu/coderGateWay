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

// Build assembles LLM messages under a soft token budget:
// system + memory snippets + session summary + recent history + current user.
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

	system := strings.TrimSpace(opt.System)
	if system != "" {
		out = append(out, provider.Message{Role: "system", Content: system})
		used += EstimateTokens(system)
	}

	// Memory retrieval (session + global)
	if opt.Cfg.MemoryConfig.Enabled && opt.Memory != nil && opt.SessionID != "" {
		limit := opt.Cfg.MemoryConfig.MaxSnippets
		if limit <= 0 {
			limit = 5
		}
		hits, _ := opt.Memory.SearchSessionRelevant(opt.SessionID, opt.UserMessage, limit)
		if len(hits) > 0 {
			var b strings.Builder
			b.WriteString("Relevant memory (retrieved, may be incomplete):\n")
			for i, h := range hits {
				snippet := strings.TrimSpace(h.Content)
				if snippet == "" {
					continue
				}
				if EstimateTokens(snippet) > 400 {
					snippet = truncateRunes(snippet, 800)
				}
				b.WriteString(fmt.Sprintf("%d. (%s) %s\n", i+1, h.Type, snippet))
			}
			memMsg := strings.TrimSpace(b.String())
			cost := EstimateTokens(memMsg)
			if memMsg != "" && used+cost < budget-500 {
				out = append(out, provider.Message{Role: "system", Content: memMsg})
				used += cost
			}
		}
	}

	// Sliding window of prior turns
	history := loadHistory(db, opt.SessionID, opt.ExcludeMsgID)
	if len(history) > 0 {
		// Keep last maxTurns*2 messages (user+assistant pairs roughly)
		keep := maxTurns * 2
		if keep > len(history) {
			keep = len(history)
		}
		window := history[len(history)-keep:]

		// Drop oldest until under budget (reserve room for current user)
		reserve := EstimateTokens(opt.UserMessage) + EstimateTokens(opt.ExtraUserPrefix) + 200
		for len(window) > 0 {
			cost := 0
			for _, m := range window {
				cost += EstimateTokens(m.Content) + 4
			}
			if used+cost+reserve <= budget {
				break
			}
			window = window[1:]
		}

		for _, m := range window {
			role := m.Role
			if role != "user" && role != "assistant" && role != "system" {
				continue
			}
			content := m.Content
			if EstimateTokens(content) > 1200 {
				content = truncateRunes(content, 2400) + "\n…[truncated for context budget]"
			}
			out = append(out, provider.Message{Role: role, Content: content})
			used += EstimateTokens(content) + 4
		}
	}

	userContent := opt.UserMessage
	if strings.TrimSpace(opt.ExtraUserPrefix) != "" {
		userContent = strings.TrimSpace(opt.ExtraUserPrefix) + "\n\nUser request:\n" + opt.UserMessage
	}
	out = append(out, provider.Message{Role: "user", Content: userContent})
	return out
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

// MaybeCheckpoint writes an extractive session summary into FTS memory.
// Avoids an extra LLM call to keep token cost low.
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
	// Summarize older half extractively
	cut := len(history) - 4
	if cut < 2 {
		cut = len(history) / 2
	}
	old := history[:cut]
	var b strings.Builder
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
	_ = mem.UpsertSessionMemory(sessionID, b.String())
}

// CompactToolMessages truncates older tool results in-place to save tokens.
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
		// Still cap oversized recent results
		for _, i := range toolIdx {
			if len(messages[i].Content) > maxChars*2 {
				messages[i].Content = messages[i].Content[:maxChars] + "\n…[truncated tool result]"
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
			messages[i].Content = content[:maxChars] + "\n…[truncated tool result]"
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
