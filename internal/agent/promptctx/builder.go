package promptctx

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
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
	ProjectID       string // workspace/project id for project-scoped memory
	Cfg             config.AgentConfig
	Memory          *memory.MemoryService
	// ToolsSchemaCost is an optional precomputed token estimate for tools JSON.
	ToolsSchemaCost int
}

type storedMsg struct {
	ID      string
	Role    string
	Content string
}

// EstimateTokens roughly estimates tokens with CJK awareness.
func EstimateTokens(s string) int {
	if s == "" {
		return 0
	}
	latin, cjk, other := 0, 0, 0
	for _, r := range s {
		switch {
		case unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul):
			cjk++
		case r <= 0x7f:
			latin++
		default:
			other++
		}
	}
	// ~4 Latin chars/token, ~1.5–2 CJK chars/token
	return (latin+3)/4 + (cjk*2+2)/3 + (other+2)/3
}

// EstimateToolsSchema estimates tokens for the tools array sent to the model.
func EstimateToolsSchema(tools []provider.Tool) int {
	if len(tools) == 0 {
		return 0
	}
	b, err := json.Marshal(tools)
	if err != nil {
		n := 0
		for _, t := range tools {
			n += EstimateTokens(t.Function.Name) + EstimateTokens(t.Function.Description) + 40
		}
		return n
	}
	return EstimateTokens(string(b))
}

// Build assembles LLM messages in a prefix-cache-friendly layout:
//
//	stable prefix: fixed system → session checkpoint → append-only history
//	variable suffix: current user (workspace hint + FTS retrieval + request)
//
// When history exceeds turn/token budget, older live messages are folded into the
// checkpoint (advancing cutoff) instead of silently sliding the cached prefix.
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

	keep := maxTurns * 2
	fetchLimit := keep * 3
	if fetchLimit < 48 {
		fetchLimit = 48
	}
	history := loadHistory(db, opt.SessionID, opt.ExcludeMsgID, cutoffID, fetchLimit)

	reserve := EstimateTokens(opt.UserMessage) + EstimateTokens(opt.ExtraUserPrefix) + 400 + opt.ToolsSchemaCost
	needFold := false
	if keep > 0 && len(history) > keep {
		needFold = true
	}
	for !needFold && len(history) > 0 {
		cost := 0
		for _, m := range history {
			cost += EstimateTokens(m.Content) + 4
		}
		if used+cost+reserve <= budget {
			break
		}
		needFold = true
		break
	}

	if needFold && opt.Memory != nil && opt.SessionID != "" {
		keepN := keep
		if keepN <= 0 {
			keepN = 8
		}
		if len(history) > keepN {
			fold := history[:len(history)-keepN]
			history = history[len(history)-keepN:]
			FoldMessages(opt.Memory, opt.SessionID, fold)
			hasCP := false
			for _, m := range out {
				if m.Role == "system" && strings.Contains(m.Content, "Session checkpoint") {
					hasCP = true
					break
				}
			}
			if !hasCP {
				if cp, err := opt.Memory.GetSessionCheckpoint(opt.SessionID); err == nil && cp != nil {
					text := strings.TrimSpace(cp.Content)
					if text != "" {
						msg := "Session checkpoint (stable summary of earlier turns):\n" + text
						cost := EstimateTokens(msg)
						if used+cost < budget-800 {
							cpMsg := provider.Message{Role: "system", Content: msg}
							if len(out) >= 1 && out[0].Role == "system" {
								out = append([]provider.Message{out[0], cpMsg}, out[1:]...)
							} else {
								out = append([]provider.Message{cpMsg}, out...)
							}
							used += cost
						}
					}
				}
			}
		}
	}

	// Final soft trim only if still over budget after fold (should be rare).
	for len(history) > 0 {
		cost := 0
		for _, m := range history {
			cost += EstimateTokens(m.Content) + 4
		}
		if used+cost+reserve <= budget {
			break
		}
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

	userContent := buildUserSuffix(opt)
	out = append(out, provider.Message{Role: "user", Content: userContent})
	return out
}

func buildUserSuffix(opt Options) string {
	var parts []string
	if p := strings.TrimSpace(opt.ExtraUserPrefix); p != "" {
		parts = append(parts, p)
	}

	if opt.Cfg.MemoryConfig.Enabled && opt.Memory != nil {
		if opt.Cfg.MemoryConfig.ReconcileOnSearch {
			_, _ = opt.Memory.Reconcile()
		}
		limit := opt.Cfg.MemoryConfig.MaxSnippets
		if limit <= 0 {
			limit = 5
		}
		hits, _ := opt.Memory.SearchRelevant(opt.SessionID, opt.ProjectID, opt.UserMessage, limit, opt.Cfg.MemoryConfig.ScoreFloor)
		var lines []string
		for i, h := range hits {
			if h == nil || h.Type == "checkpoint" {
				continue
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

// FoldMessages merges extractive lines into the session checkpoint and advances cutoff.
func FoldMessages(mem *memory.MemoryService, sessionID string, old []storedMsg) {
	if mem == nil || sessionID == "" || len(old) == 0 {
		return
	}
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

func loadHistory(db *sql.DB, sessionID, excludeID, afterID string, limit int) []storedMsg {
	if db == nil || sessionID == "" {
		return nil
	}
	if limit <= 0 {
		limit = 64
	}

	// Fetch newest-first with LIMIT, then reverse for chronological order.
	// When afterID is set, only messages after that id (by created_at/rowid) are wanted.
	var rows *sql.Rows
	var err error
	if afterID != "" {
		rows, err = db.Query(`
			SELECT id, role, content FROM messages
			WHERE session_id = ?
			  AND rowid > COALESCE((SELECT rowid FROM messages WHERE id = ? LIMIT 1), 0)
			ORDER BY created_at DESC, rowid DESC
			LIMIT ?
		`, sessionID, afterID, limit)
		if err != nil {
			rows, err = db.Query(`
				SELECT id, role, content FROM messages
				WHERE session_id = ?
				ORDER BY created_at DESC, rowid DESC
				LIMIT ?
			`, sessionID, limit)
		}
	} else {
		rows, err = db.Query(`
			SELECT id, role, content FROM messages
			WHERE session_id = ?
			ORDER BY created_at DESC, rowid DESC
			LIMIT ?
		`, sessionID, limit)
	}
	if err != nil {
		return nil
	}
	defer rows.Close()

	tmp := make([]storedMsg, 0, limit)
	for rows.Next() {
		var m storedMsg
		if err := rows.Scan(&m.ID, &m.Role, &m.Content); err != nil {
			continue
		}
		if excludeID != "" && m.ID == excludeID {
			continue
		}
		tmp = append(tmp, m)
	}
	// Reverse to ascending
	out := make([]storedMsg, 0, len(tmp))
	for i := len(tmp) - 1; i >= 0; i-- {
		out = append(out, tmp[i])
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
		return
	}

	var cutoffID string
	if cp, err := mem.GetSessionCheckpoint(sessionID); err == nil && cp != nil {
		cutoffID = cp.AfterMsgID
	}
	history := loadHistory(db, sessionID, "", cutoffID, every*3)
	if len(history) < 4 {
		return
	}

	cut := len(history) - 4
	if cut < 2 {
		cut = len(history) / 2
	}
	FoldMessages(mem, sessionID, history[:cut])
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
