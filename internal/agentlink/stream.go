package agentlink

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"time"

	"github.com/alex/codegateway/internal/agentcore"
	provider "github.com/alex/codegateway/internal/agentprovider"
	cgprovider "github.com/alex/codegateway/internal/provider"
)

// StreamOptions configures the CG → agentprovider.StreamFn adapter.
type StreamOptions struct {
	Provider    cgprovider.Provider
	Temperature float64
	MaxTokens   int
	// PromptCacheKey enables prompt caching when non-empty.
	PromptCacheKey string
	EnableCache    bool
	// MaxStreamCalls caps StreamFn invocations (LLM turns). 0 = unlimited.
	MaxStreamCalls int
	// OnUsage is called whenever usage is observed on a completion.
	OnUsage func(cgprovider.Usage)
	// ShouldCancel aborts the stream with stopReason=aborted when true.
	ShouldCancel func() bool
}

// NewStreamFn adapts a CodeGateway Provider into an agentruntime StreamFn.
func NewStreamFn(opt StreamOptions) provider.StreamFn {
	var calls atomic.Int64
	return func(ctx context.Context, model string, llm provider.LlmContext, cfg provider.StreamConfig) (*provider.AssistantMessageEventStream, error) {
		if opt.ShouldCancel != nil && opt.ShouldCancel() {
			return errorStream(ctx, model, agentcore.StopReasonAborted, "cancelled"), nil
		}
		n := calls.Add(1)
		if opt.MaxStreamCalls > 0 && int(n) > opt.MaxStreamCalls {
			return errorStream(ctx, model, agentcore.StopReasonError, "max tool iterations reached; try a more specific request"), nil
		}

		msgs := ToProviderMessages(ConvertToLlm(llm.Messages))
		if sp := strings.TrimSpace(llm.SystemPrompt); sp != "" {
			msgs = append([]cgprovider.Message{{Role: "system", Content: sp}}, msgs...)
		}
		temp := opt.Temperature
		mt := opt.MaxTokens
		req := &cgprovider.ChatCompletionRequest{
			Model:       model,
			Messages:    msgs,
			Temperature: &temp,
			MaxTokens:   &mt,
			Tools:       ToolsFromAgent(llm.Tools),
		}
		if opt.EnableCache && opt.PromptCacheKey != "" {
			cgprovider.ApplyPromptCache(req, opt.PromptCacheKey)
		}

		s := provider.NewAssistantMessageEventStream(16)
		go func() {
			defer s.Close()
			partial := agentcore.AssistantMessage{
				RoleField: agentcore.RoleAssistant,
				Model:     model,
				Timestamp: time.Now().UnixMilli(),
			}
			_ = s.Emit(ctx, provider.StreamStartEvent{Partial: partial})

			// Prefer streaming for text deltas; fall back to non-stream.
			req.StreamOptions = &cgprovider.StreamOptions{IncludeUsage: true}
			chunks, err := opt.Provider.ChatCompletionStream(ctx, req)
			if err != nil {
				// Non-stream fallback.
				resp, cerr := opt.Provider.ChatCompletion(ctx, req)
				if cerr != nil {
					msg := errorAssistant(model, agentcore.StopReasonError, cerr.Error())
					_ = s.Emit(ctx, provider.StreamErrorEvent{Message: msg, Err: cerr})
					return
				}
				final := assistantFromCompletion(model, resp)
				if opt.OnUsage != nil {
					opt.OnUsage(resp.Usage)
				}
				_ = s.Emit(ctx, provider.StreamDoneEvent{Message: final})
				return
			}

			var content strings.Builder
			var thinking strings.Builder
			toolCalls := map[int]*cgprovider.ToolCall{}
			var finish string
			var usage cgprovider.Usage

			for chunk := range chunks {
				if ctx.Err() != nil || (opt.ShouldCancel != nil && opt.ShouldCancel()) {
					msg := errorAssistant(model, agentcore.StopReasonAborted, "cancelled")
					_ = s.Emit(ctx, provider.StreamErrorEvent{Message: msg})
					return
				}
				if chunk.Usage != nil {
					usage.Add(*chunk.Usage)
				}
				if len(chunk.Choices) == 0 {
					continue
				}
				ch := chunk.Choices[0]
				if ch.FinishReason != nil {
					finish = *ch.FinishReason
				}
				d := ch.Delta
				if d.ReasoningContent != "" {
					thinking.WriteString(d.ReasoningContent)
				}
				if d.Content != "" {
					content.WriteString(d.Content)
				}
				for _, tc := range d.ToolCalls {
					idx := 0
					if tc.Index != nil {
						idx = *tc.Index
					}
					cur, ok := toolCalls[idx]
					if !ok {
						cp := tc
						toolCalls[idx] = &cp
						continue
					}
					if tc.ID != "" {
						cur.ID = tc.ID
					}
					if tc.Type != "" {
						cur.Type = tc.Type
					}
					if tc.Function.Name != "" {
						cur.Function.Name = tc.Function.Name
					}
					cur.Function.Arguments += tc.Function.Arguments
				}

				partial = buildPartial(model, content.String(), thinking.String(), toolCalls)
				if d.Content != "" {
					_ = s.Emit(ctx, provider.StreamTextEvent{Partial: partial})
				} else if d.ReasoningContent != "" {
					_ = s.Emit(ctx, provider.StreamThinkingEvent{Partial: partial})
				} else if len(d.ToolCalls) > 0 {
					_ = s.Emit(ctx, provider.StreamToolCallEvent{Partial: partial})
				}
			}

			final := buildPartial(model, content.String(), thinking.String(), toolCalls)
			final.StopReason = FinishReasonToStop(finish, len(toolCalls) > 0)
			if final.Usage == nil && (usage.PromptTokens > 0 || usage.CompletionTokens > 0) {
				final.Usage = &agentcore.Usage{
					InputTokens:  usage.PromptTokens,
					OutputTokens: usage.CompletionTokens,
				}
			}
			if opt.OnUsage != nil {
				opt.OnUsage(usage)
			}
			_ = s.Emit(ctx, provider.StreamDoneEvent{Message: final})
		}()
		return s, nil
	}
}

func buildPartial(model, text, thinking string, toolCalls map[int]*cgprovider.ToolCall) agentcore.AssistantMessage {
	var content agentcore.ContentList
	if thinking != "" {
		content = append(content, agentcore.ThinkingContent{Type: agentcore.ContentTypeThinking, Thinking: thinking})
	}
	if text != "" {
		content = append(content, agentcore.NewTextContent(text))
	}
	// Stable order by index.
	maxIdx := -1
	for i := range toolCalls {
		if i > maxIdx {
			maxIdx = i
		}
	}
	for i := 0; i <= maxIdx; i++ {
		tc, ok := toolCalls[i]
		if !ok || tc == nil {
			continue
		}
		args := json.RawMessage(tc.Function.Arguments)
		if len(args) == 0 {
			args = json.RawMessage(`{}`)
		}
		content = append(content, agentcore.NewToolCallContent(tc.ID, tc.Function.Name, args))
	}
	stop := agentcore.StopReasonEndTurn
	if len(toolCalls) > 0 {
		stop = agentcore.StopReasonToolUse
	}
	return agentcore.AssistantMessage{
		RoleField:  agentcore.RoleAssistant,
		Content:    content,
		Model:      model,
		StopReason: stop,
		Timestamp:  time.Now().UnixMilli(),
	}
}

func assistantFromCompletion(model string, resp *cgprovider.ChatCompletionResponse) agentcore.AssistantMessage {
	if resp == nil || len(resp.Choices) == 0 {
		return errorAssistant(model, agentcore.StopReasonError, "empty model response")
	}
	msg := resp.Choices[0].Message
	am := assistantFromProvider(msg, time.Now().UnixMilli())
	am.Model = model
	am.StopReason = FinishReasonToStop(resp.Choices[0].FinishReason, len(msg.ToolCalls) > 0)
	if resp.Usage.PromptTokens > 0 || resp.Usage.CompletionTokens > 0 {
		am.Usage = &agentcore.Usage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		}
	}
	return am
}

func errorAssistant(model, stop, errMsg string) agentcore.AssistantMessage {
	return agentcore.AssistantMessage{
		RoleField:    agentcore.RoleAssistant,
		Model:        model,
		StopReason:   stop,
		ErrorMessage: errMsg,
		Timestamp:    time.Now().UnixMilli(),
	}
}

func errorStream(ctx context.Context, model, stop, errMsg string) *provider.AssistantMessageEventStream {
	s := provider.NewAssistantMessageEventStream(0)
	go func() {
		msg := errorAssistant(model, stop, errMsg)
		_ = s.Emit(ctx, provider.StreamErrorEvent{Message: msg})
		s.Close()
	}()
	return s
}
