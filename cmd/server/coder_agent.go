package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/alex/codegateway/internal/agent/promptctx"
	"github.com/alex/codegateway/internal/provider"
	"github.com/alex/codegateway/internal/tool"
	"github.com/alex/codegateway/internal/workspace"
)

// AgentEvent is emitted during streaming agent/coder runs.
type AgentEvent struct {
	Type      string              `json:"type"` // meta|delta|tool_step|done|error
	Content   string              `json:"content,omitempty"`
	Step      map[string]string   `json:"step,omitempty"`
	ToolSteps []map[string]string `json:"tool_steps,omitempty"`
	Usage     *provider.Usage     `json:"usage,omitempty"`
	Model     string              `json:"model,omitempty"`
	Session   string              `json:"session_id,omitempty"`
}

type coderOptions struct {
	Temperature          float64
	MaxTokens            int
	MaxIterations        int
	ToolResultMaxChars   int
	ParallelReadonly     bool
	PromptCacheKey       string
	EnablePromptCache    bool
	OnEvent              func(AgentEvent)
}

func runCoderAgent(
	ctx context.Context,
	prov provider.Provider,
	modelName string,
	seed []provider.Message,
	ws *workspace.Workspace,
	opt coderOptions,
) (string, provider.Usage, []map[string]string, error) {
	if opt.MaxIterations <= 0 {
		opt.MaxIterations = 8
	}
	if opt.MaxTokens <= 0 {
		opt.MaxTokens = 4096
	}

	registry := tool.NewChrootedRegistry(ws.RootPath)
	tools := toProviderTools(registry)

	messages := make([]provider.Message, len(seed))
	copy(messages, seed)
	if len(messages) == 0 {
		return "", provider.Usage{}, nil, fmt.Errorf("empty coder seed messages")
	}

	var usage provider.Usage
	var steps []map[string]string
	emit := func(ev AgentEvent) {
		if opt.OnEvent != nil {
			opt.OnEvent(ev)
		}
	}

	for i := 0; i < opt.MaxIterations; i++ {
		temp := opt.Temperature
		mt := opt.MaxTokens
		req := &provider.ChatCompletionRequest{
			Model:       modelName,
			Messages:    messages,
			Temperature: &temp,
			MaxTokens:   &mt,
			Tools:       tools,
		}
		if opt.EnablePromptCache {
			provider.ApplyPromptCache(req, opt.PromptCacheKey)
		}

		resp, err := prov.ChatCompletion(ctx, req)
		if err != nil {
			emit(AgentEvent{Type: "error", Content: err.Error()})
			return "", usage, steps, err
		}

		usage.Add(resp.Usage)

		if len(resp.Choices) == 0 {
			err := fmt.Errorf("empty model response")
			emit(AgentEvent{Type: "error", Content: err.Error()})
			return "", usage, steps, err
		}

		msg := resp.Choices[0].Message
		if len(msg.ToolCalls) == 0 {
			if msg.Content != "" {
				emit(AgentEvent{Type: "delta", Content: msg.Content})
			}
			return msg.Content, usage, steps, nil
		}

		messages = append(messages, msg)

		toolMsgs, newSteps := executeToolCalls(ctx, registry, msg.ToolCalls, opt.ToolResultMaxChars, opt.ParallelReadonly, ws.ID, emit)
		steps = append(steps, newSteps...)
		messages = append(messages, toolMsgs...)
	}

	err := fmt.Errorf("max tool iterations reached; try a more specific request")
	emit(AgentEvent{Type: "error", Content: err.Error()})
	return "", usage, steps, err
}

func executeToolCalls(
	ctx context.Context,
	registry *tool.ToolRegistry,
	calls []provider.ToolCall,
	toolResultMaxChars int,
	parallelReadonly bool,
	workspaceID string,
	emit func(AgentEvent),
) ([]provider.Message, []map[string]string) {
	type result struct {
		idx     int
		msg     provider.Message
		step    map[string]string
	}

	runOne := func(i int, tc provider.ToolCall) result {
		args := map[string]interface{}{}
		raw := tc.Function.Arguments
		if raw == "" && tc.Function.Parameters != nil {
			b, _ := json.Marshal(tc.Function.Parameters)
			raw = string(b)
		}
		if raw != "" {
			_ = json.Unmarshal([]byte(raw), &args)
		}

		t, err := registry.Get(tc.Function.Name)
		content := ""
		if err != nil {
			content = fmt.Sprintf("Error: %v", err)
		} else {
			out, herr := t.Handler(ctx, args)
			if herr != nil {
				content = fmt.Sprintf("%s\nError: %v", out, herr)
			} else {
				content = out
			}
		}
		modelContent := promptctx.TruncateToolResult(content, toolResultMaxChars)
		step := map[string]string{
			"tool":   tc.Function.Name,
			"args":   raw,
			"result": truncate(content, 2000),
		}
		log.Printf("[coder] tool=%s workspace=%s", tc.Function.Name, workspaceID)
		return result{
			idx: i,
			msg: provider.Message{
				Role:       "tool",
				Content:    modelContent,
				ToolCallID: tc.ID,
			},
			step: step,
		}
	}

	allReadonly := parallelReadonly && len(calls) > 1
	if allReadonly {
		for _, tc := range calls {
			if !tool.IsReadOnly(tc.Function.Name) {
				allReadonly = false
				break
			}
		}
	}

	results := make([]result, len(calls))
	if allReadonly {
		var wg sync.WaitGroup
		for i, tc := range calls {
			wg.Add(1)
			go func(i int, tc provider.ToolCall) {
				defer wg.Done()
				results[i] = runOne(i, tc)
			}(i, tc)
		}
		wg.Wait()
	} else {
		for i, tc := range calls {
			results[i] = runOne(i, tc)
		}
	}

	msgs := make([]provider.Message, 0, len(results))
	steps := make([]map[string]string, 0, len(results))
	for _, r := range results {
		msgs = append(msgs, r.msg)
		steps = append(steps, r.step)
		if emit != nil {
			emit(AgentEvent{Type: "tool_step", Step: r.step})
		}
	}
	return msgs, steps
}

func toProviderTools(registry *tool.ToolRegistry) []provider.Tool {
	out := make([]provider.Tool, 0)
	for _, t := range registry.List() {
		out = append(out, provider.Tool{
			Type: "function",
			Function: provider.ToolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.Parameters,
			},
		})
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func coderSystemPrompt(modelName, workspaceName string) string {
	return fmt.Sprintf(
		"You are CodeGateway Coder, an expert software engineering agent powered by %s working inside a cloud workspace.\n"+
			"Project: %s (treat paths as relative to project root).\n"+
			"Use tools to explore and edit files. Prefer read_file / list_directory / grep / search_files before writing.\n"+
			"When reading large files, use offset/limit line ranges.\n"+
			"When changing code, use write_file with complete file contents for the files you modify.\n"+
			"After edits, briefly summarize what changed and how to verify.\n"+
			"Do not attempt to access paths outside the project.\n"+
			"Use retrieved memory and prior chat turns when relevant; prefer concise tool usage to save tokens.",
		modelName, workspaceName,
	)
}

func chatSystemPrompt(modelName, mode string) string {
	if mode == "coder" {
		return fmt.Sprintf(
			"You are CodeGateway Coder, an expert software engineering assistant powered by %s. "+
				"No workspace tools are attached for this turn. Prefer concrete code in fenced markdown blocks. "+
				"Use conversation memory and prior turns when relevant.",
			modelName,
		)
	}
	return fmt.Sprintf(
		"You are a helpful AI assistant powered by %s served by CodeGateway. "+
			"Use conversation memory and prior turns when relevant; keep answers concise.",
		modelName,
	)
}

// RankedTreeHint picks query-relevant paths from a workspace tree.
func RankedTreeHint(entries []workspace.TreeEntry, query string, limit int) string {
	if limit <= 0 {
		limit = 40
	}
	if len(entries) == 0 {
		return "(empty project)"
	}
	tokens := tokenizeQuery(query)
	type scored struct {
		e     workspace.TreeEntry
		score int
	}
	scoredEntries := make([]scored, 0, len(entries))
	for _, e := range entries {
		s := scorePath(e.Path, tokens)
		// Prefer source-like files slightly
		lower := strings.ToLower(e.Path)
		if strings.HasSuffix(lower, ".go") || strings.HasSuffix(lower, ".ts") || strings.HasSuffix(lower, ".tsx") ||
			strings.HasSuffix(lower, ".py") || strings.HasSuffix(lower, ".rs") || strings.HasSuffix(lower, ".java") {
			s++
		}
		if strings.Contains(lower, "node_modules") || strings.Contains(lower, ".git/") || strings.HasPrefix(lower, ".") {
			s -= 5
		}
		scoredEntries = append(scoredEntries, scored{e: e, score: s})
	}
	// Stable-ish: higher score first, then shorter path
	for i := 0; i < len(scoredEntries); i++ {
		for j := i + 1; j < len(scoredEntries); j++ {
			a, b := scoredEntries[i], scoredEntries[j]
			if b.score > a.score || (b.score == a.score && len(b.e.Path) < len(a.e.Path)) {
				scoredEntries[i], scoredEntries[j] = scoredEntries[j], scoredEntries[i]
			}
		}
	}
	var b strings.Builder
	n := limit
	if n > len(scoredEntries) {
		n = len(scoredEntries)
	}
	for i := 0; i < n; i++ {
		e := scoredEntries[i].e
		if e.IsDir {
			b.WriteString("[DIR] ")
		} else {
			b.WriteString("[FILE] ")
		}
		b.WriteString(e.Path)
		b.WriteString("\n")
	}
	if len(scoredEntries) > n {
		b.WriteString("…\n")
	}
	return b.String()
}

func tokenizeQuery(q string) []string {
	fields := strings.FieldsFunc(strings.ToLower(q), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r >= 0x4e00)
	})
	out := make([]string, 0, len(fields))
	seen := map[string]bool{}
	for _, f := range fields {
		if len([]rune(f)) < 2 || seen[f] {
			continue
		}
		seen[f] = true
		out = append(out, f)
		if len(out) >= 12 {
			break
		}
	}
	return out
}

func scorePath(path string, tokens []string) int {
	lower := strings.ToLower(path)
	score := 0
	for _, t := range tokens {
		if strings.Contains(lower, t) {
			score += 3
		}
	}
	return score
}

func summarizeTreeHint(entries []workspace.TreeEntry) string {
	return RankedTreeHint(entries, "", 40)
}
