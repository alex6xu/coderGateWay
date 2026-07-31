package provider

import (
	"os"
	"regexp"
	"strings"
)

// Claude Code tool rename map (OmniRoute claudeCodeToolRemapper + ExtraRemap).
var claudeToolRenameMap = map[string]string{
	"bash": "Bash", "read": "Read", "write": "Write", "edit": "Edit",
	"glob": "Glob", "grep": "Grep", "task": "Task",
	"webfetch": "WebFetch", "websearch": "WebSearch",
	"todowrite": "TodoWrite", "todoread": "TodoRead",
	"question": "Question", "skill": "Skill",
	"multiedit": "MultiEdit", "notebook": "Notebook", "lsp": "Lsp",
	"apply_patch": "ApplyPatch",
	"subagents": "SubDispatch", "session_status": "CheckStatus",
}

var claudeHarnessCanonicalMap = map[string]string{
	"read_file": "Read", "write_file": "Write", "search_files": "Grep",
	"grep_search": "Grep", "list_directory": "Glob", "run_command": "Bash",
	"terminal": "Bash", "todo": "TodoWrite", "todo_write": "TodoWrite",
	"todo_read": "TodoRead", "patch": "Edit", "multi_edit": "MultiEdit",
}

var claudeBuiltinToolNames = func() map[string]struct{} {
	m := make(map[string]struct{}, len(claudeToolRenameMap))
	for _, v := range claudeToolRenameMap {
		m[v] = struct{}{}
	}
	return m
}()

var versionedServerToolType = regexp.MustCompile(`^[a-z][a-z0-9_]*_\d{8}$`)

var nonVersionedServerToolTypes = map[string]struct{}{
	"web_search": {}, "web_search_preview": {},
}

func isAnthropicServerToolType(t interface{}) bool {
	s, ok := t.(string)
	if !ok || s == "" {
		return false
	}
	if _, ok := nonVersionedServerToolTypes[s]; ok {
		return true
	}
	return versionedServerToolType.MatchString(s)
}

func needsThirdPartyCloak(name string) bool {
	if name == "" {
		return false
	}
	if _, ok := claudeBuiltinToolNames[name]; ok {
		return false
	}
	if strings.HasPrefix(name, "mcp__") {
		return false
	}
	if name[0] >= 'a' && name[0] <= 'z' {
		return true
	}
	return strings.Contains(name, "_") || strings.Contains(name, "-")
}

func toPascalCaseToolName(name string) string {
	parts := regexp.MustCompile(`[_\s-]+`).Split(name, -1)
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		b.WriteString(strings.ToUpper(p[:1]))
		if len(p) > 1 {
			b.WriteString(p[1:])
		}
	}
	if b.Len() == 0 {
		return name
	}
	return b.String()
}

func collectServerToolNames(tools []interface{}) map[string]struct{} {
	out := map[string]struct{}{}
	for _, raw := range tools {
		t, ok := raw.(map[string]interface{})
		if !ok || t == nil {
			continue
		}
		if isAnthropicServerToolType(t["type"]) {
			if n, ok := t["name"].(string); ok && n != "" {
				out[n] = struct{}{}
			}
		}
	}
	return out
}

func asToolMaps(v interface{}) []map[string]interface{} {
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(arr))
	for _, item := range arr {
		if m, ok := item.(map[string]interface{}); ok {
			out = append(out, m)
		}
	}
	return out
}

func asMessageMaps(v interface{}) []map[string]interface{} {
	return asToolMaps(v) // same shape
}

// remapClaudeToolNames lowercase→TitleCase for known Claude Code tools.
func remapClaudeToolNames(body map[string]interface{}) {
	tools := asToolMaps(body["tools"])
	server := collectServerToolNames(interfaceSlice(body["tools"]))
	nameMap := toolNameMapFrom(body)

	for _, tool := range tools {
		if isAnthropicServerToolType(tool["type"]) {
			continue
		}
		name, _ := tool["name"].(string)
		if mapped, ok := claudeToolRenameMap[name]; ok {
			tool["name"] = mapped
			nameMap[mapped] = name
		}
	}
	if msgs := asMessageMaps(body["messages"]); msgs != nil {
		for _, msg := range msgs {
			content, ok := msg["content"].([]interface{})
			if !ok {
				continue
			}
			for _, raw := range content {
				block, ok := raw.(map[string]interface{})
				if !ok {
					continue
				}
				if block["type"] != "tool_use" {
					continue
				}
				name, _ := block["name"].(string)
				if _, skip := server[name]; skip {
					continue
				}
				if mapped, ok := claudeToolRenameMap[name]; ok {
					block["name"] = mapped
					nameMap[mapped] = name
				}
			}
		}
	}
	if tc, ok := body["tool_choice"].(map[string]interface{}); ok {
		if tc["type"] == "tool" {
			name, _ := tc["name"].(string)
			if _, skip := server[name]; !skip {
				if mapped, ok := claudeToolRenameMap[name]; ok {
					tc["name"] = mapped
					nameMap[mapped] = name
				}
			}
		}
	}
	if len(nameMap) > 0 {
		body["_toolNameMap"] = nameMap
	}
}

// cloakThirdPartyToolNames aliases snake_case / harness tools to PascalCase.
func cloakThirdPartyToolNames(body map[string]interface{}) {
	if os.Getenv("CLAUDE_DISABLE_TOOL_NAME_CLOAK") == "true" {
		return
	}
	toolsRaw, _ := body["tools"].([]interface{})
	server := collectServerToolNames(toolsRaw)
	nameMap := toolNameMapFrom(body)

	used := map[string]struct{}{}
	for _, tool := range asToolMaps(body["tools"]) {
		if n, ok := tool["name"].(string); ok {
			used[n] = struct{}{}
		}
	}
	for alias := range nameMap {
		used[alias] = struct{}{}
	}
	assigned := map[string]string{}

	aliasFor := func(original string) string {
		if a, ok := assigned[original]; ok {
			return a
		}
		base := original
		if m, ok := claudeToolRenameMap[original]; ok {
			base = m
		} else if m, ok := claudeHarnessCanonicalMap[original]; ok {
			base = m
		} else {
			base = toPascalCaseToolName(original)
		}
		alias := base
		suffix := 2
		for alias != original {
			if _, taken := used[alias]; !taken {
				break
			}
			alias = base + itoa(suffix)
			suffix++
		}
		delete(used, original)
		used[alias] = struct{}{}
		assigned[original] = alias
		nameMap[alias] = original
		return alias
	}

	shouldCloak := func(name string) bool {
		return needsThirdPartyCloak(name)
	}

	if toolsRaw != nil {
		newTools := make([]interface{}, len(toolsRaw))
		for i, raw := range toolsRaw {
			tool, ok := raw.(map[string]interface{})
			if !ok {
				newTools[i] = raw
				continue
			}
			if isAnthropicServerToolType(tool["type"]) {
				newTools[i] = tool
				continue
			}
			name, _ := tool["name"].(string)
			if shouldCloak(name) {
				cp := cloneMap(tool)
				cp["name"] = aliasFor(name)
				newTools[i] = cp
			} else {
				newTools[i] = tool
			}
		}
		body["tools"] = newTools
	}

	if msgs, ok := body["messages"].([]interface{}); ok {
		newMsgs := make([]interface{}, len(msgs))
		for i, raw := range msgs {
			msg, ok := raw.(map[string]interface{})
			if !ok {
				newMsgs[i] = raw
				continue
			}
			content, ok := msg["content"].([]interface{})
			if !ok {
				newMsgs[i] = msg
				continue
			}
			changed := false
			newContent := make([]interface{}, len(content))
			for j, braw := range content {
				block, ok := braw.(map[string]interface{})
				if !ok {
					newContent[j] = braw
					continue
				}
				name, _ := block["name"].(string)
				if block["type"] == "tool_use" {
					if _, skip := server[name]; !skip && shouldCloak(name) {
						cp := cloneMap(block)
						cp["name"] = aliasFor(name)
						newContent[j] = cp
						changed = true
						continue
					}
				}
				newContent[j] = block
			}
			if changed {
				cp := cloneMap(msg)
				cp["content"] = newContent
				newMsgs[i] = cp
			} else {
				newMsgs[i] = msg
			}
		}
		body["messages"] = newMsgs
	}

	if tc, ok := body["tool_choice"].(map[string]interface{}); ok {
		name, _ := tc["name"].(string)
		if tc["type"] == "tool" {
			if _, skip := server[name]; !skip && shouldCloak(name) {
				cp := cloneMap(tc)
				cp["name"] = aliasFor(name)
				body["tool_choice"] = cp
			}
		}
	}

	if len(nameMap) > 0 {
		body["_toolNameMap"] = nameMap
	}
}

func stripProxyToolPrefix(body map[string]interface{}) {
	strip := func(n string) string {
		if strings.HasPrefix(n, "proxy_") {
			return n[len("proxy_"):]
		}
		return n
	}
	for _, tool := range asToolMaps(body["tools"]) {
		if n, ok := tool["name"].(string); ok {
			tool["name"] = strip(n)
		}
	}
	if tc, ok := body["tool_choice"].(map[string]interface{}); ok {
		if n, ok := tc["name"].(string); ok {
			tc["name"] = strip(n)
		}
	}
	for _, msg := range asMessageMaps(body["messages"]) {
		content, ok := msg["content"].([]interface{})
		if !ok {
			continue
		}
		for _, raw := range content {
			block, ok := raw.(map[string]interface{})
			if !ok || block["type"] != "tool_use" {
				continue
			}
			if n, ok := block["name"].(string); ok {
				block["name"] = strip(n)
			}
		}
	}
}

func stripToolCacheControl(body map[string]interface{}) {
	for _, tool := range asToolMaps(body["tools"]) {
		delete(tool, "cache_control")
	}
}

func stripVersionedToolModelPrefix(body map[string]interface{}) {
	for _, tool := range asToolMaps(body["tools"]) {
		typ, _ := tool["type"].(string)
		model, _ := tool["model"].(string)
		if versionedServerToolType.MatchString(typ) && strings.Contains(model, "/") {
			parts := strings.Split(model, "/")
			tool["model"] = parts[len(parts)-1]
		}
	}
}

func toolNameMapFrom(body map[string]interface{}) map[string]string {
	if m, ok := body["_toolNameMap"].(map[string]string); ok && m != nil {
		return m
	}
	m := map[string]string{}
	body["_toolNameMap"] = m
	return m
}

func interfaceSlice(v interface{}) []interface{} {
	if a, ok := v.([]interface{}); ok {
		return a
	}
	return nil
}

func cloneMap(m map[string]interface{}) map[string]interface{} {
	cp := make(map[string]interface{}, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [16]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
