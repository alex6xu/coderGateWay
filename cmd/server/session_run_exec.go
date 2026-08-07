package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/alex/codegateway/internal/agent/memory"
	"github.com/alex/codegateway/internal/agent/promptctx"
	sessionrun "github.com/alex/codegateway/internal/agent/sessionrun"
	"github.com/alex/codegateway/internal/config"
	"github.com/alex/codegateway/internal/db"
	"github.com/alex/codegateway/internal/provider"
	"github.com/alex/codegateway/internal/tool"
	"github.com/alex/codegateway/internal/workspace"
	"github.com/google/uuid"
)

type sessionRunRuntime struct {
	store        *sessionrun.Store
	hub          *sessionrun.Hub
	database     *db.DB
	cfg          *config.Config
	workspaceMgr *workspace.Manager
	mem          *memory.MemoryService
}

func newSessionRunRuntime(database *db.DB, cfg *config.Config, workspaceMgr *workspace.Manager, mem *memory.MemoryService) *sessionRunRuntime {
	return &sessionRunRuntime{
		store:        sessionrun.NewStore(database.DB),
		hub:          sessionrun.NewHub(),
		database:     database,
		cfg:          cfg,
		workspaceMgr: workspaceMgr,
		mem:          mem,
	}
}

func (rt *sessionRunRuntime) emit(run *sessionrun.Run, typ sessionrun.EventType, ev AgentEvent) {
	if ev.Session == "" {
		ev.Session = run.SessionID
	}
	if ev.Model == "" {
		ev.Model = run.Model
	}
	ev.Type = string(typ)
	payload := ev
	stored, err := rt.store.AppendEvent(run.ID, typ, payload)
	if err != nil {
		log.Printf("[session-run] append event: %v", err)
		return
	}
	rt.hub.Publish(*stored)
}

func (rt *sessionRunRuntime) execute(ctx context.Context, run *sessionrun.Run) error {
	userContent := ""
	_ = rt.database.QueryRow(`SELECT content FROM messages WHERE id = ?`, run.TriggerMessageID).Scan(&userContent)
	if strings.TrimSpace(userContent) == "" {
		err := fmt.Errorf("trigger message missing")
		rt.emit(run, sessionrun.EventError, AgentEvent{Content: err.Error()})
		_ = rt.store.Finish(run.ID, sessionrun.StatusFailed, err.Error())
		return err
	}

	modelName := run.Model
	channel, err := findChannelForModel(rt.database, run.UserID, modelName)
	if channel == nil || err != nil {
		channel, err = findAnyChannel(rt.database, run.UserID)
		if err != nil {
			rt.emit(run, sessionrun.EventError, AgentEvent{Content: "no available channel"})
			_ = rt.store.Finish(run.ID, sessionrun.StatusFailed, "no available channel")
			return err
		}
	}
	modelName = resolveModelForChannel(channel, modelName)
	run.Model = modelName

	prov, err := createProviderFromChannel(channel)
	if err != nil {
		rt.emit(run, sessionrun.EventError, AgentEvent{Content: err.Error()})
		_ = rt.store.Finish(run.ID, sessionrun.StatusFailed, err.Error())
		return err
	}
	prov = wrapProviderWithRequestLog(rt.database, run.UserID, channel, prov)

	temperature := rt.cfg.Agent.Temperature
	maxTokens := rt.cfg.Agent.MaxTokens
	if maxTokens <= 0 || maxTokens > 16000 {
		maxTokens = 4096
	}

	rt.emit(run, sessionrun.EventMeta, AgentEvent{Session: run.SessionID, Model: modelName})

	mode := strings.ToLower(strings.TrimSpace(run.Mode))
	system := chatSystemPrompt(modelName, mode)
	var usage provider.Usage
	var toolSteps []map[string]string
	responseContent := ""
	forceCheckpoint := false
	var runErr error

	injectPending := func() []provider.Message {
		items, err := rt.store.DrainPendingForRun(run.ID)
		if err != nil || len(items) == 0 {
			return nil
		}
		out := make([]provider.Message, 0, len(items))
		for _, it := range items {
			out = append(out, provider.Message{
				Role:    "user",
				Content: "[User follow-up while tools were running]\n" + it.Content,
			})
			rt.emit(run, sessionrun.EventUserInjected, AgentEvent{Content: it.Content})
		}
		return out
	}
	shouldCancel := func() bool { return rt.store.IsCancelRequested(run.ID) }

	if mode == "coder" && run.WorkspaceID != "" && rt.workspaceMgr != nil {
		ws, werr := rt.workspaceMgr.Get(run.UserID, run.WorkspaceID)
		if werr != nil {
			runErr = fmt.Errorf("workspace not found")
			rt.emit(run, sessionrun.EventError, AgentEvent{Content: runErr.Error()})
			_ = rt.store.Finish(run.ID, sessionrun.StatusFailed, runErr.Error())
			return runErr
		}
		tree, _ := rt.workspaceMgr.ListTree(ws, ".", true)
		hintLimit := rt.cfg.Agent.TreeHintLimit
		if hintLimit <= 0 {
			hintLimit = 40
		}
		extraPrefix := ""
		if hint := RankedTreeHint(tree, userContent, hintLimit); hint != "" {
			extraPrefix = "Project files (ranked for this request):\n" + hint
		}
		system = coderSystemPrompt(modelName, ws.Name)
		if rt.cfg.Agent.MemoryConfig.Enabled && rt.mem != nil {
			_ = rt.mem.UpsertProjectMemory(ws.ID, "Workspace "+ws.Name+" files:\n"+hintOrEmpty(tree, hintLimit))
		}
		toolLimits := tool.ToolLimits{
			ReadFileDefaultLines: rt.cfg.Agent.ReadFileDefaultLines,
			ReadFileMaxBytes:     rt.cfg.Agent.ReadFileMaxBytes,
			GrepMaxBytes:         rt.cfg.Agent.GrepMaxBytes,
		}
		registry := tool.NewChrootedRegistry(ws.RootPath, toolLimits)
		toolsCost := promptctx.EstimateToolsSchema(toProviderTools(registry))
		seed := promptctx.Build(rt.database.DB, promptctx.Options{
			System:          system,
			UserMessage:     userContent,
			SessionID:       run.SessionID,
			ExcludeMsgID:    run.TriggerMessageID,
			ExtraUserPrefix: extraPrefix,
			ProjectID:       ws.ID,
			Cfg:             rt.cfg.Agent,
			Memory:          rt.mem,
			ToolsSchemaCost: toolsCost,
		})

		var compacted bool
		responseContent, usage, toolSteps, compacted, runErr = runCoderAgent(ctx, prov, modelName, seed, ws, coderOptions{
			Temperature:          temperature,
			MaxTokens:            maxTokens,
			MaxIterations:        rt.cfg.Agent.MaxIterations,
			ToolResultMaxChars:   rt.cfg.Agent.ToolResultMaxChars,
			ToolResultKeepRecent: rt.cfg.Agent.ToolResultKeepRecent,
			ContextBudgetTokens:  rt.cfg.Agent.ContextBudgetTokens,
			ContextCompactRatio:  rt.cfg.Agent.ContextCompactRatio,
			ContextTargetRatio:   rt.cfg.Agent.ContextTargetRatio,
			ParallelReadonly:     rt.cfg.Agent.ParallelReadonlyTools,
			PromptCacheKey:       "cg-session-" + run.SessionID,
			EnablePromptCache:    rt.cfg.Agent.PromptCacheEnabled,
			ToolLimits:           toolLimits,
			InjectPending:        injectPending,
			ShouldCancel:         shouldCancel,
			OnEvent: func(ev AgentEvent) {
				switch ev.Type {
				case "delta":
					rt.emit(run, sessionrun.EventDelta, ev)
				case "tool_step":
					rt.emit(run, sessionrun.EventToolStep, ev)
				case "error":
					rt.emit(run, sessionrun.EventError, ev)
				}
			},
		})
		forceCheckpoint = compacted
		_ = rt.workspaceMgr.RefreshStats(ws)
	} else {
		messages := promptctx.Build(rt.database.DB, promptctx.Options{
			System:       system,
			UserMessage:  userContent,
			SessionID:    run.SessionID,
			ExcludeMsgID: run.TriggerMessageID,
			Cfg:          rt.cfg.Agent,
			Memory:       rt.mem,
		})
		if extra := injectPending(); len(extra) > 0 {
			// For plain chat, inject before the single completion.
			messages = append(messages[:len(messages)-1], append(extra, messages[len(messages)-1])...)
		}
		chatReq := &provider.ChatCompletionRequest{
			Model:       modelName,
			Messages:    messages,
			Temperature: &temperature,
			MaxTokens:   &maxTokens,
		}
		if rt.cfg.Agent.PromptCacheEnabled {
			provider.ApplyPromptCache(chatReq, "cg-session-"+run.SessionID)
		}
		if shouldCancel() {
			runErr = fmt.Errorf("cancelled")
		} else {
			chatReq.StreamOptions = &provider.StreamOptions{IncludeUsage: true}
			chunks, serr := prov.ChatCompletionStream(ctx, chatReq)
			if serr != nil {
				runErr = serr
			} else {
				var b strings.Builder
				for chunk := range chunks {
					if shouldCancel() {
						runErr = fmt.Errorf("cancelled")
						break
					}
					if chunk.Usage != nil {
						usage.Add(*chunk.Usage)
					}
					if len(chunk.Choices) > 0 {
						delta := chunk.Choices[0].Delta
						piece := delta.Content
						if piece == "" {
							piece = delta.ReasoningContent
						}
						if piece != "" {
							b.WriteString(piece)
							rt.emit(run, sessionrun.EventDelta, AgentEvent{Content: piece})
						}
					}
				}
				responseContent = b.String()
			}
		}
	}

	if runErr != nil {
		status := sessionrun.StatusFailed
		if strings.Contains(runErr.Error(), "cancelled") {
			status = sessionrun.StatusCancelled
		}
		rt.emit(run, sessionrun.EventError, AgentEvent{Content: runErr.Error()})
		_ = rt.store.Finish(run.ID, status, runErr.Error())
		return runErr
	}

	assistantMsgID := uuid.New().String()
	_, _ = rt.database.Exec(`
		INSERT INTO messages (id, session_id, role, content, model, provider, tokens, created_at)
		VALUES (?, ?, 'assistant', ?, ?, ?, ?, ?)
	`, assistantMsgID, run.SessionID, responseContent, modelName, channel.Name, usage.TotalTokens, time.Now())
	_, _ = rt.database.Exec(`
		UPDATE sessions SET message_count = message_count + 2, updated_at = ? WHERE id = ?
	`, time.Now(), run.SessionID)

	if rt.cfg.Agent.MemoryConfig.Enabled && rt.mem != nil {
		promptctx.MaybeCheckpointEx(rt.database.DB, rt.mem, run.SessionID, rt.cfg.Agent.SummarizeEveryTurns, forceCheckpoint)
	}
	logUsage(rt.database, run.UserID, channel, modelName, usage.PromptTokens, usage.CompletionTokens, usage.CachedTokens)

	rt.emit(run, sessionrun.EventDone, AgentEvent{
		Content:   responseContent,
		Usage:     &usage,
		Session:   run.SessionID,
		Model:     modelName,
		ToolSteps: toolSteps,
	})
	_ = rt.store.Finish(run.ID, sessionrun.StatusSucceeded, "")
	return nil
}

func eventPayloadToAgentEvent(ev sessionrun.Event) AgentEvent {
	var out AgentEvent
	_ = json.Unmarshal(ev.Payload, &out)
	if out.Type == "" {
		out.Type = string(ev.Type)
	}
	return out
}
