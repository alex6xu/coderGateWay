package provider

import (
	"strings"
)

// fixToolPairs removes orphan tool_use / tool_result pairs (OmniRoute contextManager).
func fixToolPairs(messages []interface{}) []interface{} {
	toolResultIDs := map[string]struct{}{}
	for _, raw := range messages {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if role == "tool" {
			if id, ok := msg["tool_call_id"].(string); ok && id != "" {
				toolResultIDs[id] = struct{}{}
			}
		}
		if role == "user" {
			if content, ok := msg["content"].([]interface{}); ok {
				for _, braw := range content {
					block, ok := braw.(map[string]interface{})
					if !ok {
						continue
					}
					if block["type"] == "tool_result" {
						if id, ok := block["tool_use_id"].(string); ok && id != "" {
							toolResultIDs[id] = struct{}{}
						}
					}
				}
			}
		}
	}

	filtered := make([]interface{}, 0, len(messages))
	for idx, raw := range messages {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			filtered = append(filtered, raw)
			continue
		}
		role, _ := msg["role"].(string)
		isLast := idx == len(messages)-1
		if role == "assistant" && !isLast {
			cp := cloneMap(msg)
			modified := false
			if tcs, ok := cp["tool_calls"].([]interface{}); ok {
				kept := make([]interface{}, 0, len(tcs))
				for _, tcRaw := range tcs {
					tc, ok := tcRaw.(map[string]interface{})
					if !ok {
						kept = append(kept, tcRaw)
						continue
					}
					id, _ := tc["id"].(string)
					if id == "" {
						kept = append(kept, tc)
						continue
					}
					if _, has := toolResultIDs[id]; has {
						kept = append(kept, tc)
					} else {
						modified = true
					}
				}
				cp["tool_calls"] = kept
			}
			if content, ok := cp["content"].([]interface{}); ok {
				kept := make([]interface{}, 0, len(content))
				for _, braw := range content {
					block, ok := braw.(map[string]interface{})
					if !ok {
						kept = append(kept, braw)
						continue
					}
					if block["type"] != "tool_use" {
						kept = append(kept, block)
						continue
					}
					id, _ := block["id"].(string)
					if id == "" {
						kept = append(kept, block)
						continue
					}
					if _, has := toolResultIDs[id]; has {
						kept = append(kept, block)
					} else {
						modified = true
					}
				}
				cp["content"] = kept
			}
			if modified {
				filtered = append(filtered, cp)
			} else {
				filtered = append(filtered, msg)
			}
			continue
		}
		filtered = append(filtered, msg)
	}

	toolCallIDs := map[string]struct{}{}
	for _, raw := range filtered {
		msg, ok := raw.(map[string]interface{})
		if !ok || msg["role"] != "assistant" {
			continue
		}
		if tcs, ok := msg["tool_calls"].([]interface{}); ok {
			for _, tcRaw := range tcs {
				if tc, ok := tcRaw.(map[string]interface{}); ok {
					if id, ok := tc["id"].(string); ok && id != "" {
						toolCallIDs[id] = struct{}{}
					}
				}
			}
		}
		if content, ok := msg["content"].([]interface{}); ok {
			for _, braw := range content {
				if block, ok := braw.(map[string]interface{}); ok && block["type"] == "tool_use" {
					if id, ok := block["id"].(string); ok && id != "" {
						toolCallIDs[id] = struct{}{}
					}
				}
			}
		}
	}

	out := make([]interface{}, 0, len(filtered))
	for _, raw := range filtered {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			out = append(out, raw)
			continue
		}
		role, _ := msg["role"].(string)
		if role == "tool" {
			id, _ := msg["tool_call_id"].(string)
			if id != "" {
				if _, has := toolCallIDs[id]; !has {
					continue
				}
			}
		}
		if role == "user" {
			if content, ok := msg["content"].([]interface{}); ok {
				kept := make([]interface{}, 0, len(content))
				for _, braw := range content {
					block, ok := braw.(map[string]interface{})
					if !ok {
						kept = append(kept, braw)
						continue
					}
					if block["type"] != "tool_result" {
						kept = append(kept, block)
						continue
					}
					id, _ := block["tool_use_id"].(string)
					if id == "" {
						kept = append(kept, block)
						continue
					}
					if _, has := toolCallIDs[id]; has {
						kept = append(kept, block)
					}
				}
				if len(kept) == 0 && len(content) > 0 {
					// only tool_results that were all orphaned
					allResults := true
					for _, braw := range content {
						if block, ok := braw.(map[string]interface{}); ok && block["type"] == "tool_result" {
							continue
						}
						allResults = false
						break
					}
					if allResults {
						continue
					}
				}
				if len(kept) != len(content) {
					cp := cloneMap(msg)
					cp["content"] = kept
					out = append(out, cp)
					continue
				}
			}
		}
		if role == "assistant" {
			hasContent := false
			switch c := msg["content"].(type) {
			case string:
				hasContent = stringsTrimSpace(c) != ""
			case []interface{}:
				hasContent = len(c) > 0
			}
			hasToolCalls := false
			if tcs, ok := msg["tool_calls"].([]interface{}); ok && len(tcs) > 0 {
				hasToolCalls = true
			}
			if !hasContent && !hasToolCalls {
				continue
			}
		}
		out = append(out, msg)
	}
	return out
}

func stringsTrimSpace(s string) string {
	return strings.TrimSpace(s)
}

// fixToolAdjacency: tool_result must be in the immediately next message.
func fixToolAdjacency(messages []interface{}) []interface{} {
	if len(messages) <= 1 {
		return messages
	}
	result := make([]interface{}, 0, len(messages))
	for i, raw := range messages {
		msg, ok := raw.(map[string]interface{})
		if !ok {
			result = append(result, raw)
			continue
		}
		var next map[string]interface{}
		if i+1 < len(messages) {
			next, _ = messages[i+1].(map[string]interface{})
		}
		if msg["role"] != "assistant" || next == nil {
			result = append(result, msg)
			continue
		}
		nextIDs := map[string]struct{}{}
		if next["role"] == "tool" {
			if id, ok := next["tool_call_id"].(string); ok {
				nextIDs[id] = struct{}{}
			}
		}
		if next["role"] == "user" {
			if content, ok := next["content"].([]interface{}); ok {
				for _, braw := range content {
					if block, ok := braw.(map[string]interface{}); ok && block["type"] == "tool_result" {
						if id, ok := block["tool_use_id"].(string); ok {
							nextIDs[id] = struct{}{}
						}
					}
				}
			}
		}
		cp := cloneMap(msg)
		modified := false
		if content, ok := cp["content"].([]interface{}); ok {
			kept := make([]interface{}, 0, len(content))
			for _, braw := range content {
				block, ok := braw.(map[string]interface{})
				if !ok {
					kept = append(kept, braw)
					continue
				}
				if block["type"] != "tool_use" {
					kept = append(kept, block)
					continue
				}
				id, _ := block["id"].(string)
				if id == "" {
					kept = append(kept, block)
					continue
				}
				if _, has := nextIDs[id]; has {
					kept = append(kept, block)
				} else {
					modified = true
				}
			}
			cp["content"] = kept
		}
		if modified {
			result = append(result, cp)
		} else {
			result = append(result, msg)
		}
	}
	return result
}

func stripTrailingAssistantOrphanToolUse(messages []interface{}) []interface{} {
	if len(messages) == 0 {
		return messages
	}
	lastIdx := len(messages) - 1
	last, ok := messages[lastIdx].(map[string]interface{})
	if !ok || last["role"] != "assistant" {
		return messages
	}
	cp := cloneMap(last)
	modified := false
	if tcs, ok := cp["tool_calls"].([]interface{}); ok && len(tcs) > 0 {
		cp["tool_calls"] = []interface{}{}
		modified = true
	}
	if content, ok := cp["content"].([]interface{}); ok {
		kept := make([]interface{}, 0, len(content))
		for _, braw := range content {
			block, ok := braw.(map[string]interface{})
			if !ok {
				kept = append(kept, braw)
				continue
			}
			if block["type"] == "tool_use" {
				modified = true
				continue
			}
			kept = append(kept, block)
		}
		cp["content"] = kept
	}
	if !modified {
		return messages
	}
	hasContent := false
	switch c := cp["content"].(type) {
	case string:
		hasContent = stringsTrimSpace(c) != ""
	case []interface{}:
		hasContent = len(c) > 0
	}
	hasToolCalls := false
	if tcs, ok := cp["tool_calls"].([]interface{}); ok && len(tcs) > 0 {
		hasToolCalls = true
	}
	out := messages[:lastIdx]
	if hasContent || hasToolCalls {
		out = append(append([]interface{}{}, out...), cp)
	}
	return out
}

func fixClaudeToolMessagePairs(body map[string]interface{}) {
	msgs, ok := body["messages"].([]interface{})
	if !ok {
		return
	}
	fixed := fixToolPairs(msgs)
	adjacent := fixToolPairs(fixToolAdjacency(fixed))
	body["messages"] = stripTrailingAssistantOrphanToolUse(adjacent)
}
