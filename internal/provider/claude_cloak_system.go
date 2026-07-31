package provider

import (
	"regexp"
	"strings"
	"sync"
)

const zwj = "\u200d"

// Default sensitive words (OmniRoute claudeCodeObfuscation + systemTransforms OpenWebUI/Hermes).
var defaultObfuscateWords = []string{
	"opencode", "open-code", "cline", "roo-cline", "roo_cline",
	"cursor", "windsurf", "aider", "continue.dev", "copilot",
	"avante", "codecompanion",
	"openwebui", "open-webui",
	"hermes-agent", "hermes",
	"codegateway",
}

var (
	obfuscationRegexMu    sync.Mutex
	obfuscationRegexCache = map[string]*regexp.Regexp{}
)

func getObfuscationRegex(word string) *regexp.Regexp {
	obfuscationRegexMu.Lock()
	defer obfuscationRegexMu.Unlock()
	if r, ok := obfuscationRegexCache[word]; ok {
		return r
	}
	if len(obfuscationRegexCache) > 2000 {
		obfuscationRegexCache = map[string]*regexp.Regexp{}
	}
	r := regexp.MustCompile("(?i)" + regexp.QuoteMeta(word))
	obfuscationRegexCache[word] = r
	return r
}

func obfuscateWord(word string) string {
	if len(word) <= 1 {
		return word
	}
	// Preserve original casing of first rune.
	r := []rune(word)
	return string(r[0]) + zwj + string(r[1:])
}

func obfuscateSensitiveWords(text string, words []string) string {
	if text == "" || len(words) == 0 {
		return text
	}
	result := text
	for _, word := range words {
		if word == "" {
			continue
		}
		re := getObfuscationRegex(word)
		result = re.ReplaceAllStringFunc(result, obfuscateWord)
	}
	return result
}

func obfuscateInBody(body map[string]interface{}, words []string) {
	if words == nil {
		words = defaultObfuscateWords
	}
	switch sys := body["system"].(type) {
	case string:
		body["system"] = obfuscateSensitiveWords(sys, words)
	case []interface{}:
		for _, raw := range sys {
			if block, ok := raw.(map[string]interface{}); ok {
				if t, ok := block["text"].(string); ok {
					block["text"] = obfuscateSensitiveWords(t, words)
				}
			}
		}
	}
	if msgs, ok := body["messages"].([]interface{}); ok {
		for _, raw := range msgs {
			msg, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			switch c := msg["content"].(type) {
			case string:
				msg["content"] = obfuscateSensitiveWords(c, words)
			case []interface{}:
				for _, braw := range c {
					if block, ok := braw.(map[string]interface{}); ok {
						if t, ok := block["text"].(string); ok {
							block["text"] = obfuscateSensitiveWords(t, words)
						}
					}
				}
			}
		}
	}
	for _, tool := range asToolMaps(body["tools"]) {
		if d, ok := tool["description"].(string); ok {
			tool["description"] = obfuscateSensitiveWords(d, words)
		}
		if fn, ok := tool["function"].(map[string]interface{}); ok {
			if d, ok := fn["description"].(string); ok {
				fn["description"] = obfuscateSensitiveWords(d, words)
			}
		}
	}
}

// System transform needles (OmniRoute DEFAULT_CLAUDE_PIPELINE).
var paragraphRemovalAnchors = []string{
	"github.com/anomalyco/opencode",
	"opencode.ai/docs",
	"github.com/cline/cline",
	"github.com/getcursor/cursor",
	"continue.dev",
	"github.com/open-webui/open-webui",
	"openwebui.com",
	"docs.openwebui.com",
	"@earendil-works/pi-coding-agent",
	"/.pi/",
	"Pi documentation (read only when the user asks about pi itself",
	"hermes-agent.nousresearch.com",
	"github.com/NousResearch/hermes-agent",
}

var identityPrefixes = []string{
	"You are OpenCode",
	"You are Open WebUI",
	"You are Hermes Agent",
}

var textReplacements = []struct{ match, replacement string }{
	{"if OpenCode honestly", "if the assistant honestly"},
	{
		"Here is some useful information about the environment you are running in:",
		"Environment context you are running in:",
	},
}

func splitParagraphs(text string) []string {
	return regexp.MustCompile(`\n\n+`).Split(text, -1)
}

func applySystemTransforms(body map[string]interface{}) {
	blocks := normalizeSystemBlocks(body["system"])
	blocks = dropParagraphIfContains(blocks, paragraphRemovalAnchors)
	blocks = dropParagraphIfStartsWith(blocks, identityPrefixes)
	for _, r := range textReplacements {
		blocks = replaceTextInBlocks(blocks, r.match, r.replacement, true)
	}
	// Filter empty text blocks.
	filtered := make([]interface{}, 0, len(blocks))
	for _, raw := range blocks {
		block, ok := raw.(map[string]interface{})
		if !ok {
			filtered = append(filtered, raw)
			continue
		}
		if t, ok := block["text"].(string); ok && t == "" {
			continue
		}
		filtered = append(filtered, block)
	}
	body["system"] = filtered
	obfuscateInBody(body, defaultObfuscateWords)
}

func normalizeSystemBlocks(system interface{}) []interface{} {
	switch s := system.(type) {
	case string:
		if s == "" {
			return nil
		}
		return []interface{}{map[string]interface{}{"type": "text", "text": s}}
	case []interface{}:
		return s
	default:
		return nil
	}
}

func dropParagraphIfContains(blocks []interface{}, needles []string) []interface{} {
	out := make([]interface{}, 0, len(blocks))
	for _, raw := range blocks {
		block, ok := raw.(map[string]interface{})
		if !ok {
			out = append(out, raw)
			continue
		}
		text, ok := block["text"].(string)
		if !ok || block["type"] != "text" {
			out = append(out, block)
			continue
		}
		paras := splitParagraphs(text)
		kept := make([]string, 0, len(paras))
		for _, p := range paras {
			drop := false
			for _, n := range needles {
				if strings.Contains(p, n) {
					drop = true
					break
				}
			}
			if !drop {
				kept = append(kept, p)
			}
		}
		cp := cloneMap(block)
		cp["text"] = strings.Join(kept, "\n\n")
		out = append(out, cp)
	}
	return out
}

func dropParagraphIfStartsWith(blocks []interface{}, prefixes []string) []interface{} {
	out := make([]interface{}, 0, len(blocks))
	for _, raw := range blocks {
		block, ok := raw.(map[string]interface{})
		if !ok {
			out = append(out, raw)
			continue
		}
		text, ok := block["text"].(string)
		if !ok || block["type"] != "text" {
			out = append(out, block)
			continue
		}
		paras := splitParagraphs(text)
		kept := make([]string, 0, len(paras))
		for _, p := range paras {
			trim := strings.TrimLeft(p, " \t")
			drop := false
			for _, prefix := range prefixes {
				if strings.HasPrefix(trim, prefix) {
					drop = true
					break
				}
			}
			if !drop {
				kept = append(kept, p)
			}
		}
		cp := cloneMap(block)
		cp["text"] = strings.Join(kept, "\n\n")
		out = append(out, cp)
	}
	return out
}

func replaceTextInBlocks(blocks []interface{}, match, replacement string, all bool) []interface{} {
	if match == "" {
		return blocks
	}
	out := make([]interface{}, 0, len(blocks))
	for _, raw := range blocks {
		block, ok := raw.(map[string]interface{})
		if !ok {
			out = append(out, raw)
			continue
		}
		text, ok := block["text"].(string)
		if !ok || !strings.Contains(text, match) {
			out = append(out, block)
			continue
		}
		cp := cloneMap(block)
		if all {
			cp["text"] = strings.ReplaceAll(text, match, replacement)
		} else {
			cp["text"] = strings.Replace(text, match, replacement, 1)
		}
		out = append(out, cp)
	}
	return out
}
