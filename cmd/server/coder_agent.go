package server

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/alex/codegateway/internal/agent/promptctx"
	"github.com/alex/codegateway/internal/agentcore"
	"github.com/alex/codegateway/internal/agentlink"
	"github.com/alex/codegateway/internal/agentruntime"
	"github.com/alex/codegateway/internal/agenttool"
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
	ToolResultKeepRecent int
	ContextBudgetTokens  int
	ContextCompactRatio  float64
	ContextTargetRatio   float64
	ParallelReadonly     bool
	PromptCacheKey       string
	EnablePromptCache    bool
	ToolLimits           tool.ToolLimits
	OnEvent              func(AgentEvent)
	// InjectPending is called before each LLM request (after tools) to append mid-run user context.
	InjectPending func() []provider.Message
	// ShouldCancel returns true when the durable run was cancelled.
	ShouldCancel func() bool
}

func runCoderAgent(
	ctx context.Context,
	prov provider.Provider,
	modelName string,
	seed []provider.Message,
	ws *workspace.Workspace,
	opt coderOptions,
) (string, provider.Usage, []map[string]string, bool, error) {
	if opt.MaxIterations <= 0 {
		opt.MaxIterations = 8
	}
	if opt.MaxTokens <= 0 {
		opt.MaxTokens = 4096
	}
	if len(seed) == 0 {
		return "", provider.Usage{}, nil, false, fmt.Errorf("empty coder seed messages")
	}

	chroot := tool.NewChrootedRegistry(ws.RootPath, opt.ToolLimits)
	toolReg := agenttool.NewToolRegistry()
	if err := agentlink.RegisterChrootTools(toolReg, chroot); err != nil {
		return "", provider.Usage{}, nil, false, err
	}
	agentTools := agentlink.AgentToolsFromChroot(chroot)

	var usage provider.Usage
	var steps []map[string]string
	didCompact := false
	var processOut strings.Builder
	emit := func(ev AgentEvent) {
		if opt.OnEvent != nil {
			opt.OnEvent(ev)
		}
	}

	// Tool clipping (CG): shrink tool payloads on the seed. Session-level
	// overflow is handled mid-loop by pigo compaction, not EnsureWithinBudget.
	seedCopy := make([]provider.Message, len(seed))
	copy(seedCopy, seed)
	promptctx.CompactToolMessages(seedCopy, opt.ToolResultKeepRecent, opt.ToolResultMaxChars)

	msgs := agentlink.MessagesFromProvider(seedCopy)
	if opt.InjectPending != nil {
		if extra := opt.InjectPending(); len(extra) > 0 {
			msgs = append(msgs, agentlink.MessagesFromProvider(extra)...)
		}
	}
	agentCtx := &agentcore.AgentContext{
		SystemPrompt: agentlink.ExtractSystemPrompt(seedCopy),
		Messages:     msgs,
		Tools:        agentTools,
	}

	onUsage := func(u provider.Usage) { usage.Add(u) }
	streamOpt := agentlink.StreamOptions{
		Provider:       prov,
		Temperature:    opt.Temperature,
		MaxTokens:      opt.MaxTokens,
		PromptCacheKey: opt.PromptCacheKey,
		EnableCache:    opt.EnablePromptCache,
		MaxStreamCalls: opt.MaxIterations,
		ShouldCancel:   opt.ShouldCancel,
		OnUsage:        onUsage,
	}
	streamFn := agentlink.NewStreamFn(streamOpt)
	// Summarization must not consume the coder iteration budget.
	summaryOpt := streamOpt
	summaryOpt.MaxStreamCalls = 0
	summaryFn := agentlink.NewStreamFn(summaryOpt)

	contextWindow, compactSettings := agentlink.CompactionSettingsFromBudget(
		opt.ContextBudgetTokens,
		opt.ContextCompactRatio,
		opt.ContextTargetRatio,
	)

	cfg := agentruntime.RunConfig{
		LoopConfig: agentruntime.LoopConfig{
			Model:         modelName,
			Stream:        streamFn,
			ConvertToLlm:  agentlink.ConvertToLlm,
			ContextWindow: contextWindow,
			Compaction:    compactSettings,
			SummaryStream: summaryFn,
		},
		Batch: agenttool.BatchConfig{
			ToolExecutorConfig: agenttool.ToolExecutorConfig{
				Registry:       toolReg,
				MaxResultBytes: opt.ToolResultMaxChars,
			},
			ForceSequential: !opt.ParallelReadonly,
		},
		PrepareNextTurn: func(ctx context.Context, ac *agentcore.AgentContext) *agentruntime.TurnUpdate {
			agentlink.CompactToolResults(ac.Messages, opt.ToolResultKeepRecent, opt.ToolResultMaxChars)
			return nil
		},
		GetSteeringMessages: func(ctx context.Context) []agentcore.AgentMessage {
			if opt.ShouldCancel != nil && opt.ShouldCancel() {
				return nil
			}
			if opt.InjectPending == nil {
				return nil
			}
			extra := opt.InjectPending()
			if len(extra) == 0 {
				return nil
			}
			return agentlink.MessagesFromProvider(extra)
		},
		SessionID: ws.ID,
	}

	loopStream := agentruntime.StartRun(ctx, agentCtx, cfg)
	bridge := agentlink.EventBridge{}
	var runErr error
	for ev := range loopStream.Events() {
		for _, ui := range bridge.Handle(ev) {
			switch ui.Type {
			case "delta":
				if strings.TrimSpace(ui.Content) != "" {
					processOut.WriteString(ui.Content)
					emit(AgentEvent{Type: "delta", Content: ui.Content})
				}
			case "tool_step":
				if ui.Step != nil {
					steps = append(steps, ui.Step)
					log.Printf("[coder] tool=%s workspace=%s", ui.Step["tool"], ws.ID)
					emit(AgentEvent{Type: "tool_step", Step: ui.Step})
				}
			case "error":
				runErr = fmt.Errorf("%s", ui.Content)
				emit(AgentEvent{Type: "error", Content: ui.Content})
			}
		}
		if ce, ok := ev.(agentcore.CompactionEvent); ok && ce.ErrorMessage == "" {
			didCompact = true
		}
	}
	if _, err := loopStream.Result(ctx); err != nil && runErr == nil {
		runErr = err
	}
	if opt.ShouldCancel != nil && opt.ShouldCancel() && runErr == nil {
		runErr = fmt.Errorf("cancelled")
	}

	out := strings.TrimSpace(processOut.String())
	if out == "" && runErr != nil {
		out = runErr.Error()
	}
	// Prefer the last assistant text from the context if streaming missed it.
	if out == "" {
		if last := agentcore.LastAssistantOf(agentCtx.Messages); last != nil {
			out = strings.TrimSpace(agentcore.ContentToText(last.Content))
		}
	}
	return out, usage, steps, didCompact, runErr
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

func coderSystemPrompt(modelName, workspaceName string) string {
	return fmt.Sprintf(
		"You are CodeGateway Coder, an expert software engineering agent powered by %s working inside a cloud workspace.\n"+
			"Project: %s (treat paths as relative to project root).\n"+
			"Explore with list_directory / grep / search_files first, then read_file in small windows.\n"+
			"Never dump whole large files: always pass offset/limit; continue with a later offset if needed.\n"+
			"Prefer read_file / list_directory / grep / search_files before writing.\n"+
			"When changing code, use write_file with complete file contents for the files you modify.\n"+
			"Batch large edits by file; keep only relevant snippets in context.\n"+
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
