package provider

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestSelectClaudeOAuthBetas_FullAgentOpus(t *testing.T) {
	betas := selectClaudeOAuthBetas("claude-opus-4-6", true, true, false)
	for _, want := range []string{
		"claude-code-20250219",
		"oauth-2025-04-20",
		"context-1m-2025-08-07",
		"advanced-tool-use-2025-11-20",
		"effort-2025-11-24",
	} {
		if !strings.Contains(betas, want) {
			t.Fatalf("missing beta %q in %s", want, betas)
		}
	}
}

func TestSelectClaudeOAuthBetas_Probe(t *testing.T) {
	betas := selectClaudeOAuthBetas("claude-haiku-4-5", false, false, false)
	if strings.Contains(betas, "claude-code-20250219") {
		t.Fatalf("probe should not include claude-code beta: %s", betas)
	}
	if !strings.Contains(betas, "oauth-2025-04-20") {
		t.Fatalf("expected oauth beta: %s", betas)
	}
}

func TestClaudeOAuthBodyCloak(t *testing.T) {
	p := NewClaudeProvider(&ProviderConfig{
		Name:              "claude",
		Type:              ProviderTypeClaude,
		AuthMode:          "oauth",
		APIKey:            "tok",
		ClaudeDeviceID:    strings.Repeat("ab", 32),
		ClaudeAccountUUID: "11111111-2222-4333-8444-555555555555",
	})
	body, err := p.buildBody(&ChatCompletionRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []Message{
			{Role: "system", Content: "You are OpenCode, a helpful agent.\n\nSee github.com/anomalyco/opencode for docs."},
			{Role: "user", Content: "hi from cursor"},
		},
		Tools: []Tool{{
			Type: "function",
			Function: ToolFunction{
				Name:        "read_file",
				Description: "Read a file via cursor",
				Parameters:  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
			},
		}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatal(err)
	}

	// Body field order: model should be first key in raw JSON.
	raw := string(body)
	if !strings.HasPrefix(raw, `{"model"`) {
		t.Fatalf("expected model-first body, got prefix %s", raw[:min(40, len(raw))])
	}

	sys, ok := parsed["system"].([]interface{})
	if !ok || len(sys) < 2 {
		t.Fatalf("expected system prefix blocks, got %#v", parsed["system"])
	}
	first := sys[0].(map[string]interface{})["text"].(string)
	second := sys[1].(map[string]interface{})["text"].(string)
	if !strings.HasPrefix(first, "x-anthropic-billing-header:") {
		t.Fatalf("billing line missing: %s", first)
	}
	if second != claudeCodeSentinel {
		t.Fatalf("sentinel missing: %s", second)
	}

	// Third-party identity / anchors dropped.
	sysText := ""
	for _, s := range sys {
		if b, ok := s.(map[string]interface{}); ok {
			if t, ok := b["text"].(string); ok {
				sysText += t
			}
		}
	}
	if strings.Contains(sysText, "You are OpenCode") {
		t.Fatalf("OpenCode identity should be dropped: %s", sysText)
	}
	if strings.Contains(sysText, "anomalyco/opencode") {
		t.Fatalf("opencode anchor should be dropped: %s", sysText)
	}

	// Tool cloaked to PascalCase.
	tools := parsed["tools"].([]interface{})
	toolName := tools[0].(map[string]interface{})["name"].(string)
	if toolName != "Read" {
		t.Fatalf("expected Read cloak, got %s", toolName)
	}

	// Sensitive word obfuscated in user message.
	msgs := parsed["messages"].([]interface{})
	userContent := msgs[0].(map[string]interface{})["content"].(string)
	if strings.Contains(strings.ToLower(userContent), "cursor") && !strings.Contains(userContent, zwj) {
		t.Fatalf("expected cursor obfuscation, got %q", userContent)
	}

	md := parsed["metadata"].(map[string]interface{})
	userID := md["user_id"].(string)
	var uid map[string]string
	if err := json.Unmarshal([]byte(userID), &uid); err != nil {
		t.Fatal(err)
	}
	if uid["device_id"] == "" || uid["account_uuid"] == "" || uid["session_id"] == "" {
		t.Fatalf("incomplete metadata.user_id: %#v", uid)
	}
}

func TestClaudeOAuthHeaders(t *testing.T) {
	p := NewClaudeProvider(&ProviderConfig{
		Name:           "claude",
		Type:           ProviderTypeClaude,
		AuthMode:       "oauth",
		APIKey:         "tok",
		ClaudeDeviceID: strings.Repeat("cd", 32),
	})
	req, err := p.newHTTPRequest(
		context.Background(),
		[]byte(`{"model":"claude-sonnet-4-20250514","max_tokens":16,"messages":[]}`),
		&ChatCompletionRequest{Model: "claude-sonnet-4-20250514"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(req.URL.String(), "/v1/messages?beta=true") {
		t.Fatalf("url = %s", req.URL.String())
	}
	if got := req.Header.Get("Authorization"); got != "Bearer tok" {
		t.Fatalf("Authorization = %q", got)
	}
	if got := req.Header.Get("User-Agent"); got != claudeCodeUserAgent {
		t.Fatalf("User-Agent = %q", got)
	}
	if req.Header.Get("X-Claude-Code-Session-Id") == "" {
		t.Fatal("missing session id header")
	}
	if req.Header.Get("x-app") != "cli" {
		t.Fatal("missing x-app")
	}
	if !strings.Contains(req.Header.Get("anthropic-beta"), "oauth-2025-04-20") {
		t.Fatalf("betas = %s", req.Header.Get("anthropic-beta"))
	}
}

func TestCloakToolNames(t *testing.T) {
	body := map[string]interface{}{
		"tools": []interface{}{
			map[string]interface{}{"name": "bash"},
			map[string]interface{}{"name": "mixture_of_agents"},
			map[string]interface{}{"type": "web_search_20250305", "name": "web_search"},
		},
	}
	remapClaudeToolNames(body)
	cloakThirdPartyToolNames(body)
	tools := body["tools"].([]interface{})
	if tools[0].(map[string]interface{})["name"] != "Bash" {
		t.Fatalf("bash remap: %#v", tools[0])
	}
	if tools[1].(map[string]interface{})["name"] != "MixtureOfAgents" {
		t.Fatalf("cloak: %#v", tools[1])
	}
	if tools[2].(map[string]interface{})["name"] != "web_search" {
		t.Fatalf("server tool must stay: %#v", tools[2])
	}
}

func TestFixToolPairs(t *testing.T) {
	msgs := []interface{}{
		map[string]interface{}{
			"role": "assistant",
			"content": []interface{}{
				map[string]interface{}{"type": "tool_use", "id": "a", "name": "Bash"},
				map[string]interface{}{"type": "tool_use", "id": "orphan", "name": "Read"},
			},
		},
		map[string]interface{}{
			"role": "user",
			"content": []interface{}{
				map[string]interface{}{"type": "tool_result", "tool_use_id": "a", "content": "ok"},
			},
		},
	}
	fixed := fixToolPairs(msgs)
	asst := fixed[0].(map[string]interface{})
	content := asst["content"].([]interface{})
	if len(content) != 1 {
		t.Fatalf("expected orphan stripped, got %#v", content)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
