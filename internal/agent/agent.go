package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/alex/codegateway/internal/provider"
)

// Agent represents the core agent
type Agent struct {
	provider   provider.Provider
	tools      *ToolRegistry
	memory     *MemoryService
	skills     *SkillManager
	tasks      *TaskRegistry
	cron       *Scheduler
	context    *ContextManager
	config     *AgentConfig
}

// AgentConfig represents agent configuration
type AgentConfig struct {
	DefaultModel  string
	MaxIterations int
	MaxTokens     int
	Temperature   float64
}

// NewAgent creates a new agent
func NewAgent(provider provider.Provider, config *AgentConfig) *Agent {
	return &Agent{
		provider: provider,
		tools:    NewToolRegistry(),
		memory:   NewMemoryService(),
		skills:   NewSkillManager(),
		tasks:    NewTaskRegistry(),
		cron:     NewScheduler(),
		context:  NewContextManager(),
		config:   config,
	}
}

// Run runs the agent with the given input
func (a *Agent) Run(ctx context.Context, input string) (*Response, error) {
	// 1. Load context (memory, skills, task state)
	systemPrompt, err := a.buildSystemPrompt(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to build system prompt: %w", err)
	}

	// 2. Build messages
	messages := []provider.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: input},
	}

	// 3. Agent loop
	for i := 0; i < a.config.MaxIterations; i++ {
		// Call LLM
		resp, err := a.provider.ChatCompletion(ctx, &provider.ChatCompletionRequest{
			Model:       a.config.DefaultModel,
			Messages:    messages,
			Temperature: &a.config.Temperature,
			MaxTokens:   &a.config.MaxTokens,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to call LLM: %w", err)
		}

		// Check if there are tool calls
		if len(resp.Choices) > 0 && len(resp.Choices[0].Message.ToolCalls) > 0 {
			// Execute tools
			toolResults, err := a.executeToolCalls(ctx, resp.Choices[0].Message.ToolCalls)
			if err != nil {
				return nil, fmt.Errorf("failed to execute tools: %w", err)
			}

			// Add assistant message with tool calls
			messages = append(messages, resp.Choices[0].Message)

			// Add tool results
			for _, result := range toolResults {
				messages = append(messages, provider.Message{
					Role:       "tool",
					Content:    result.Content,
					ToolCallID: result.ToolCallID,
				})
			}

			continue
		}

		// No tool calls, return response
		if len(resp.Choices) > 0 {
			return &Response{
				Content: resp.Choices[0].Message.Content,
				Usage:   resp.Usage,
			}, nil
		}
	}

	return nil, fmt.Errorf("max iterations reached")
}

// buildSystemPrompt builds the system prompt
func (a *Agent) buildSystemPrompt(ctx context.Context) (string, error) {
	prompt := "You are a helpful AI assistant. You can help with coding, research, and general tasks."

	// Add memory context
	if a.memory != nil {
		memories, err := a.memory.Recent(ctx, 10)
		if err == nil && len(memories) > 0 {
			prompt += "\n\n## Recent Memories\n"
			for _, m := range memories {
				prompt += fmt.Sprintf("- %s\n", m.Content)
			}
		}
	}

	// Add skills context
	if a.skills != nil {
		skills := a.skills.List()
		if len(skills) > 0 {
			prompt += "\n\n## Available Skills\n"
			for _, s := range skills {
				prompt += fmt.Sprintf("- %s: %s\n", s.Name, s.Description)
			}
		}
	}

	return prompt, nil
}

// executeToolCalls executes tool calls
func (a *Agent) executeToolCalls(ctx context.Context, toolCalls []provider.ToolCall) ([]ToolResult, error) {
	results := make([]ToolResult, 0, len(toolCalls))

	for _, tc := range toolCalls {
		tool, err := a.tools.Get(tc.Function.Name)
		if err != nil {
			results = append(results, ToolResult{
				ToolCallID: tc.ID,
				Content:    fmt.Sprintf("Error: tool not found: %s", tc.Function.Name),
			})
			continue
		}

		// Parse arguments
		args := make(map[string]interface{})
		paramBytes, err := json.Marshal(tc.Function.Parameters)
		if err != nil {
			results = append(results, ToolResult{
				ToolCallID: tc.ID,
				Content:    fmt.Sprintf("Error: invalid parameters: %v", err),
			})
			continue
		}
		if err := json.Unmarshal(paramBytes, &args); err != nil {
			results = append(results, ToolResult{
				ToolCallID: tc.ID,
				Content:    fmt.Sprintf("Error: invalid arguments: %v", err),
			})
			continue
		}

		// Execute tool
		result, err := tool.Handler(ctx, args)
		if err != nil {
			results = append(results, ToolResult{
				ToolCallID: tc.ID,
				Content:    fmt.Sprintf("Error: %v", err),
			})
			continue
		}

		results = append(results, ToolResult{
			ToolCallID: tc.ID,
			Content:    result,
		})
	}

	return results, nil
}

// Response represents an agent response
type Response struct {
	Content string
	Usage   provider.Usage
}

// ToolResult represents a tool execution result
type ToolResult struct {
	ToolCallID string
	Content    string
}

// ToolRegistry manages tools
type ToolRegistry struct {
	tools map[string]*Tool
}

// NewToolRegistry creates a new tool registry
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]*Tool),
	}
}

// Register registers a tool
func (r *ToolRegistry) Register(tool *Tool) {
	r.tools[tool.Name] = tool
}

// Get returns a tool by name
func (r *ToolRegistry) Get(name string) (*Tool, error) {
	tool, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("tool not found: %s", name)
	}
	return tool, nil
}

// List returns all registered tools
func (r *ToolRegistry) List() []*Tool {
	tools := make([]*Tool, 0, len(r.tools))
	for _, tool := range r.tools {
		tools = append(tools, tool)
	}
	return tools
}

// Tool represents a tool
type Tool struct {
	Name        string
	Description string
	Handler     func(ctx context.Context, args map[string]interface{}) (string, error)
}

// MemoryService manages memories
type MemoryService struct {
	// TODO: Implement memory service
}

// NewMemoryService creates a new memory service
func NewMemoryService() *MemoryService {
	return &MemoryService{}
}

// Recent returns recent memories
func (s *MemoryService) Recent(ctx context.Context, limit int) ([]Memory, error) {
	// TODO: Implement recent memories
	return nil, nil
}

// Memory represents a memory
type Memory struct {
	ID      string
	Content string
	Scope   string
	ScopeID string
	Type    string
}

// SkillManager manages skills
type SkillManager struct {
	skills map[string]*Skill
}

// NewSkillManager creates a new skill manager
func NewSkillManager() *SkillManager {
	return &SkillManager{
		skills: make(map[string]*Skill),
	}
}

// List returns all skills
func (m *SkillManager) List() []*Skill {
	skills := make([]*Skill, 0, len(m.skills))
	for _, skill := range m.skills {
		skills = append(skills, skill)
	}
	return skills
}

// Skill represents a skill
type Skill struct {
	Name        string
	Description string
	Content     string
}

// TaskRegistry manages tasks
type TaskRegistry struct {
	// TODO: Implement task registry
}

// NewTaskRegistry creates a new task registry
func NewTaskRegistry() *TaskRegistry {
	return &TaskRegistry{}
}

// Scheduler manages cron jobs
type Scheduler struct {
	// TODO: Implement scheduler
}

// NewScheduler creates a new scheduler
func NewScheduler() *Scheduler {
	return &Scheduler{}
}

// ContextManager manages context
type ContextManager struct {
	// TODO: Implement context manager
}

// NewContextManager creates a new context manager
func NewContextManager() *ContextManager {
	return &ContextManager{}
}
