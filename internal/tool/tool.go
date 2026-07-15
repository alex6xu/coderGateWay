package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Tool represents a tool definition
type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"`
	Handler     func(ctx context.Context, args map[string]interface{}) (string, error)
}

// ToolRegistry manages tools
type ToolRegistry struct {
	tools map[string]*Tool
}

// NewToolRegistry creates a new tool registry
func NewToolRegistry() *ToolRegistry {
	r := &ToolRegistry{
		tools: make(map[string]*Tool),
	}
	// Register built-in tools
	r.registerBuiltinTools()
	return r
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

// List returns all registered tools in stable name order (prefix-cache friendly).
func (r *ToolRegistry) List() []*Tool {
	tools := make([]*Tool, 0, len(r.tools))
	for _, tool := range r.tools {
		tools = append(tools, tool)
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools
}

// GetSchemas returns all tool schemas for LLM in stable name order.
func (r *ToolRegistry) GetSchemas() []map[string]interface{} {
	listed := r.List()
	schemas := make([]map[string]interface{}, 0, len(listed))
	for _, tool := range listed {
		schemas = append(schemas, map[string]interface{}{
			"type": "function",
			"function": map[string]interface{}{
				"name":        tool.Name,
				"description": tool.Description,
				"parameters":  tool.Parameters,
			},
		})
	}
	return schemas
}

// IsReadOnly reports whether a tool is safe to run concurrently.
func IsReadOnly(name string) bool {
	switch name {
	case "read_file", "list_directory", "grep", "search_files":
		return true
	default:
		return false
	}
}

// registerBuiltinTools registers built-in tools
func (r *ToolRegistry) registerBuiltinTools() {
	// Bash tool
	r.Register(&Tool{
		Name:        "bash",
		Description: "Execute a shell command",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]interface{}{
					"type":        "string",
					"description": "The command to execute",
				},
			},
			"required": []string{"command"},
		},
		Handler: handleBash,
	})

	// Read file tool
	r.Register(&Tool{
		Name:        "read_file",
		Description: "Read a file",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "The file path to read",
				},
			},
			"required": []string{"path"},
		},
		Handler: handleReadFile,
	})

	// Write file tool
	r.Register(&Tool{
		Name:        "write_file",
		Description: "Write content to a file",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "The file path to write",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "The content to write",
				},
			},
			"required": []string{"path", "content"},
		},
		Handler: handleWriteFile,
	})

	// List directory tool
	r.Register(&Tool{
		Name:        "list_directory",
		Description: "List files in a directory",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "The directory path to list",
				},
			},
			"required": []string{"path"},
		},
		Handler: handleListDirectory,
	})

	// Search files tool
	r.Register(&Tool{
		Name:        "search_files",
		Description: "Search for files matching a pattern",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"pattern": map[string]interface{}{
					"type":        "string",
					"description": "The search pattern (glob)",
				},
				"path": map[string]interface{}{
					"type":        "string",
					"description": "The directory to search in",
				},
			},
			"required": []string{"pattern"},
		},
		Handler: handleSearchFiles,
	})

	// Grep tool
	r.Register(&Tool{
		Name:        "grep",
		Description: "Search for content in files",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"pattern": map[string]interface{}{
					"type":        "string",
					"description": "The search pattern (regex)",
				},
				"path": map[string]interface{}{
					"type":        "string",
					"description": "The file or directory to search in",
				},
			},
			"required": []string{"pattern"},
		},
		Handler: handleGrep,
	})
}

// handleBash handles bash command execution
func handleBash(ctx context.Context, args map[string]interface{}) (string, error) {
	command, ok := args["command"].(string)
	if !ok {
		return "", fmt.Errorf("invalid command argument")
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("command failed: %w\nOutput: %s", err, string(output))
	}

	return string(output), nil
}

// handleReadFile handles file reading
func handleReadFile(ctx context.Context, args map[string]interface{}) (string, error) {
	path, ok := args["path"].(string)
	if !ok {
		return "", fmt.Errorf("invalid path argument")
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	return string(content), nil
}

// handleWriteFile handles file writing
func handleWriteFile(ctx context.Context, args map[string]interface{}) (string, error) {
	path, ok := args["path"].(string)
	if !ok {
		return "", fmt.Errorf("invalid path argument")
	}

	content, ok := args["content"].(string)
	if !ok {
		return "", fmt.Errorf("invalid content argument")
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	return fmt.Sprintf("File written successfully: %s", path), nil
}

// handleListDirectory handles directory listing
func handleListDirectory(ctx context.Context, args map[string]interface{}) (string, error) {
	path, ok := args["path"].(string)
	if !ok {
		path = "."
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return "", fmt.Errorf("failed to read directory: %w", err)
	}

	var result strings.Builder
	for _, entry := range entries {
		if entry.IsDir() {
			result.WriteString(fmt.Sprintf("[DIR]  %s\n", entry.Name()))
		} else {
			result.WriteString(fmt.Sprintf("[FILE] %s\n", entry.Name()))
		}
	}

	return result.String(), nil
}

// handleSearchFiles handles file searching
func handleSearchFiles(ctx context.Context, args map[string]interface{}) (string, error) {
	pattern, ok := args["pattern"].(string)
	if !ok {
		return "", fmt.Errorf("invalid pattern argument")
	}

	path, _ := args["path"].(string)
	if path == "" {
		path = "."
	}

	matches, err := filepath.Glob(filepath.Join(path, pattern))
	if err != nil {
		return "", fmt.Errorf("invalid pattern: %w", err)
	}

	var result strings.Builder
	for _, match := range matches {
		result.WriteString(match + "\n")
	}

	return result.String(), nil
}

// handleGrep handles content searching
func handleGrep(ctx context.Context, args map[string]interface{}) (string, error) {
	pattern, ok := args["pattern"].(string)
	if !ok {
		return "", fmt.Errorf("invalid pattern argument")
	}

	path, _ := args["path"].(string)
	if path == "" {
		path = "."
	}

	cmd := exec.CommandContext(ctx, "grep", "-r", pattern, path)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// grep returns exit code 1 when no matches found
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return "No matches found", nil
		}
		return string(output), fmt.Errorf("grep failed: %w", err)
	}

	return string(output), nil
}

// MarshalToolCall marshals a tool call to JSON
func MarshalToolCall(name string, args map[string]interface{}) (string, error) {
	data, err := json.Marshal(args)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
