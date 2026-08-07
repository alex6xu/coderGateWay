package agentlink

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/alex/codegateway/internal/agent/agentcore"
	"github.com/alex/codegateway/internal/agent/agenttool"
	"github.com/alex/codegateway/internal/tool"
)

// RegisterChrootTools wraps CodeGateway chroot tools as agentcore.AgentTool and
// registers them into an agenttool registry.
func RegisterChrootTools(reg *agenttool.ToolRegistry, src *tool.ToolRegistry) error {
	if reg == nil || src == nil {
		return fmt.Errorf("nil registry")
	}
	for _, t := range src.List() {
		if err := reg.Register(wrapChrootTool(t)); err != nil {
			return err
		}
	}
	return nil
}

type chrootAgentTool struct {
	inner *tool.Tool
}

func wrapChrootTool(t *tool.Tool) agentcore.AgentTool {
	return &chrootAgentTool{inner: t}
}

func (t *chrootAgentTool) Name() string        { return t.inner.Name }
func (t *chrootAgentTool) Description() string { return t.inner.Description }

func (t *chrootAgentTool) Schema() json.RawMessage {
	if t.inner.Parameters == nil {
		return json.RawMessage(`{"type":"object","properties":{}}`)
	}
	b, err := json.Marshal(t.inner.Parameters)
	if err != nil {
		return json.RawMessage(`{"type":"object","properties":{}}`)
	}
	return b
}

func (t *chrootAgentTool) ExecutionMode() agentcore.ToolExecutionMode {
	if tool.IsReadOnly(t.inner.Name) {
		return agentcore.ToolExecutionParallel
	}
	return agentcore.ToolExecutionSequential
}

func (t *chrootAgentTool) Execute(ctx context.Context, id string, args json.RawMessage, onUpdate agentcore.ToolUpdateFunc) (agentcore.AgentToolResult, error) {
	_ = id
	params := map[string]interface{}{}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &params); err != nil {
			return agentcore.AgentToolResult{
				Content: agentcore.ContentList{agentcore.NewTextContent("invalid tool arguments: " + err.Error())},
			}, err
		}
	}
	out, err := t.inner.Handler(ctx, params)
	text := out
	if err != nil {
		if text != "" {
			text = text + "\nError: " + err.Error()
		} else {
			text = "Error: " + err.Error()
		}
	}
	res := agentcore.AgentToolResult{
		Content: agentcore.ContentList{agentcore.NewTextContent(text)},
		Details: map[string]string{
			"tool":   t.inner.Name,
			"args":   string(args),
			"result": out,
		},
	}
	if onUpdate != nil {
		onUpdate(res)
	}
	if err != nil {
		return res, err
	}
	return res, nil
}

// AgentToolsFromChroot returns the list of wrapped tools for AgentContext.Tools.
func AgentToolsFromChroot(src *tool.ToolRegistry) []agentcore.AgentTool {
	list := src.List()
	out := make([]agentcore.AgentTool, 0, len(list))
	for _, t := range list {
		out = append(out, wrapChrootTool(t))
	}
	return out
}
