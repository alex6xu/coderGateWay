package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	agenttask "github.com/alex/codegateway/internal/agent/task"
	"github.com/alex/codegateway/internal/config"
	"github.com/alex/codegateway/internal/db"
	"github.com/alex/codegateway/internal/gateway/profile"
	"github.com/alex/codegateway/internal/provider"
	"github.com/alex/codegateway/internal/tool"
	"github.com/alex/codegateway/internal/workspace"
	"github.com/gin-gonic/gin"
)

type agentTaskRequest struct {
	WorkspaceID  string         `json:"workspace_id"`
	RouteProfile string         `json:"route_profile"`
	Type         agenttask.Type `json:"type"`
	Prompt       string         `json:"prompt"`
}

func agentTaskStore(database *db.DB) *agenttask.Store { return agenttask.NewStore(database.DB) }

func handleCreateAgentTask(database *db.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}
		var req agentTaskRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid agent task"})
			return
		}

		workspaceID := strings.TrimSpace(req.WorkspaceID)
		if !agentTaskWorkspaceOwnedBy(database, accountID, workspaceID) {
			// Do not distinguish another tenant's workspace from an unknown one.
			c.JSON(http.StatusNotFound, gin.H{"error": "workspace not found"})
			return
		}
		selected, err := routeProfileStore(database).GetByName(accountID, req.RouteProfile)
		if errors.Is(err, profile.ErrNotFound) {
			// Profiles are scoped in the query so another tenant's name is also absent.
			c.JSON(http.StatusNotFound, gin.H{"error": "route profile not found"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load route profile"})
			return
		}

		created, err := agentTaskStore(database).Create(accountID, agenttask.CreateInput{
			WorkspaceID:    workspaceID,
			RouteProfileID: selected.ID,
			Type:           req.Type,
			Prompt:         req.Prompt,
		})
		if errors.Is(err, agenttask.ErrInvalid) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to queue agent task"})
			return
		}
		c.JSON(http.StatusAccepted, gin.H{"task": created})
	}
}

func handleListAgentTasks(database *db.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}
		tasks, err := agentTaskStore(database).List(accountID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list agent tasks"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"tasks": tasks})
	}
}

func handleGetAgentTask(database *db.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}
		task, err := agentTaskStore(database).Get(accountID, c.Param("id"))
		if errors.Is(err, agenttask.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "agent task not found"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get agent task"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"task": task})
	}
}

func agentTaskWorkspaceOwnedBy(database *db.DB, accountID int64, workspaceID string) bool {
	if workspaceID == "" {
		return false
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM workspaces WHERE id = ? AND user_id = ?`, workspaceID, accountID).Scan(&count); err != nil {
		return false
	}
	return count == 1
}

func newAgentTaskWorker(database *db.DB, workspaceMgr *workspace.Manager, cfg *config.Config) *agenttask.Worker {
	return agenttask.NewWorker(agenttask.WorkerConfig{
		Store: agentTaskStore(database),
		WorkspaceForTask: func(userID int64, workspaceID string) (*workspace.Workspace, error) {
			return workspaceMgr.Get(userID, workspaceID)
		},
		ProviderForTask: func(task *agenttask.Task) (provider.Provider, string, error) {
			return resolveAgentTaskProvider(database, task.UserID, task.RouteProfileID)
		},
		Run: func(ctx context.Context, prov provider.Provider, modelName, systemPrompt, prompt string, ws *workspace.Workspace) (string, []map[string]string, error) {
			result, _, steps, _, err := runCoderAgent(ctx, prov, modelName, []provider.Message{
				{Role: "system", Content: systemPrompt},
				{Role: "user", Content: prompt},
			}, ws, coderOptions{
				Temperature:            cfg.Agent.Temperature,
				MaxTokens:              cfg.Agent.MaxTokens,
				MaxIterations:          cfg.Agent.MaxIterations,
				ToolResultMaxChars:     cfg.Agent.ToolResultMaxChars,
				ToolResultKeepRecent:   cfg.Agent.ToolResultKeepRecent,
				ContextBudgetTokens:    cfg.Agent.ContextBudgetTokens,
				ContextCompactRatio:    cfg.Agent.ContextCompactRatio,
				ParallelReadonly:       cfg.Agent.ParallelReadonlyTools,
				EnablePromptCache:      cfg.Agent.PromptCacheEnabled,
				ToolLimits: tool.ToolLimits{
					ReadFileDefaultLines: cfg.Agent.ReadFileDefaultLines,
					ReadFileMaxBytes:     cfg.Agent.ReadFileMaxBytes,
					GrepMaxBytes:         cfg.Agent.GrepMaxBytes,
				},
			})
			return result, steps, err
		},
		RefreshStats: workspaceMgr.RefreshStats,
	})
}

// resolveAgentTaskProvider loads the profile by its durable ID and selects the
// first enabled, tenant-owned model candidate using gateway candidate ordering.
// The task itself never receives the channel or its credentials.
func resolveAgentTaskProvider(database *db.DB, userID, profileID int64) (provider.Provider, string, error) {
	selected, err := routeProfileStore(database).Get(userID, profileID)
	if err != nil {
		return nil, "", fmt.Errorf("load route profile: %w", err)
	}
	candidates, err := resolveModelCandidates(database, userID, selected.Name, selected.Models)
	if err != nil {
		return nil, "", err
	}
	candidate := firstStreamCandidate(candidates)
	prov, err := createProviderFromChannel(candidate.channel)
	if err != nil {
		return nil, "", fmt.Errorf("create provider: %w", err)
	}
	return wrapProviderWithRequestLog(database, userID, candidate.channel, prov), candidate.model, nil
}
