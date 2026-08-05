package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ToolLimits caps tool outputs so Coder context stays within budget.
type ToolLimits struct {
	ReadFileDefaultLines int
	ReadFileMaxBytes     int
	GrepMaxBytes         int
}

func (l ToolLimits) withDefaults() ToolLimits {
	if l.ReadFileDefaultLines <= 0 {
		l.ReadFileDefaultLines = 400
	}
	if l.ReadFileMaxBytes <= 0 {
		l.ReadFileMaxBytes = 32 << 10
	}
	if l.GrepMaxBytes <= 0 {
		l.GrepMaxBytes = 16 << 10
	}
	return l
}

// NewChrootedRegistry creates tools restricted to rootDir.
// Optional limits zero-values mean package defaults (400 lines / 32KB / 16KB).
func NewChrootedRegistry(rootDir string, limits ...ToolLimits) *ToolRegistry {
	r := &ToolRegistry{tools: make(map[string]*Tool)}
	rootDir, _ = filepath.Abs(rootDir)
	lim := ToolLimits{}
	if len(limits) > 0 {
		lim = limits[0]
	}
	r.registerChrootedTools(rootDir, lim.withDefaults())
	return r
}

func (r *ToolRegistry) registerChrootedTools(root string, limits ToolLimits) {
	resolve := func(rel string) (string, error) {
		if strings.TrimSpace(rel) == "" || rel == "." {
			return root, nil
		}
		rel = strings.ReplaceAll(rel, "\\", "/")
		rel = strings.TrimPrefix(rel, "/")
		clean := filepath.Clean(filepath.Join(root, filepath.FromSlash(rel)))
		if clean != root && !strings.HasPrefix(clean, root+string(os.PathSeparator)) {
			return "", fmt.Errorf("path escapes workspace: %s", rel)
		}
		return clean, nil
	}

	relDisplay := func(abs string) string {
		rel, err := filepath.Rel(root, abs)
		if err != nil {
			return abs
		}
		return filepath.ToSlash(rel)
	}

	r.Register(&Tool{
		Name:        "list_directory",
		Description: "List files in a directory relative to the project root. Use '.' for root.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Relative directory path (default '.')",
				},
			},
		},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			path, _ := args["path"].(string)
			if path == "" {
				path = "."
			}
			abs, err := resolve(path)
			if err != nil {
				return "", err
			}
			entries, err := os.ReadDir(abs)
			if err != nil {
				return "", err
			}
			var b strings.Builder
			for _, e := range entries {
				if e.IsDir() {
					b.WriteString("[DIR]  " + e.Name() + "\n")
				} else {
					b.WriteString("[FILE] " + e.Name() + "\n")
				}
			}
			if b.Len() == 0 {
				return "(empty)", nil
			}
			return b.String(), nil
		},
	})

	r.Register(&Tool{
		Name: "read_file",
		Description: fmt.Sprintf(
			"Read a text file relative to the project root. Prefer offset/limit for large files. "+
				"Default window is %d lines / %d bytes max when limit is omitted.",
			limits.ReadFileDefaultLines, limits.ReadFileMaxBytes,
		),
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Relative file path",
				},
				"offset": map[string]interface{}{
					"type":        "integer",
					"description": "1-based start line (optional)",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Max number of lines to return (optional; defaults to a small window)",
				},
			},
			"required": []string{"path"},
		},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			path, _ := args["path"].(string)
			abs, err := resolve(path)
			if err != nil {
				return "", err
			}
			data, err := os.ReadFile(abs)
			if err != nil {
				return "", err
			}
			text := string(data)
			lines := strings.Split(text, "\n")
			offset := intFromArg(args["offset"], 0)
			limit := intFromArg(args["limit"], 0)
			if limit <= 0 {
				limit = limits.ReadFileDefaultLines
			}
			if limit > 800 {
				limit = 800
			}
			start := 0
			if offset > 0 {
				start = offset - 1
				if start < 0 {
					start = 0
				}
				if start > len(lines) {
					start = len(lines)
				}
			}
			end := start + limit
			if end > len(lines) {
				end = len(lines)
			}
			out := strings.Join(lines[start:end], "\n")
			header := fmt.Sprintf("# %s lines %d-%d of %d\n", relDisplay(abs), start+1, end, len(lines))
			if end < len(lines) {
				out += fmt.Sprintf("\n…[%d more lines; pass offset/limit to continue]", len(lines)-end)
			}
			out = header + out
			if len(out) > limits.ReadFileMaxBytes {
				return out[:limits.ReadFileMaxBytes] + "\n…[truncated to read_file_max_bytes]", nil
			}
			return out, nil
		},
	})

	r.Register(&Tool{
		Name:        "write_file",
		Description: "Write content to a file relative to the project root (creates parent dirs).",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Relative file path",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "File content",
				},
			},
			"required": []string{"path", "content"},
		},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			path, _ := args["path"].(string)
			content, _ := args["content"].(string)
			abs, err := resolve(path)
			if err != nil {
				return "", err
			}
			if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
				return "", err
			}
			if err := os.WriteFile(abs, []byte(content), 0644); err != nil {
				return "", err
			}
			return "Wrote " + relDisplay(abs), nil
		},
	})

	r.Register(&Tool{
		Name:        "search_files",
		Description: "Find files by glob pattern under the project (e.g. **/*.go).",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"pattern": map[string]interface{}{
					"type":        "string",
					"description": "Glob pattern relative to project root",
				},
			},
			"required": []string{"pattern"},
		},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			pattern, _ := args["pattern"].(string)
			pattern = strings.TrimPrefix(filepath.ToSlash(pattern), "/")
			var matches []string
			err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
				if err != nil || info == nil || info.IsDir() {
					return nil
				}
				rel, err := filepath.Rel(root, path)
				if err != nil {
					return nil
				}
				rel = filepath.ToSlash(rel)
				ok, _ := filepath.Match(pattern, rel)
				if !ok {
					ok, _ = filepath.Match(pattern, filepath.Base(rel))
				}
				if !ok && strings.HasPrefix(pattern, "**/") {
					ok, _ = filepath.Match(strings.TrimPrefix(pattern, "**/"), filepath.Base(rel))
				}
				if ok {
					matches = append(matches, rel)
				}
				if len(matches) >= 200 {
					return fmt.Errorf("limit")
				}
				return nil
			})
			if err != nil && err.Error() != "limit" {
				return "", err
			}
			if len(matches) == 0 {
				return "No matches", nil
			}
			return strings.Join(matches, "\n"), nil
		},
	})

	r.Register(&Tool{
		Name:        "grep",
		Description: "Search file contents with a regex/string under the project. Output is capped for context budget.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"pattern": map[string]interface{}{
					"type":        "string",
					"description": "Search pattern",
				},
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Relative path (file or dir), default '.'",
				},
			},
			"required": []string{"pattern"},
		},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			pattern, _ := args["pattern"].(string)
			path, _ := args["path"].(string)
			if path == "" {
				path = "."
			}
			abs, err := resolve(path)
			if err != nil {
				return "", err
			}
			cmd := exec.CommandContext(ctx, "grep", "-RIn", "--exclude-dir=.git", pattern, abs)
			cmd.Dir = root
			out, err := cmd.CombinedOutput()
			if err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
					return "No matches found", nil
				}
				return string(out), err
			}
			text := string(out)
			text = strings.ReplaceAll(text, root+string(os.PathSeparator), "")
			if len(text) > limits.GrepMaxBytes {
				return text[:limits.GrepMaxBytes] + "\n…[truncated to grep_max_bytes]", nil
			}
			return text, nil
		},
	})

	r.Register(&Tool{
		Name:        "bash",
		Description: "Run a shell command with cwd set to the project root. Prefer read/write/list tools for file edits.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]interface{}{
					"type":        "string",
					"description": "Shell command",
				},
			},
			"required": []string{"command"},
		},
		Handler: func(ctx context.Context, args map[string]interface{}) (string, error) {
			command, _ := args["command"].(string)
			if strings.TrimSpace(command) == "" {
				return "", fmt.Errorf("empty command")
			}
			cmd := exec.CommandContext(ctx, "sh", "-c", command)
			cmd.Dir = root
			cmd.Env = append(os.Environ(), "PWD="+root)
			out, err := cmd.CombinedOutput()
			text := string(out)
			if len(text) > 50_000 {
				text = text[:50_000] + "\n...[truncated]"
			}
			if err != nil {
				return text, fmt.Errorf("command failed: %w", err)
			}
			return text, nil
		},
	})
}

func intFromArg(v interface{}, def int) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case json.Number:
		i, err := n.Int64()
		if err == nil {
			return int(i)
		}
	}
	return def
}
