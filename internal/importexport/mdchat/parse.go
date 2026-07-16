package mdchat

import (
	"regexp"
	"strings"
	"unicode"
)

// Message is one turn parsed from markdown.
type Message struct {
	Role    string `json:"role"` // user | assistant | system
	Content string `json:"content"`
}

// ParseResult is the outcome of parsing a markdown transcript.
type ParseResult struct {
	Title    string    `json:"title"`
	Messages []Message `json:"messages"`
}

var (
	headingRoleRe = regexp.MustCompile(`(?i)^#{1,6}\s*(user|human|assistant|ai|system|model|bot|你|用户|助手|系统|人工)\s*[:：]?\s*$`)
	boldRoleRe    = regexp.MustCompile(`(?i)^\*{0,2}_?(user|human|assistant|ai|system|model|bot|你|用户|助手|系统|人工)_?\*{0,2}\s*[:：]\s*(.*)$`)
	plainRoleRe   = regexp.MustCompile(`(?i)^(user|human|assistant|ai|system|model|bot|你|用户|助手|系统|人工)\s*[:：]\s*(.*)$`)
	hrRe          = regexp.MustCompile(`(?m)^(?:---+|___+|\*\*\*+)\s*$`)
	titleRe       = regexp.MustCompile(`(?m)^#\s+(.+)$`)
)

// Parse extracts chat turns from markdown conversation exports.
func Parse(md string) ParseResult {
	md = strings.ReplaceAll(md, "\r\n", "\n")
	md = strings.TrimSpace(md)
	res := ParseResult{}

	if m := titleRe.FindStringSubmatch(md); len(m) > 1 {
		cand := strings.TrimSpace(m[1])
		if !isRoleLabel(cand) {
			res.Title = cand
		}
	}

	lines := strings.Split(md, "\n")
	var curRole string
	var buf strings.Builder
	flush := func() {
		content := strings.TrimSpace(buf.String())
		buf.Reset()
		if curRole == "" || content == "" {
			return
		}
		res.Messages = append(res.Messages, Message{Role: curRole, Content: content})
	}

	for _, line := range lines {
		trim := strings.TrimSpace(line)

		if hrRe.MatchString(trim) {
			continue
		}

		// Heading role: ## User
		if m := headingRoleRe.FindStringSubmatch(trim); len(m) > 1 {
			flush()
			curRole = normalizeRole(m[1])
			continue
		}

		// Bold / plain role with optional same-line content
		if m := boldRoleRe.FindStringSubmatch(trim); len(m) > 1 {
			flush()
			curRole = normalizeRole(m[1])
			rest := strings.TrimSpace(m[2])
			if rest != "" {
				buf.WriteString(rest)
				buf.WriteByte('\n')
			}
			continue
		}
		if m := plainRoleRe.FindStringSubmatch(trim); len(m) > 1 {
			flush()
			curRole = normalizeRole(m[1])
			rest := strings.TrimSpace(m[2])
			if rest != "" {
				buf.WriteString(rest)
				buf.WriteByte('\n')
			}
			continue
		}

		// Skip lone H1 title line once captured
		if strings.HasPrefix(trim, "# ") && res.Title != "" && strings.TrimSpace(strings.TrimPrefix(trim, "# ")) == res.Title {
			continue
		}

		if curRole == "" {
			// Content before first role — treat as system note if substantial
			continue
		}
		buf.WriteString(line)
		buf.WriteByte('\n')
	}
	flush()

	// Fallback: Q/A style blocks separated by blank lines with leading markers
	if len(res.Messages) == 0 {
		res.Messages = parseAlternatingBlocks(md)
	}

	if res.Title == "" && len(res.Messages) > 0 {
		res.Title = deriveTitle(res.Messages)
	}
	return res
}

func parseAlternatingBlocks(md string) []Message {
	// Patterns like:
	// > User: ...
	// or paragraphs starting with Q:/A:
	qaRe := regexp.MustCompile(`(?im)^(?:>\s*)?(q|a|问|答)\s*[:：]\s*(.+)$`)
	var out []Message
	var curRole string
	var buf strings.Builder
	flush := func() {
		c := strings.TrimSpace(buf.String())
		buf.Reset()
		if curRole == "" || c == "" {
			return
		}
		out = append(out, Message{Role: curRole, Content: c})
	}
	for _, line := range strings.Split(md, "\n") {
		trim := strings.TrimSpace(line)
		if m := qaRe.FindStringSubmatch(trim); len(m) > 1 {
			flush()
			label := strings.ToLower(m[1])
			if label == "q" || label == "问" {
				curRole = "user"
			} else {
				curRole = "assistant"
			}
			buf.WriteString(strings.TrimSpace(m[2]))
			buf.WriteByte('\n')
			continue
		}
		if curRole != "" {
			buf.WriteString(line)
			buf.WriteByte('\n')
		}
	}
	flush()
	return out
}

func normalizeRole(label string) string {
	l := strings.ToLower(strings.TrimSpace(label))
	switch l {
	case "user", "human", "你", "用户", "人工":
		return "user"
	case "assistant", "ai", "model", "bot", "助手":
		return "assistant"
	case "system", "系统":
		return "system"
	default:
		return "user"
	}
}

func isRoleLabel(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "user", "human", "assistant", "ai", "system", "model", "bot",
		"你", "用户", "助手", "系统", "人工":
		return true
	default:
		return false
	}
}

func deriveTitle(msgs []Message) string {
	for _, m := range msgs {
		if m.Role != "user" {
			continue
		}
		line := strings.TrimSpace(m.Content)
		line = strings.Split(line, "\n")[0]
		r := []rune(line)
		if len(r) > 48 {
			return string(r[:48]) + "…"
		}
		if line != "" {
			return line
		}
	}
	return "Imported chat"
}

// Validate checks a parse result is importable.
func Validate(r ParseResult) error {
	if len(r.Messages) == 0 {
		return errNoMessages
	}
	hasUA := false
	for _, m := range r.Messages {
		if m.Role == "user" || m.Role == "assistant" {
			hasUA = true
			break
		}
	}
	if !hasUA {
		return errNoMessages
	}
	return nil
}

type parseError string

func (e parseError) Error() string { return string(e) }

const errNoMessages parseError = "未能从 Markdown 中解析出对话（请使用 User/Assistant 或 用户/助手 标题分段）"

// CountRunes is a small helper for tests / previews.
func CountRunes(s string) int {
	n := 0
	for range s {
		n++
	}
	_ = unicode.ReplacementChar
	return n
}
