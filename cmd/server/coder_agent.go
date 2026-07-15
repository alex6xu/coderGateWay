package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/alex/codegateway/internal/provider"
	"github.com/alex/codegateway/internal/tool"
	"github.com/alex/codegateway/internal/workspace"
)

func runCoderAgent(
	ctx context.Context,
	prov provider.Provider,
	modelName string,
	userMessage string,
	ws *workspace.Workspace,
	temperature float64,
	maxTokens int,
	maxIterations int,
) (string, provider.Usage, []map[string]string, error) {
	if maxIterations <= 0 {
		maxIterations = 8
	}

	registry := tool.NewChrootedRegistry(ws.RootPath)
	tools := toProviderTools(registry)

	system := fmt.Sprintf(
		"You are CodeGateway Coder, an expert software engineering agent working inside a cloud workspace.\n"+
			"Project root: %s (treat paths as relative to this root).\n"+
			"Use tools to explore and edit files. Prefer read_file / list_directory / grep / search_files before writing.\n"+
			"When changing code, use write_file with complete file contents for the files you modify.\n"+
			"After edits, briefly summarize what changed and how to verify.\n"+
			"Do not attempt to access paths outside the project.",
		ws.Name,
	)

	messages := []provider.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: userMessage},
	}

	var usage provider.Usage
	var steps []map[string]string

	for i := 0; i < maxIterations; i++ {
		temp := temperature
		mt := maxTokens
		resp, err := prov.ChatCompletion(ctx, &provider.ChatCompletionRequest{
			Model:       modelName,
			Messages:    messages,
			Temperature: &temp,
			MaxTokens:   &mt,
			Tools:       tools,
		})
		if err != nil {
			return "", usage, steps, err
		}

		usage.PromptTokens += resp.Usage.PromptTokens
		usage.CompletionTokens += resp.Usage.CompletionTokens
		usage.TotalTokens += resp.Usage.TotalTokens

		if len(resp.Choices) == 0 {
			return "", usage, steps, fmt.Errorf("empty model response")
		}

		msg := resp.Choices[0].Message
		if len(msg.ToolCalls) == 0 {
			return msg.Content, usage, steps, nil
		}

		messages = append(messages, msg)

		for _, tc := range msg.ToolCalls {
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

			steps = append(steps, map[string]string{
				"tool": tc.Function.Name,
				"args": raw,
				"result": truncate(content, 2000),
			})
			log.Printf("[coder] tool=%s workspace=%s", tc.Function.Name, ws.ID)

			messages = append(messages, provider.Message{
				Role:       "tool",
				Content:    content,
				ToolCallID: tc.ID,
			})
		}
	}

	return "", usage, steps, fmt.Errorf("max tool iterations reached; try a more specific request")
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

func coderFallbackPrompt(modelName, userMessage string) []provider.Message {
	system := fmt.Sprintf(
		"You are CodeGateway Coder, an expert software engineering assistant powered by %s. "+
			"No workspace is attached. Ask the user to upload a project directory for file editing, "+
			"or provide concrete code in your reply using fenced markdown blocks.",
		modelName,
	)
	return []provider.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: userMessage},
	}
}

func summarizeTreeHint(entries []workspace.TreeEntry) string {
	if len(entries) == 0 {
		return "(empty project)"
	}
	var b strings.Builder
	limit := 40
	for i, e := range entries {
		if i >= limit {
			b.WriteString("…\n")
			break
		}
		if e.IsDir {
			b.WriteString("[DIR] ")
		} else {
			b.WriteString("[FILE] ")
		}
		b.WriteString(e.Path)
		b.WriteString("\n")
	}
	return b.String()
}
