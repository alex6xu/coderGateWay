package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/alex/codegateway/internal/config"
	"github.com/alex/codegateway/internal/db"
	"github.com/alex/codegateway/internal/model"
	"github.com/alex/codegateway/internal/provider"
	"github.com/alex/codegateway/internal/agent/memory"
	"github.com/alex/codegateway/internal/agent/promptctx"
	"github.com/alex/codegateway/internal/tool"
	"github.com/alex/codegateway/internal/workspace"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ========== Channel Handlers ==========

func handleListChannels(database *db.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}

		rows, err := database.Query(`
			SELECT id, user_id, name, type, key, base_url, models, weight, priority, status, balance, used_quota, model_mapping, groups, created_at, updated_at 
			FROM channels WHERE user_id = ? ORDER BY id DESC
		`, accountID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query channels"})
			return
		}
		defer rows.Close()

		channels := make([]model.Channel, 0)
		for rows.Next() {
			var ch model.Channel
			err := rows.Scan(&ch.ID, &ch.UserID, &ch.Name, &ch.Type, &ch.Key, &ch.BaseURL, &ch.Models, &ch.Weight, &ch.Priority, &ch.Status, &ch.Balance, &ch.UsedQuota, &ch.ModelMapping, &ch.Groups, &ch.CreatedAt, &ch.UpdatedAt)
			if err != nil {
				continue
			}
			// Mask key for security
			ch.Key = maskKey(ch.Key)
			channels = append(channels, ch)
		}

		c.JSON(http.StatusOK, gin.H{"channels": channels})
	}
}

func handleCreateChannel(database *db.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}

		var req struct {
			Name         string `json:"name" binding:"required"`
			Type         int    `json:"type" binding:"required"`
			Key          string `json:"key"`
			BaseURL      string `json:"base_url"`
			Models       string `json:"models"`
			Weight       int    `json:"weight"`
			Priority     int    `json:"priority"`
			ModelMapping string `json:"model_mapping"`
			Groups       string `json:"groups"`
		}

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Set defaults
		if req.Weight == 0 {
			req.Weight = 1
		}
		if req.Groups == "" {
			req.Groups = "default"
		}

		now := time.Now()
		result, err := database.Exec(`
			INSERT INTO channels (user_id, name, type, key, base_url, models, weight, priority, status, model_mapping, groups, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?)
		`, accountID, req.Name, req.Type, req.Key, req.BaseURL, req.Models, req.Weight, req.Priority, req.ModelMapping, req.Groups, now, now)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create channel"})
			return
		}

		id, _ := result.LastInsertId()
		c.JSON(http.StatusOK, gin.H{
			"message":    "channel created",
			"id":         id,
			"account_id": accountID,
		})
	}
}

func handleUpdateChannel(database *db.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}

		id := c.Param("id")
		channelID, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid channel id"})
			return
		}

		if !channelOwnedBy(database, channelID, accountID) {
			c.JSON(http.StatusNotFound, gin.H{"error": "channel not found"})
			return
		}

		var req struct {
			Name         *string `json:"name"`
			Type         *int    `json:"type"`
			Key          *string `json:"key"`
			BaseURL      *string `json:"base_url"`
			Models       *string `json:"models"`
			Weight       *int    `json:"weight"`
			Priority     *int    `json:"priority"`
			Status       *int    `json:"status"`
			ModelMapping *string `json:"model_mapping"`
			Groups       *string `json:"groups"`
		}

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Build update query dynamically
		query := "UPDATE channels SET updated_at = ?"
		args := []interface{}{time.Now()}

		if req.Name != nil {
			query += ", name = ?"
			args = append(args, *req.Name)
		}
		if req.Type != nil {
			query += ", type = ?"
			args = append(args, *req.Type)
		}
		if req.Key != nil {
			query += ", key = ?"
			args = append(args, *req.Key)
		}
		if req.BaseURL != nil {
			query += ", base_url = ?"
			args = append(args, *req.BaseURL)
		}
		if req.Models != nil {
			query += ", models = ?"
			args = append(args, *req.Models)
		}
		if req.Weight != nil {
			query += ", weight = ?"
			args = append(args, *req.Weight)
		}
		if req.Priority != nil {
			query += ", priority = ?"
			args = append(args, *req.Priority)
		}
		if req.Status != nil {
			query += ", status = ?"
			args = append(args, *req.Status)
		}
		if req.ModelMapping != nil {
			query += ", model_mapping = ?"
			args = append(args, *req.ModelMapping)
		}
		if req.Groups != nil {
			query += ", groups = ?"
			args = append(args, *req.Groups)
		}

		query += " WHERE id = ? AND user_id = ?"
		args = append(args, channelID, accountID)

		_, err = database.Exec(query, args...)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update channel"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "channel updated"})
	}
}

func handleDeleteChannel(database *db.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}

		id := c.Param("id")
		channelID, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid channel id"})
			return
		}

		result, err := database.Exec("DELETE FROM channels WHERE id = ? AND user_id = ?", channelID, accountID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete channel"})
			return
		}
		affected, _ := result.RowsAffected()
		if affected == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "channel not found"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "channel deleted"})
	}
}

// ========== Chat Completions Handler ==========

func handleChatCompletions(database *db.DB, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}

		var req provider.ChatCompletionRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
			return
		}

		// Find a channel that supports this model (scoped to account)
		channel, err := findChannelForModel(database, accountID, req.Model)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no available channel for model: " + req.Model})
			return
		}

		req.Model = resolveModelForChannel(channel, req.Model)
		log.Printf("[chat] account=%d model=%s channel=%s(type=%d) stream=%v", accountID, req.Model, channel.Name, channel.Type, req.Stream)

		prov, err := createProviderFromChannel(channel)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Handle streaming
		if req.Stream {
			handleStreamResponse(c, prov, &req)
			return
		}

		// Non-streaming response
		resp, err := prov.ChatCompletion(c.Request.Context(), &req)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		resp.Usage.Normalize()

		// Log usage
		logUsage(database, accountID, channel, req.Model, resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.CachedTokens)

		c.JSON(http.StatusOK, resp)
	}
}

func handleStreamResponse(c *gin.Context, prov provider.Provider, req *provider.ChatCompletionRequest) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
		return
	}

	chunks, err := prov.ChatCompletionStream(c.Request.Context(), req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	for chunk := range chunks {
		data, err := json.Marshal(chunk)
		if err != nil {
			continue
		}
		fmt.Fprintf(c.Writer, "data: %s\n\n", data)
		flusher.Flush()
	}

	fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
	flusher.Flush()
}

// ========== Agent Chat Handler ==========

func handleAgentChat(database *db.DB, cfg *config.Config, workspaceMgr *workspace.Manager, mem *memory.MemoryService) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}

		var req struct {
			Message     string `json:"message" binding:"required"`
			SessionID   string `json:"session_id"`
			Mode        string `json:"mode"`  // "coder" for code development, empty for general chat
			Model       string `json:"model"` // optional model override
			WorkspaceID string `json:"workspace_id"`
			Stream      bool   `json:"stream"`
		}

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		mode := strings.ToLower(strings.TrimSpace(req.Mode))
		platform := "web"
		if mode == "coder" {
			platform = "coder"
		}

		sessionID := req.SessionID
		if sessionID == "" {
			sessionID = uuid.New().String()
			_, err := database.Exec(`
				INSERT INTO sessions (id, user_id, title, platform, message_count, created_at, updated_at)
				VALUES (?, ?, ?, ?, 0, ?, ?)
			`, sessionID, accountID, req.Message[:min(50, len(req.Message))], platform, time.Now(), time.Now())
			if err != nil {
				// Continue anyway
			}
		} else if !sessionOwnedBy(database, sessionID, accountID) {
			c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
			return
		}

		userMsgID := uuid.New().String()
		database.Exec(`
			INSERT INTO messages (id, session_id, role, content, created_at)
			VALUES (?, ?, 'user', ?, ?)
		`, userMsgID, sessionID, req.Message, time.Now())

		modelName := cfg.Agent.DefaultModel
		if req.Model != "" {
			modelName = req.Model
		}
		channel, err := findChannelForModel(database, accountID, modelName)
		if err != nil {
			channel, err = findAnyChannel(database, accountID)
			if err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no available channel"})
				return
			}
		}

		modelName = resolveModelForChannel(channel, modelName)
		log.Printf("[chat/agent] account=%d session=%s mode=%s model=%s channel=%s(type=%d) workspace=%s stream=%v",
			accountID, sessionID, mode, modelName, channel.Name, channel.Type, req.WorkspaceID, req.Stream)

		prov, err := createProviderFromChannel(channel)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		temperature := cfg.Agent.Temperature
		maxTokens := cfg.Agent.MaxTokens
		if maxTokens <= 0 || maxTokens > 16000 {
			maxTokens = 4096
		}

		var emit func(AgentEvent)
		var flusher http.Flusher
		if req.Stream {
			c.Header("Content-Type", "text/event-stream")
			c.Header("Cache-Control", "no-cache")
			c.Header("Connection", "keep-alive")
			var okFlush bool
			flusher, okFlush = c.Writer.(http.Flusher)
			if !okFlush {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
				return
			}
			emit = func(ev AgentEvent) {
				if ev.Session == "" {
					ev.Session = sessionID
				}
				if ev.Model == "" {
					ev.Model = modelName
				}
				data, err := json.Marshal(ev)
				if err != nil {
					return
				}
				fmt.Fprintf(c.Writer, "data: %s\n\n", data)
				flusher.Flush()
			}
			emit(AgentEvent{Type: "meta", Session: sessionID, Model: modelName})
		}

		responseContent := ""
		var usage provider.Usage
		var toolSteps []map[string]string

		extraPrefix := ""
		system := chatSystemPrompt(modelName, mode)
		projectID := ""
		toolsCost := 0

		if mode == "coder" && req.WorkspaceID != "" && workspaceMgr != nil {
			ws, werr := workspaceMgr.Get(accountID, req.WorkspaceID)
			if werr != nil {
				if emit != nil {
					emit(AgentEvent{Type: "error", Content: "workspace not found"})
					fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
					flusher.Flush()
					return
				}
				c.JSON(http.StatusNotFound, gin.H{"error": "workspace not found"})
				return
			}
			projectID = ws.ID
			tree, _ := workspaceMgr.ListTree(ws, ".", true)
			hintLimit := cfg.Agent.TreeHintLimit
			if hintLimit <= 0 {
				hintLimit = 40
			}
			if hint := RankedTreeHint(tree, req.Message, hintLimit); hint != "" {
				extraPrefix = "Project files (ranked for this request):\n" + hint
			}
			system = coderSystemPrompt(modelName, ws.Name)

			// Refresh project memory snapshot for FTS (upsert)
			if cfg.Agent.MemoryConfig.Enabled && mem != nil {
				_ = mem.UpsertProjectMemory(ws.ID, "Workspace "+ws.Name+" files:\n"+hintOrEmpty(tree, hintLimit))
			}

			registry := tool.NewChrootedRegistry(ws.RootPath)
			toolsCost = promptctx.EstimateToolsSchema(toProviderTools(registry))

			seed := promptctx.Build(database.DB, promptctx.Options{
				System:          system,
				UserMessage:     req.Message,
				SessionID:       sessionID,
				ExcludeMsgID:    userMsgID,
				ExtraUserPrefix: extraPrefix,
				ProjectID:       projectID,
				Cfg:             cfg.Agent,
				Memory:          mem,
				ToolsSchemaCost: toolsCost,
			})

			responseContent, usage, toolSteps, err = runCoderAgent(
				c.Request.Context(),
				prov,
				modelName,
				seed,
				ws,
				coderOptions{
					Temperature:        temperature,
					MaxTokens:          maxTokens,
					MaxIterations:      cfg.Agent.MaxIterations,
					ToolResultMaxChars: cfg.Agent.ToolResultMaxChars,
					ParallelReadonly:   cfg.Agent.ParallelReadonlyTools,
					PromptCacheKey:     "cg-session-" + sessionID,
					EnablePromptCache:  cfg.Agent.PromptCacheEnabled,
					OnEvent:            emit,
				},
			)
			_ = workspaceMgr.RefreshStats(ws)
		} else {
			var messages []provider.Message
			if channel.Type == model.ChannelTypeMiMoFree {
				messages = buildAgentMessages(channel, modelName, req.Message, mode)
			} else {
				messages = promptctx.Build(database.DB, promptctx.Options{
					System:       system,
					UserMessage:  req.Message,
					SessionID:    sessionID,
					ExcludeMsgID: userMsgID,
					ProjectID:    projectID,
					Cfg:          cfg.Agent,
					Memory:       mem,
				})
			}

			chatReq := &provider.ChatCompletionRequest{
				Model:       modelName,
				Messages:    messages,
				Temperature: &temperature,
				MaxTokens:   &maxTokens,
			}
			if cfg.Agent.PromptCacheEnabled {
				provider.ApplyPromptCache(chatReq, "cg-session-"+sessionID)
			}

			if req.Stream && emit != nil {
				chatReq.StreamOptions = &provider.StreamOptions{IncludeUsage: true}
				chunks, serr := prov.ChatCompletionStream(c.Request.Context(), chatReq)
				if serr != nil {
					err = serr
				} else {
					var b strings.Builder
					for chunk := range chunks {
						if chunk.Usage != nil {
							usage.Add(*chunk.Usage)
						}
						if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
							b.WriteString(chunk.Choices[0].Delta.Content)
							emit(AgentEvent{Type: "delta", Content: chunk.Choices[0].Delta.Content})
						}
					}
					responseContent = b.String()
				}
			} else {
				var resp *provider.ChatCompletionResponse
				resp, err = prov.ChatCompletion(c.Request.Context(), chatReq)
				if err == nil && resp != nil {
					usage = resp.Usage
					usage.Normalize()
					if len(resp.Choices) > 0 {
						responseContent = resp.Choices[0].Message.Content
					}
				}
			}
		}

		if err != nil {
			if emit != nil {
				emit(AgentEvent{Type: "error", Content: err.Error()})
				fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
				flusher.Flush()
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		assistantMsgID := uuid.New().String()
		database.Exec(`
			INSERT INTO messages (id, session_id, role, content, model, provider, tokens, created_at)
			VALUES (?, ?, 'assistant', ?, ?, ?, ?, ?)
		`, assistantMsgID, sessionID, responseContent, modelName, channel.Name, usage.TotalTokens, time.Now())

		database.Exec(`
			UPDATE sessions SET message_count = message_count + 2, updated_at = ? WHERE id = ?
		`, time.Now(), sessionID)

		if cfg.Agent.MemoryConfig.Enabled && mem != nil {
			promptctx.MaybeCheckpoint(database.DB, mem, sessionID, cfg.Agent.SummarizeEveryTurns)
		}

		logUsage(database, accountID, channel, modelName, usage.PromptTokens, usage.CompletionTokens, usage.CachedTokens)

		if emit != nil {
			emit(AgentEvent{
				Type:      "done",
				Content:   responseContent,
				Usage:     &usage,
				Session:   sessionID,
				Model:     modelName,
				ToolSteps: toolSteps,
			})
			fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
			flusher.Flush()
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"response":      responseContent,
			"session_id":    sessionID,
			"model":         modelName,
			"usage":         usage,
			"workspace_id":  req.WorkspaceID,
			"tool_steps":    toolSteps,
			"cached_tokens": usage.CachedTokens,
		})
	}
}

func hintOrEmpty(entries []workspace.TreeEntry, limit int) string {
	return RankedTreeHint(entries, "", limit)
}

// ========== Session Handlers ==========

func handleListSessions(database *db.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}

		rows, err := database.Query(`
			SELECT id, title, platform, message_count, created_at, updated_at
			FROM sessions WHERE user_id = ? ORDER BY updated_at DESC LIMIT 50
		`, accountID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query sessions"})
			return
		}
		defer rows.Close()

		sessions := make([]map[string]interface{}, 0)
		for rows.Next() {
			var id, title, platform string
			var messageCount int
			var createdAt, updatedAt time.Time
			rows.Scan(&id, &title, &platform, &messageCount, &createdAt, &updatedAt)

			sessions = append(sessions, map[string]interface{}{
				"id":            id,
				"title":         title,
				"platform":      platform,
				"message_count": messageCount,
				"created_at":    createdAt,
				"updated_at":    updatedAt,
			})
		}

		c.JSON(http.StatusOK, gin.H{"sessions": sessions})
	}
}

func handleGetSession(database *db.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}

		id := c.Param("id")

		// Get session (must belong to account)
		var session struct {
			ID           string
			Title        string
			Platform     string
			MessageCount int
			CreatedAt    time.Time
			UpdatedAt    time.Time
		}

		err := database.QueryRow(`
			SELECT id, title, platform, message_count, created_at, updated_at
			FROM sessions WHERE id = ? AND user_id = ?
		`, id, accountID).Scan(&session.ID, &session.Title, &session.Platform, &session.MessageCount, &session.CreatedAt, &session.UpdatedAt)

		if err == sql.ErrNoRows {
			c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get session"})
			return
		}

		// Get messages
		rows, err := database.Query(`
			SELECT id, role, content, model, provider, created_at
			FROM messages WHERE session_id = ? ORDER BY created_at ASC
		`, id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get messages"})
			return
		}
		defer rows.Close()

		messages := make([]map[string]interface{}, 0)
		for rows.Next() {
			var msgID, role, content string
			var model, provider sql.NullString
			var createdAt time.Time
			rows.Scan(&msgID, &role, &content, &model, &provider, &createdAt)

			msg := map[string]interface{}{
				"id":         msgID,
				"role":       role,
				"content":    content,
				"created_at": createdAt,
			}
			if model.Valid {
				msg["model"] = model.String
			}
			if provider.Valid {
				msg["provider"] = provider.String
			}
			messages = append(messages, msg)
		}

		c.JSON(http.StatusOK, gin.H{
			"session":  session,
			"messages": messages,
		})
	}
}

// ========== Stats Handler ==========

func handleGetStats(database *db.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}

		stats := map[string]interface{}{}

		// Total sessions for account
		var totalSessions int
		database.QueryRow("SELECT COUNT(*) FROM sessions WHERE user_id = ?", accountID).Scan(&totalSessions)
		stats["totalSessions"] = totalSessions

		// Total messages for account sessions
		var totalMessages int
		database.QueryRow(`
			SELECT COUNT(*) FROM messages WHERE session_id IN (SELECT id FROM sessions WHERE user_id = ?)
		`, accountID).Scan(&totalMessages)
		stats["totalMessages"] = totalMessages

		// Active channels for account
		var activeChannels int
		database.QueryRow("SELECT COUNT(*) FROM channels WHERE status = 1 AND user_id = ?", accountID).Scan(&activeChannels)
		stats["activeChannels"] = activeChannels

		// Total tokens and cost from usage_logs for account
		var totalTokens int64
		var totalCost float64
		database.QueryRow(`
			SELECT COALESCE(SUM(prompt_tokens + completion_tokens), 0), COALESCE(SUM(cost), 0)
			FROM usage_logs WHERE user_id = ?
		`, accountID).Scan(&totalTokens, &totalCost)
		stats["totalTokens"] = totalTokens
		stats["totalCost"] = totalCost

		c.JSON(http.StatusOK, stats)
	}
}

// ========== Helper Functions ==========

func channelOwnedBy(database *db.DB, channelID, accountID int64) bool {
	var count int
	database.QueryRow("SELECT COUNT(*) FROM channels WHERE id = ? AND user_id = ?", channelID, accountID).Scan(&count)
	return count > 0
}

func sessionOwnedBy(database *db.DB, sessionID string, accountID int64) bool {
	var count int
	database.QueryRow("SELECT COUNT(*) FROM sessions WHERE id = ? AND user_id = ?", sessionID, accountID).Scan(&count)
	return count > 0
}

func findChannelForModel(database *db.DB, accountID int64, modelName string) (*model.Channel, error) {
	rows, err := database.Query(`
		SELECT id, name, type, key, base_url, models, weight, priority, status
		FROM channels WHERE status = 1 AND user_id = ? ORDER BY priority DESC, weight DESC
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	normalizedRequest := provider.NormalizeModelAlias(modelName)
	var matches []*model.Channel

	for rows.Next() {
		var ch model.Channel
		rows.Scan(&ch.ID, &ch.Name, &ch.Type, &ch.Key, &ch.BaseURL, &ch.Models, &ch.Weight, &ch.Priority, &ch.Status)

		if ch.Models == "" {
			matches = append(matches, &ch)
			continue
		}

		var models []string
		if json.Unmarshal([]byte(ch.Models), &models) == nil {
			for _, m := range models {
				if m == modelName || provider.NormalizeModelAlias(m) == normalizedRequest {
					chCopy := ch
					matches = append(matches, &chCopy)
					break
				}
			}
		}
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("no channel found for model: %s", modelName)
	}

	if normalizedRequest == "mimo-auto" || normalizedRequest == "mimo-free" || strings.HasPrefix(normalizedRequest, "mimo-") {
		for _, ch := range matches {
			if ch.Type == model.ChannelTypeMiMoFree {
				return ch, nil
			}
		}
	}

	for _, ch := range matches {
		if ch.Type == model.ChannelTypeMiMoFree && ch.Models == "" {
			return ch, nil
		}
	}

	return matches[0], nil
}

func createProviderFromChannel(channel *model.Channel) (provider.Provider, error) {
	providerCfg := &provider.ProviderConfig{
		Name:    channel.Name,
		Type:    getProviderType(channel.Type),
		BaseURL: channel.BaseURL,
		APIKey:  channel.Key,
	}
	if providerCfg.BaseURL == "" {
		providerCfg.BaseURL = getDefaultBaseURL(channel.Type)
	}
	return provider.NewProvider(providerCfg)
}

func resolveModelForChannel(channel *model.Channel, modelName string) string {
	if channel.Type == model.ChannelTypeMiMoFree {
		return provider.NormalizeModelForMiMoAuto(modelName)
	}
	return modelName
}

func buildAgentMessages(channel *model.Channel, modelName, userMessage, mode string) []provider.Message {
	if channel.Type == model.ChannelTypeMiMoFree {
		if mode == "coder" {
			return []provider.Message{{
				Role: "user",
				Content: "[CodeGateway Coder mode] Act as an expert software engineer. Prefer concrete code, diffs, and step-by-step implementation guidance.\n\n" + userMessage,
			}}
		}
		return []provider.Message{{Role: "user", Content: userMessage}}
	}

	system := fmt.Sprintf(
		"You are a helpful AI assistant. When asked about your identity, say you are the %s model served by CodeGateway.",
		modelName,
	)
	if mode == "coder" {
		system = fmt.Sprintf(
			"You are CodeGateway Coder, an expert software engineering assistant powered by %s. "+
				"Focus on writing, reviewing, debugging, refactoring, and explaining code. "+
				"Prefer concrete implementations, clear diffs, and actionable steps. "+
				"Use fenced markdown code blocks with language tags. Ask clarifying questions only when necessary.",
			modelName,
		)
	}
	return []provider.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: userMessage},
	}
}

func findAnyChannel(database *db.DB, accountID int64) (*model.Channel, error) {
	var ch model.Channel
	err := database.QueryRow(`
		SELECT id, name, type, key, base_url, models, weight, priority, status
		FROM channels WHERE status = 1 AND user_id = ? ORDER BY priority DESC, weight DESC LIMIT 1
	`, accountID).Scan(&ch.ID, &ch.Name, &ch.Type, &ch.Key, &ch.BaseURL, &ch.Models, &ch.Weight, &ch.Priority, &ch.Status)

	if err != nil {
		return nil, err
	}
	return &ch, nil
}

func getProviderType(channelType int) provider.ProviderType {
	switch channelType {
	case 1:
		return provider.ProviderTypeOpenAI
	case 2:
		return provider.ProviderTypeClaude
	case 3:
		return provider.ProviderTypeGemini
	case 4:
		return provider.ProviderTypeDeepSeek
	case 5:
		return provider.ProviderTypeOllama
	case 6:
		return provider.ProviderTypeMiMo
	case 7:
		return provider.ProviderTypeMiMoFree
	case 8:
		return provider.ProviderTypeMiMoCode
	case 9:
		return provider.ProviderTypeAgnes
	case 10:
		return provider.ProviderTypeGLM
	default:
		return provider.ProviderTypeOpenAI
	}
}

func getDefaultBaseURL(channelType int) string {
	switch channelType {
	case 1:
		return "https://api.openai.com/v1"
	case 2:
		return "https://api.anthropic.com"
	case 3:
		return "https://generativelanguage.googleapis.com/v1beta"
	case 4:
		return "https://api.deepseek.com/v1"
	case 6:
		return "https://api.xiaomimimo.com/v1"
	case 7:
		return "https://api.xiaomimimo.com"
	case 8:
		return "http://127.0.0.1:10001" // MiMoCode backend default
	case 9:
		return "https://apihub.agnes-ai.com/v1"
	case 10:
		return "https://open.bigmodel.cn/api/paas/v4"
	default:
		return "https://api.openai.com/v1"
	}
}

func maskKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "****" + key[len(key)-4:]
}

func logUsage(database *db.DB, accountID int64, channel *model.Channel, model string, promptTokens, completionTokens int, cachedTokens ...int) {
	costPerInputToken := 0.000003  // $3 per 1M tokens
	costPerOutputToken := 0.000015 // $15 per 1M tokens
	cached := 0
	if len(cachedTokens) > 0 {
		cached = cachedTokens[0]
	}
	// Cached input is typically billed at a discount (~10% of input); approximate.
	billablePrompt := promptTokens - cached
	if billablePrompt < 0 {
		billablePrompt = 0
	}
	cost := float64(billablePrompt)*costPerInputToken +
		float64(cached)*costPerInputToken*0.1 +
		float64(completionTokens)*costPerOutputToken

	database.Exec(`
		INSERT INTO usage_logs (user_id, channel_id, model, prompt_tokens, completion_tokens, cost, latency, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, 0, 1, ?)
	`, accountID, channel.ID, model, promptTokens, completionTokens, cost, time.Now())
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ========== Stub Handlers ==========

func handleClaudeMessages(database *db.DB, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "claude messages not implemented yet"})
	}
}

func handleGemini(database *db.DB, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "gemini not implemented yet"})
	}
}

func handleListTokens(database *db.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"tokens": []interface{}{}})
	}
}

func handleCreateToken(database *db.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "token management not implemented yet"})
	}
}

// ========== Message Processing ==========

func processMessage(database *db.DB, cfg *config.Config, sessionID string, message string, accountID int64) string {
	modelName := cfg.Agent.DefaultModel
	channel, err := findChannelForModel(database, accountID, modelName)
	if err != nil {
		channel, err = findAnyChannel(database, accountID)
		if err != nil {
			return "Error: No available channel. Please add a channel first."
		}
	}

	modelName = resolveModelForChannel(channel, modelName)
	log.Printf("[chat/ws] account=%d session=%s model=%s channel=%s(type=%d)", accountID, sessionID, modelName, channel.Name, channel.Type)

	prov, err := createProviderFromChannel(channel)
	if err != nil {
		return "Error: " + err.Error()
	}

	temperature := cfg.Agent.Temperature
	maxTokens := cfg.Agent.MaxTokens
	resp, err := prov.ChatCompletion(context.Background(), &provider.ChatCompletionRequest{
		Model:       modelName,
		Messages:    buildAgentMessages(channel, modelName, message, ""),
		Temperature: &temperature,
		MaxTokens:   &maxTokens,
	})

	if err != nil {
		return "Error: " + err.Error()
	}

	responseContent := ""
	if len(resp.Choices) > 0 {
		responseContent = resp.Choices[0].Message.Content
	}

	// Save messages to database
	saveMessage(database, accountID, sessionID, "user", message, "", "", 0)
	saveMessage(database, accountID, sessionID, "assistant", responseContent, modelName, channel.Name, resp.Usage.TotalTokens)

	return responseContent
}

func saveMessage(database *db.DB, accountID int64, sessionID, role, content, model, provider string, tokens int) {
	// Ensure session exists for this account
	var count int
	database.QueryRow("SELECT COUNT(*) FROM sessions WHERE id = ?", sessionID).Scan(&count)
	if count == 0 {
		database.Exec(`
			INSERT INTO sessions (id, user_id, title, platform, message_count, created_at, updated_at)
			VALUES (?, ?, ?, 'web', 0, ?, ?)
		`, sessionID, accountID, content[:min(50, len(content))], time.Now(), time.Now())
	}

	// Save message
	database.Exec(`
		INSERT INTO messages (id, session_id, role, content, model, provider, tokens, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, uuid.New().String(), sessionID, role, content, model, provider, tokens, time.Now())

	// Update session message count
	database.Exec(`
		UPDATE sessions SET message_count = message_count + 1, updated_at = ? WHERE id = ?
	`, time.Now(), sessionID)
}
