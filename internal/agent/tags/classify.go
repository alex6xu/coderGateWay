package tags

import (
	"regexp"
	"strings"
	"unicode"
)

// TagHit is a classified label for a user question.
type TagHit struct {
	Slug       string  `json:"slug"`
	Name       string  `json:"name"`
	Kind       string  `json:"kind"` // category | topic
	Confidence float64 `json:"confidence"`
}

type categoryRule struct {
	Slug     string
	Name     string
	Keywords []string
}

var categories = []categoryRule{
	{Slug: "coding", Name: "编程开发", Keywords: []string{"实现", "开发", "写代码", "编写", "新增功能", "implement", "code", "feature", "写一个", "帮我写"}},
	{Slug: "debug", Name: "调试排错", Keywords: []string{"报错", "bug", "错误", "失败", "异常", "崩溃", "debug", "fix", "排查", "不工作", "没法", "can't", "error"}},
	{Slug: "refactor", Name: "重构优化", Keywords: []string{"重构", "优化", "性能", "可读性", "refactor", "optimize", "cleanup", "整理代码"}},
	{Slug: "review", Name: "代码审查", Keywords: []string{"审查", "review", "code review", "看看这段", "有没有问题", "安全风险"}},
	{Slug: "test", Name: "测试", Keywords: []string{"测试", "单测", "单元测试", "test", "coverage", "用例"}},
	{Slug: "auth", Name: "认证鉴权", Keywords: []string{"登录", "注册", "密码", "鉴权", "oauth", "jwt", "token", "auth", "权限", "账号"}},
	{Slug: "database", Name: "数据库", Keywords: []string{"数据库", "sql", "sqlite", "postgres", "mysql", "迁移", "migration", "表结构", "query"}},
	{Slug: "frontend", Name: "前端", Keywords: []string{"前端", "页面", "ui", "react", "css", "组件", "tsx", "vite", "界面"}},
	{Slug: "backend", Name: "后端", Keywords: []string{"后端", "api", "接口", "handler", "服务端", "golang", "gin", "rpc"}},
	{Slug: "devops", Name: "部署运维", Keywords: []string{"部署", "docker", "k8s", "ci", "cd", "nginx", "运维", "服务器", "deploy"}},
	{Slug: "ai", Name: "AI / 模型", Keywords: []string{"模型", "llm", "prompt", "agent", "gpt", "claude", "whisper", "asr", "ai", "向量", "embedding"}},
	{Slug: "github", Name: "GitHub / 仓库", Keywords: []string{"github", "仓库", "repo", "git", "pull request", "pr", "clone", "导入仓库"}},
	{Slug: "workspace", Name: "工作区 / 项目", Keywords: []string{"工作区", "workspace", "上传目录", "项目文件", "云端目录"}},
	{Slug: "docs", Name: "文档说明", Keywords: []string{"解释", "说明", "文档", "是什么", "怎么用", "教程", "explain", "what is", "如何理解"}},
}

var topicKeywords = []struct {
	Slug string
	Name string
	Keys []string
}{
	{"go", "Go", []string{"golang", " go ", ".go"}},
	{"typescript", "TypeScript", []string{"typescript", "tsx", "ts "}},
	{"python", "Python", []string{"python", "django", "fastapi", ".py"}},
	{"react", "React", []string{"react", "hooks", "jsx"}},
	{"docker", "Docker", []string{"docker", "dockerfile", "compose"}},
	{"redis", "Redis", []string{"redis", "缓存"}},
	{"websocket", "WebSocket", []string{"websocket", "ws "}},
	{"speech", "语音", []string{"语音", "麦克风", "asr", "speech", "whisper"}},
	{"cache", "缓存", []string{"缓存", "cache", "prefix cache", "前缀缓存"}},
	{"security", "安全", []string{"安全", "xss", "csrf", "注入", "security"}},
}

var (
	spaceRe = regexp.MustCompile(`\s+`)
	wordRe  = regexp.MustCompile(`[\p{L}\p{N}_+#.-]{2,}`)
)

// Classify returns category + topic tags for a user question.
// Purely local rules — no LLM spend.
func Classify(question string) []TagHit {
	q := strings.TrimSpace(question)
	if q == "" {
		return nil
	}
	lower := strings.ToLower(q)
	padded := " " + lower + " "

	hits := make([]TagHit, 0, 6)
	seen := map[string]bool{}

	bestScore := 0
	var best *categoryRule
	for i := range categories {
		rule := &categories[i]
		score := 0
		for _, kw := range rule.Keywords {
			kwl := strings.ToLower(kw)
			if strings.Contains(lower, kwl) || strings.Contains(padded, " "+kwl+" ") {
				score += len([]rune(kwl))
			}
		}
		if score > bestScore {
			bestScore = score
			best = rule
		}
	}
	if best != nil && bestScore > 0 {
		conf := float64(bestScore) / 12.0
		if conf > 1 {
			conf = 1
		}
		if conf < 0.35 {
			conf = 0.35
		}
		hits = append(hits, TagHit{Slug: best.Slug, Name: best.Name, Kind: "category", Confidence: conf})
		seen[best.Slug] = true
	} else {
		hits = append(hits, TagHit{Slug: "general", Name: "通用问答", Kind: "category", Confidence: 0.3})
		seen["general"] = true
	}

	// Secondary category if clearly present
	for i := range categories {
		rule := &categories[i]
		if seen[rule.Slug] {
			continue
		}
		score := 0
		for _, kw := range rule.Keywords {
			if strings.Contains(lower, strings.ToLower(kw)) {
				score++
			}
		}
		if score >= 2 {
			hits = append(hits, TagHit{Slug: rule.Slug, Name: rule.Name, Kind: "category", Confidence: 0.45})
			seen[rule.Slug] = true
			if len(hits) >= 3 {
				break
			}
		}
	}

	for _, t := range topicKeywords {
		if seen[t.Slug] {
			continue
		}
		for _, k := range t.Keys {
			if strings.Contains(padded, strings.ToLower(k)) || strings.Contains(lower, strings.ToLower(strings.TrimSpace(k))) {
				hits = append(hits, TagHit{Slug: t.Slug, Name: t.Name, Kind: "topic", Confidence: 0.55})
				seen[t.Slug] = true
				break
			}
		}
	}

	// Light keyword tags from longer latin/CJK tokens (capped)
	for _, tok := range extractTokens(q) {
		slug := slugify(tok)
		if slug == "" || seen[slug] || len(slug) < 2 {
			continue
		}
		if isStopword(slug) {
			continue
		}
		// Prefer tech-looking tokens
		if !looksTechnical(tok) && utf8Len(tok) < 3 {
			continue
		}
		hits = append(hits, TagHit{Slug: slug, Name: tok, Kind: "topic", Confidence: 0.25})
		seen[slug] = true
		if len(hits) >= 6 {
			break
		}
	}

	return hits
}

func extractTokens(s string) []string {
	parts := wordRe.FindAllString(s, 24)
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, p := range parts {
		p = strings.Trim(p, ".-_#")
		if p == "" {
			continue
		}
		key := strings.ToLower(p)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, p)
	}
	return out
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = spaceRe.ReplaceAllString(s, "-")
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || unicode.In(r, unicode.Han) {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len([]rune(out)) > 24 {
		out = string([]rune(out)[:24])
	}
	return out
}

func utf8Len(s string) int { return len([]rune(s)) }

func looksTechnical(s string) bool {
	lower := strings.ToLower(s)
	if strings.ContainsAny(lower, "._/-") {
		return true
	}
	hasLetter, hasDigit := false, false
	for _, r := range lower {
		if unicode.IsLetter(r) {
			hasLetter = true
		}
		if unicode.IsDigit(r) {
			hasDigit = true
		}
	}
	return hasLetter && hasDigit
}

func isStopword(slug string) bool {
	switch slug {
	case "the", "and", "for", "with", "this", "that", "from", "have", "will",
		"请", "帮我", "如何", "怎么", "什么", "一个", "一下", "可以", "需要", "进行",
		"我想", "我们", "你们", "他们", "这个", "那个", "然后", "以及":
		return true
	default:
		return false
	}
}

// Preview truncates a question for list display.
func Preview(content string, maxRunes int) string {
	content = strings.TrimSpace(spaceRe.ReplaceAllString(content, " "))
	if maxRunes <= 0 {
		maxRunes = 120
	}
	r := []rune(content)
	if len(r) <= maxRunes {
		return content
	}
	return string(r[:maxRunes]) + "…"
}
