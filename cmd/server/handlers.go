package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alex/codegateway/internal/agent/memory"
	sessionrun "github.com/alex/codegateway/internal/agent/sessionrun"
	"github.com/alex/codegateway/internal/agent/tags"
	"github.com/alex/codegateway/internal/config"
	"github.com/alex/codegateway/internal/db"
	"github.com/alex/codegateway/internal/gateway/profile"
	"github.com/alex/codegateway/internal/gatewaylog"
	"github.com/alex/codegateway/internal/model"
	"github.com/alex/codegateway/internal/provider"
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
			SELECT id, user_id, name, type, key, base_url, models, weight, priority, status, balance, used_quota, model_mapping, groups, is_default, COALESCE(auth_mode, 'api_key'), created_at, updated_at
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
			err := rows.Scan(&ch.ID, &ch.UserID, &ch.Name, &ch.Type, &ch.Key, &ch.BaseURL, &ch.Models, &ch.Weight, &ch.Priority, &ch.Status, &ch.Balance, &ch.UsedQuota, &ch.ModelMapping, &ch.Groups, &ch.IsDefault, &ch.AuthMode, &ch.CreatedAt, &ch.UpdatedAt)
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
			AuthMode     string `json:"auth_mode"`
		}

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if req.Type == 7 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "mimo free channel type is no longer supported; use MiMo (API key) instead"})
			return
		}

		req.Name = strings.TrimSpace(strings.ToLower(req.Name))
		if req.Name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
			return
		}

		authMode := normalizeChannelAuthMode(req.AuthMode, req.Type)
		if authMode == "oauth" && req.Type != model.ChannelTypeClaude {
			c.JSON(http.StatusBadRequest, gin.H{"error": "auth_mode=oauth is only supported for Claude channels"})
			return
		}
		if authMode != "oauth" && strings.TrimSpace(req.Key) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "api key is required"})
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
			INSERT INTO channels (user_id, name, type, key, base_url, models, weight, priority, status, model_mapping, groups, auth_mode, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?)
		`, accountID, req.Name, req.Type, req.Key, req.BaseURL, req.Models, req.Weight, req.Priority, req.ModelMapping, req.Groups, authMode, now, now)

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
			IsDefault    *int    `json:"is_default"`
			AuthMode     *string `json:"auth_mode"`
		}

		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if req.Type != nil && *req.Type == 7 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "mimo free channel type is no longer supported; use MiMo (API key) instead"})
			return
		}

		// Build update query dynamically
		query := "UPDATE channels SET updated_at = ?"
		args := []interface{}{time.Now()}

		if req.Name != nil {
			normalized := strings.TrimSpace(strings.ToLower(*req.Name))
			if normalized == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
				return
			}
			query += ", name = ?"
			args = append(args, normalized)
		}
		if req.Type != nil {
			query += ", type = ?"
			args = append(args, *req.Type)
		}
		if req.AuthMode != nil {
			mode := normalizeChannelAuthMode(*req.AuthMode, 0)
			if mode == "oauth" {
				chType := 0
				if req.Type != nil {
					chType = *req.Type
				} else {
					_ = database.QueryRow(`SELECT type FROM channels WHERE id = ? AND user_id = ?`, channelID, accountID).Scan(&chType)
				}
				if chType != model.ChannelTypeClaude {
					c.JSON(http.StatusBadRequest, gin.H{"error": "auth_mode=oauth is only supported for Claude channels"})
					return
				}
			}
			query += ", auth_mode = ?"
			args = append(args, mode)
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
		if req.IsDefault != nil {
			query += ", is_default = ?"
			args = append(args, *req.IsDefault)
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

func handleSetDefaultChannel(database *db.DB) gin.HandlerFunc {
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

		// Clear all defaults for this account
		if _, err := database.Exec("UPDATE channels SET is_default = 0 WHERE user_id = ?", accountID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to clear defaults"})
			return
		}

		// Set this channel as default
		if _, err := database.Exec("UPDATE channels SET is_default = 1 WHERE id = ? AND user_id = ?", channelID, accountID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to set default"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "default channel set"})
	}
}

func handleFetchChannelModels(database *db.DB) gin.HandlerFunc {
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

		var ch model.Channel
		var userID int64
		err = database.QueryRow(`
			SELECT id, user_id, name, type, key, COALESCE(base_url, ''), COALESCE(models, ''), weight, priority, status,
			       COALESCE(auth_mode, 'api_key')
			FROM channels WHERE id = ? AND user_id = ?
		`, channelID, accountID).Scan(
			&ch.ID, &userID, &ch.Name, &ch.Type, &ch.Key, &ch.BaseURL, &ch.Models, &ch.Weight, &ch.Priority, &ch.Status,
			&ch.AuthMode,
		)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "channel not found"})
			return
		}
		ch.UserID = &userID

		models, err := listUpstreamModels(c.Request.Context(), &ch)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"models": models})
	}
}

// handleProbeChannelModels fetches upstream models using form credentials (for add-provider flow).
func handleProbeChannelModels() gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}

		var req struct {
			Type     int    `json:"type"`
			Key      string `json:"key"`
			BaseURL  string `json:"base_url"`
			AuthMode string `json:"auth_mode"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
			return
		}
		if req.Type == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "type is required"})
			return
		}
		authMode := strings.TrimSpace(req.AuthMode)
		if authMode == "" {
			authMode = "api_key"
		}
		if !strings.EqualFold(authMode, "oauth") && strings.TrimSpace(req.Key) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "api key is required to fetch models"})
			return
		}

		ch := &model.Channel{
			Type:     req.Type,
			Key:      req.Key,
			BaseURL:  req.BaseURL,
			AuthMode: authMode,
			UserID:   &accountID,
		}
		models, err := listUpstreamModels(c.Request.Context(), ch)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"models": models})
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
		requestJSON := gatewaylog.MarshalRequestJSON(req)
		started := time.Now()

		candidates, err := resolveChatCompletionCandidates(database, accountID, req.Model)
		if err != nil {
			saveGatewayRequestLog(database, &gatewaylog.Entry{
				UserID:      accountID,
				Model:       req.Model,
				Stream:      req.Stream,
				StatusCode:  http.StatusServiceUnavailable,
				Error:       "no available channel for model: " + req.Model,
				RequestBody: requestJSON,
				LatencyMs:   time.Since(started).Milliseconds(),
			})
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no available channel for model: " + req.Model})
			return
		}

		// Handle streaming
		if req.Stream {
			selected := firstStreamCandidate(candidates)
			req.Model = selected.model
			log.Printf("[chat] account=%d model=%s channel=%s(type=%d) stream=true", accountID, req.Model, selected.channel.Name, selected.channel.Type)
			prov, err := createProviderFromChannel(selected.channel)
			if err != nil {
				saveGatewayRequestLog(database, &gatewaylog.Entry{
					UserID:      accountID,
					ChannelID:   selected.channel.ID,
					ChannelName: selected.channel.Name,
					Model:       selected.model,
					Stream:      true,
					StatusCode:  http.StatusInternalServerError,
					Error:       err.Error(),
					RequestBody: requestJSON,
					LatencyMs:   time.Since(started).Milliseconds(),
				})
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			handleStreamResponse(c, database, accountID, selected, prov, &req, requestJSON, started)
			return
		}

		resp, selected, err := completeWithCandidates(c.Request.Context(), candidates, &req, createProviderFromChannel)
		latency := time.Since(started).Milliseconds()
		if err != nil {
			chID, chName, modelName := int64(0), "", req.Model
			if selected.channel != nil {
				chID = selected.channel.ID
				chName = selected.channel.Name
			}
			if selected.model != "" {
				modelName = selected.model
			}
			saveGatewayRequestLog(database, &gatewaylog.Entry{
				UserID:      accountID,
				ChannelID:   chID,
				ChannelName: chName,
				Model:       modelName,
				Stream:      false,
				StatusCode:  http.StatusInternalServerError,
				Error:       err.Error(),
				RequestBody: requestJSON,
				LatencyMs:   latency,
			})
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		resp.Usage.Normalize()

		respJSON, _ := json.Marshal(resp)
		saveGatewayRequestLog(database, &gatewaylog.Entry{
			UserID:           accountID,
			ChannelID:        selected.channel.ID,
			ChannelName:      selected.channel.Name,
			Model:            selected.model,
			Stream:           false,
			StatusCode:       http.StatusOK,
			RequestBody:      requestJSON,
			ResponseBody:     string(respJSON),
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			CachedTokens:     resp.Usage.CachedTokens,
			LatencyMs:        latency,
		})

		// Log usage
		logUsage(database, accountID, selected.channel, selected.model, resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.CachedTokens)

		c.JSON(http.StatusOK, resp)
	}
}

func saveGatewayRequestLog(database *db.DB, entry *gatewaylog.Entry) {
	if database == nil || entry == nil {
		return
	}
	if err := gatewayLogStore(database).Insert(entry); err != nil {
		log.Printf("[gatewaylog] insert failed: %v", err)
	}
}

// firstStreamCandidate deliberately selects one candidate only: a streaming
// response is never retried after it can have written response data.
func firstStreamCandidate(candidates []chatCompletionCandidate) chatCompletionCandidate {
	return candidates[0]
}

type chatCompletionCandidate struct {
	model   string
	channel *model.Channel
}

// resolveChatCompletionCandidates expands an owned Route Profile to enabled,
// account-owned channels. A direct model request preserves the existing
// single-channel behavior.
func resolveChatCompletionCandidates(database *db.DB, accountID int64, requestedModel string) ([]chatCompletionCandidate, error) {
	requestedModels := []string{requestedModel}
	selected, err := routeProfileStore(database).GetByName(accountID, requestedModel)
	if err == nil {
		requestedModels = selected.Models
	} else if !errors.Is(err, profile.ErrNotFound) {
		return nil, err
	}

	return resolveModelCandidates(database, accountID, requestedModel, requestedModels)
}

// resolveModelCandidates applies the enabled, account-owned channel selection
// rules shared by synchronous gateway completions and cloud Agent Tasks.
func resolveModelCandidates(database *db.DB, accountID int64, requestedName string, requestedModels []string) ([]chatCompletionCandidate, error) {
	candidates := make([]chatCompletionCandidate, 0, len(requestedModels))
	for _, requested := range requestedModels {
		channel, err := findChannelForModel(database, accountID, requested)
		if err != nil {
			continue
		}
		candidates = append(candidates, chatCompletionCandidate{
			model:   resolveModelForChannel(channel, requested),
			channel: channel,
		})
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no channel found for model: %s", requestedName)
	}
	return candidates, nil
}

// completeWithCandidates runs the failover loop over ordered candidates with
// per-channel circuit breaking, error classification, and a single all-cooled
// wait (see docs/multi-channel-failover-routing.md §7).
func completeWithCandidates(ctx context.Context, candidates []chatCompletionCandidate, request *provider.ChatCompletionRequest, newProvider func(*model.Channel) (provider.Provider, error)) (*provider.ChatCompletionResponse, chatCompletionCandidate, error) {
	resp, candidate, err, allCooled := failoverPass(ctx, candidates, request, newProvider)
	if err == nil {
		return resp, candidate, nil
	}

	// §7.2③: if every candidate was skipped purely due to cooldown, wait once
	// for the nearest cooldown to expire (within budget) and retry a single pass.
	if allCooled {
		wait := timeUntilNearestCooldown(candidates)
		if wait > 0 && wait <= maxWaitBudget {
			select {
			case <-time.After(wait):
				resp, candidate, err, _ = failoverPass(ctx, candidates, request, newProvider)
				if err == nil {
					return resp, candidate, nil
				}
			case <-ctx.Done():
				return nil, chatCompletionCandidate{}, ctx.Err()
			}
		}
	}
	return nil, chatCompletionCandidate{}, err
}

// failoverPass tries each candidate once, skipping channels in active cooldown.
// It returns allCooled=true when the only reason no attempt succeeded is that
// every candidate was skipped by its circuit breaker.
func failoverPass(ctx context.Context, candidates []chatCompletionCandidate, request *provider.ChatCompletionRequest, newProvider func(*model.Channel) (provider.Provider, error)) (*provider.ChatCompletionResponse, chatCompletionCandidate, error, bool) {
	reg := breakers()
	now := time.Now()
	var lastErr error
	attempted := false

	for _, candidate := range candidates {
		id := candidate.channel.ID
		if reg.isCoolingDown(id, now) {
			continue
		}
		attempted = true

		prov, err := newProvider(candidate.channel)
		if err != nil {
			class := classifyError(err, reg.fails(id))
			reg.reportFailure(id, class.cooldown)
			lastErr = err
			continue
		}

		attempt := *request
		attempt.Model = candidate.model
		resp, err := prov.ChatCompletion(ctx, &attempt)
		if err != nil {
			class := classifyError(err, reg.fails(id))
			reg.reportFailure(id, class.cooldown)
			lastErr = err
			if !class.retryable {
				return nil, chatCompletionCandidate{}, err, false
			}
			continue
		}

		reg.reportSuccess(id)
		resp.Model = candidate.model
		return resp, candidate, nil, false
	}

	if !attempted && len(candidates) > 0 {
		// Nothing was tried and candidates existed → all were cooling down.
		return nil, chatCompletionCandidate{}, errAllChannelsCoolingDown, true
	}
	return nil, chatCompletionCandidate{}, lastErr, false
}

var errAllChannelsCoolingDown = errors.New("all candidate channels are cooling down")

// timeUntilNearestCooldown returns the shortest remaining cooldown across
// candidates, or 0 if none are cooling down.
func timeUntilNearestCooldown(candidates []chatCompletionCandidate) time.Duration {
	reg := breakers()
	now := time.Now()
	var nearest time.Duration
	for _, candidate := range candidates {
		until := reg.cooledDownUntil(candidate.channel.ID)
		if until.IsZero() || !now.Before(until) {
			continue
		}
		d := until.Sub(now)
		if nearest == 0 || d < nearest {
			nearest = d
		}
	}
	return nearest
}

func handleStreamResponse(
	c *gin.Context,
	database *db.DB,
	accountID int64,
	selected chatCompletionCandidate,
	prov provider.Provider,
	req *provider.ChatCompletionRequest,
	requestJSON string,
	started time.Time,
) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		saveGatewayRequestLog(database, &gatewaylog.Entry{
			UserID:      accountID,
			ChannelID:   selected.channel.ID,
			ChannelName: selected.channel.Name,
			Model:       selected.model,
			Stream:      true,
			StatusCode:  http.StatusInternalServerError,
			Error:       "streaming not supported",
			RequestBody: requestJSON,
			LatencyMs:   time.Since(started).Milliseconds(),
		})
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
		return
	}

	chunks, err := prov.ChatCompletionStream(c.Request.Context(), req)
	if err != nil {
		saveGatewayRequestLog(database, &gatewaylog.Entry{
			UserID:      accountID,
			ChannelID:   selected.channel.ID,
			ChannelName: selected.channel.Name,
			Model:       selected.model,
			Stream:      true,
			StatusCode:  http.StatusInternalServerError,
			Error:       err.Error(),
			RequestBody: requestJSON,
			LatencyMs:   time.Since(started).Milliseconds(),
		})
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	agg := streamAggregator{model: selected.model}
	for chunk := range chunks {
		agg.consume(chunk)
		data, err := json.Marshal(chunk)
		if err != nil {
			continue
		}
		fmt.Fprintf(c.Writer, "data: %s\n\n", data)
		flusher.Flush()
	}

	fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
	flusher.Flush()

	resp := agg.toResponse()
	respJSON, _ := json.Marshal(resp)
	saveGatewayRequestLog(database, &gatewaylog.Entry{
		UserID:           accountID,
		ChannelID:        selected.channel.ID,
		ChannelName:      selected.channel.Name,
		Model:            selected.model,
		Stream:           true,
		StatusCode:       http.StatusOK,
		RequestBody:      requestJSON,
		ResponseBody:     string(respJSON),
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		CachedTokens:     resp.Usage.CachedTokens,
		LatencyMs:        time.Since(started).Milliseconds(),
	})
	logUsage(database, accountID, selected.channel, selected.model, resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.CachedTokens)
}

// streamAggregator rebuilds an OpenAI-compatible completion from SSE chunks for audit logs.
type streamAggregator struct {
	id           string
	model        string
	role         string
	content      strings.Builder
	toolCalls    map[int]*provider.ToolCall
	finishReason string
	usage        provider.Usage
}

func (a *streamAggregator) consume(chunk *provider.ChatCompletionChunk) {
	if chunk == nil {
		return
	}
	if chunk.ID != "" {
		a.id = chunk.ID
	}
	if chunk.Model != "" {
		a.model = chunk.Model
	}
	if chunk.Usage != nil {
		a.usage.Add(*chunk.Usage)
	}
	if len(chunk.Choices) == 0 {
		return
	}
	ch := chunk.Choices[0]
	if ch.FinishReason != nil && *ch.FinishReason != "" {
		a.finishReason = *ch.FinishReason
	}
	if ch.Delta.Role != "" {
		a.role = ch.Delta.Role
	}
	if ch.Delta.Content != "" {
		a.content.WriteString(ch.Delta.Content)
	} else if ch.Delta.ReasoningContent != "" {
		a.content.WriteString(ch.Delta.ReasoningContent)
	}
	for _, tc := range ch.Delta.ToolCalls {
		idx := 0
		if tc.Index != nil {
			idx = *tc.Index
		}
		if a.toolCalls == nil {
			a.toolCalls = map[int]*provider.ToolCall{}
		}
		cur, ok := a.toolCalls[idx]
		if !ok {
			cp := tc
			cp.Index = nil
			a.toolCalls[idx] = &cp
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
		if tc.Function.Arguments != "" {
			cur.Function.Arguments += tc.Function.Arguments
		}
	}
}

func (a *streamAggregator) toResponse() *provider.ChatCompletionResponse {
	msg := provider.Message{Role: a.role, Content: a.content.String()}
	if msg.Role == "" {
		msg.Role = "assistant"
	}
	if len(a.toolCalls) > 0 {
		keys := make([]int, 0, len(a.toolCalls))
		for k := range a.toolCalls {
			keys = append(keys, k)
		}
		sort.Ints(keys)
		for _, k := range keys {
			msg.ToolCalls = append(msg.ToolCalls, *a.toolCalls[k])
		}
	}
	finish := a.finishReason
	if finish == "" {
		finish = "stop"
	}
	a.usage.Normalize()
	return &provider.ChatCompletionResponse{
		ID:      a.id,
		Object:  "chat.completion",
		Model:   a.model,
		Choices: []provider.Choice{{Index: 0, Message: msg, FinishReason: finish}},
		Usage:   a.usage,
	}
}

// ========== Agent Chat Handler ==========

func handleAgentChat(database *db.DB, cfg *config.Config, workspaceMgr *workspace.Manager, mem *memory.MemoryService, tagSvc *tags.Service, rt *sessionRunRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}
		if rt == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "session run runtime unavailable"})
			return
		}

		var req struct {
			Message     string `json:"message" binding:"required"`
			SessionID   string `json:"session_id"`
			Mode        string `json:"mode"`
			Model       string `json:"model"`
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
			_, _ = database.Exec(`
				INSERT INTO sessions (id, user_id, title, platform, message_count, created_at, updated_at)
				VALUES (?, ?, ?, ?, 0, ?, ?)
			`, sessionID, accountID, req.Message[:min(50, len(req.Message))], platform, time.Now(), time.Now())
		} else if !sessionOwnedBy(database, sessionID, accountID) {
			c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
			return
		}

		userMsgID := uuid.New().String()
		_, _ = database.Exec(`
			INSERT INTO messages (id, session_id, role, content, created_at)
			VALUES (?, ?, 'user', ?, ?)
		`, userMsgID, sessionID, req.Message, time.Now())
		var questionTags []tags.TagHit
		if tagSvc != nil {
			if hits, err := tagSvc.TagMessage(accountID, userMsgID, req.Message); err != nil {
				log.Printf("[tags] tag failed: %v", err)
			} else {
				questionTags = hits
			}
		}

		modelName := cfg.Agent.DefaultModel
		if req.Model != "" {
			modelName = req.Model
		}

		status := "running"
		var run *sessionrun.Run
		active, err := rt.store.ActiveRunForSession(sessionID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if active != nil {
			if _, err := rt.store.EnqueueInbox(sessionID, active.ID, userMsgID, req.Message); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			run = active
			status = "accepted_queued"
			log.Printf("[chat/agent] queued inbox session=%s run=%s", sessionID, run.ID)
		} else {
			run, err = rt.store.CreateQueued(sessionrun.CreateRunInput{
				SessionID:        sessionID,
				UserID:           accountID,
				WorkspaceID:      req.WorkspaceID,
				Mode:             mode,
				Model:            modelName,
				TriggerMessageID: userMsgID,
			})
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			status = "queued"
			log.Printf("[chat/agent] queued run session=%s run=%s mode=%s model=%s workspace=%s",
				sessionID, run.ID, mode, modelName, req.WorkspaceID)
		}

		if req.Stream {
			after := int64(0)
			if status == "accepted_queued" {
				after = run.LastSeq
			}
			streamSessionRunEvents(c, rt, accountID, run.ID, after)
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"session_id": sessionID,
			"run_id":     run.ID,
			"status":     status,
			"tags":       questionTags,
		})
	}
}

func handleSessionRunEvents(rt *sessionRunRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}
		afterSeq, _ := strconv.ParseInt(c.Query("after_seq"), 10, 64)
		streamSessionRunEvents(c, rt, accountID, c.Param("id"), afterSeq)
	}
}

func streamSessionRunEvents(c *gin.Context, rt *sessionRunRuntime, accountID int64, runID string, afterSeq int64) {
	if rt == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "session run runtime unavailable"})
		return
	}
	run, err := rt.store.Get(accountID, runID)
	if errors.Is(err, sessionrun.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "run not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	writeEv := func(ev sessionrun.Event) {
		agentEv := eventPayloadToAgentEvent(ev)
		agentEv.Type = string(ev.Type)
		if agentEv.Session == "" {
			agentEv.Session = run.SessionID
		}
		data, err := json.Marshal(agentEv)
		if err != nil {
			return
		}
		fmt.Fprintf(c.Writer, "data: %s\n\n", data)
		flusher.Flush()
	}

	replay, err := rt.store.ListEventsAfter(runID, afterSeq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	last := afterSeq
	for _, ev := range replay {
		writeEv(ev)
		last = ev.Seq
		if ev.Type == sessionrun.EventDone || ev.Type == sessionrun.EventError {
			fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
			flusher.Flush()
			return
		}
	}

	// Refresh terminal status after replay.
	if cur, _ := rt.store.GetByID(runID); cur != nil {
		switch cur.Status {
		case sessionrun.StatusSucceeded, sessionrun.StatusFailed, sessionrun.StatusCancelled:
			fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
			flusher.Flush()
			return
		}
	}

	ch := rt.hub.Subscribe(runID)
	defer rt.hub.Unsubscribe(runID, ch)

	notify := c.Request.Context().Done()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-notify:
			return
		case <-ticker.C:
			fmt.Fprintf(c.Writer, ": ping\n\n")
			flusher.Flush()
			if cur, _ := rt.store.GetByID(runID); cur != nil {
				switch cur.Status {
				case sessionrun.StatusSucceeded, sessionrun.StatusFailed, sessionrun.StatusCancelled:
					// Drain any remaining events then exit.
					more, _ := rt.store.ListEventsAfter(runID, last)
					for _, ev := range more {
						writeEv(ev)
						last = ev.Seq
					}
					fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
					flusher.Flush()
					return
				}
			}
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if ev.Seq <= last {
				continue
			}
			writeEv(ev)
			last = ev.Seq
			if ev.Type == sessionrun.EventDone || ev.Type == sessionrun.EventError {
				fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
				flusher.Flush()
				return
			}
		}
	}
}

func handleCancelSessionRun(rt *sessionRunRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}
		run, err := rt.store.RequestCancel(accountID, c.Param("id"))
		if errors.Is(err, sessionrun.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "run not found"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"run": run})
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

func handleGetSession(database *db.DB, rt *sessionRunRuntime) gin.HandlerFunc {
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

		resp := gin.H{
			"session":  session,
			"messages": messages,
		}
		if rt != nil {
			if active, err := rt.store.ActiveRunForSession(id); err == nil && active != nil {
				resp["active_run"] = active
				resp["last_event_seq"] = active.LastSeq
				if steps, err := rt.store.CollectToolSteps(active.ID); err == nil && len(steps) > 0 {
					resp["active_run_tool_steps"] = steps
				}
			} else if latest, err := rt.store.LatestRunForSession(id); err == nil && latest != nil {
				resp["latest_run"] = latest
				if steps, err := rt.store.CollectToolSteps(latest.ID); err == nil && len(steps) > 0 {
					resp["latest_run_tool_steps"] = steps
				}
			}
		}
		c.JSON(http.StatusOK, resp)
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
		SELECT id, user_id, name, type, key, COALESCE(base_url, ''), COALESCE(models, ''), weight, priority, status, COALESCE(is_default, 0), COALESCE(auth_mode, 'api_key')
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
		var userID int64
		if err := rows.Scan(&ch.ID, &userID, &ch.Name, &ch.Type, &ch.Key, &ch.BaseURL, &ch.Models, &ch.Weight, &ch.Priority, &ch.Status, &ch.IsDefault, &ch.AuthMode); err != nil {
			continue
		}
		ch.UserID = &userID

		if ch.Models == "" {
			chCopy := ch
			matches = append(matches, &chCopy)
			continue
		}

		for _, m := range parseModelsJSON(ch.Models) {
			if m == modelName || provider.NormalizeModelAlias(m) == normalizedRequest {
				chCopy := ch
				matches = append(matches, &chCopy)
				break
			}
		}
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("no channel found for model: %s", modelName)
	}

	// Explicit default channel always wins
	for _, ch := range matches {
		if ch.IsDefault == 1 {
			return ch, nil
		}
	}

	return matches[0], nil
}

func createProviderFromChannel(channel *model.Channel) (provider.Provider, error) {
	providerCfg := &provider.ProviderConfig{
		Name:     channel.Name,
		Type:     getProviderType(channel.Type),
		BaseURL:  channel.BaseURL,
		APIKey:   channel.Key,
		AuthMode: channel.AuthMode,
	}
	if providerCfg.BaseURL == "" {
		providerCfg.BaseURL = getDefaultBaseURL(channel.Type)
	}
	if strings.EqualFold(channel.AuthMode, "oauth") && channel.Type == model.ChannelTypeClaude {
		if claudeOAuthSvc == nil || !claudeOAuthSvc.Configured() {
			return nil, fmt.Errorf("claude oauth is not enabled")
		}
		accountID := int64(0)
		if channel.UserID != nil {
			accountID = *channel.UserID
		}
		creds, err := claudeOAuthSvc.ValidCreds(context.Background(), accountID)
		if err != nil {
			return nil, fmt.Errorf("claude oauth token: %w", err)
		}
		providerCfg.APIKey = creds.AccessToken
		providerCfg.AuthMode = "oauth"
		providerCfg.ClaudeDeviceID = creds.DeviceID
		providerCfg.ClaudeAccountUUID = creds.AccountUUID
	}
	return provider.NewProvider(providerCfg)
}

func resolveModelForChannel(channel *model.Channel, modelName string) string {
	if modelName == "" {
		if models := parseModelsJSON(channel.Models); len(models) > 0 {
			return models[0]
		}
	}
	return modelName
}

func buildAgentMessages(channel *model.Channel, modelName, userMessage, mode string) []provider.Message {
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
	var userID int64
	err := database.QueryRow(`
		SELECT id, user_id, name, type, key, COALESCE(base_url, ''), COALESCE(models, ''), weight, priority, status,
		       COALESCE(is_default, 0), COALESCE(auth_mode, 'api_key')
		FROM channels WHERE status = 1 AND user_id = ? ORDER BY is_default DESC, priority DESC, weight DESC LIMIT 1
	`, accountID).Scan(
		&ch.ID, &userID, &ch.Name, &ch.Type, &ch.Key, &ch.BaseURL, &ch.Models, &ch.Weight, &ch.Priority, &ch.Status,
		&ch.IsDefault, &ch.AuthMode,
	)
	if err != nil {
		return nil, err
	}
	ch.UserID = &userID
	return &ch, nil
}

func getProviderType(channelType int) provider.ProviderType {
	switch channelType {
	case model.ChannelTypeOpenAI:
		return provider.ProviderTypeOpenAI
	case model.ChannelTypeClaude:
		return provider.ProviderTypeClaude
	case model.ChannelTypeGemini:
		return provider.ProviderTypeGemini
	case model.ChannelTypeDeepSeek:
		return provider.ProviderTypeDeepSeek
	case model.ChannelTypeOllama:
		return provider.ProviderTypeOllama
	case model.ChannelTypeMiMo:
		return provider.ProviderTypeMiMo
	case model.ChannelTypeAgnes:
		return provider.ProviderTypeAgnes
	case model.ChannelTypeGLM:
		return provider.ProviderTypeGLM
	case model.ChannelTypeCustom:
		return provider.ProviderTypeCustom
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
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}

		rows, err := database.Query(`
			SELECT id, user_id, name, key, status, expired_at, remain_quota, unlimited_quota,
			       COALESCE(model_limits, ''), created_at
			FROM tokens WHERE user_id = ? ORDER BY id DESC
		`, accountID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query tokens"})
			return
		}
		defer rows.Close()

		tokens := make([]model.Token, 0)
		for rows.Next() {
			var t model.Token
			if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.Key, &t.Status, &t.ExpiredAt, &t.RemainQuota, &t.UnlimitedQuota, &t.ModelLimits, &t.CreatedAt); err != nil {
				continue
			}
			// Mask key for list view; the raw key is only returned once on creation.
			t.Key = maskKey(t.Key)
			tokens = append(tokens, t)
		}

		c.JSON(http.StatusOK, gin.H{"tokens": tokens})
	}
}

func handleCreateToken(database *db.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}

		var req struct {
			Name string `json:"name"`
		}
		_ = c.ShouldBindJSON(&req)

		name := strings.TrimSpace(req.Name)
		if name == "" {
			// Default name follows a sequential order 1, 2, 3... per user.
			var maxName int
			database.QueryRow(`
				SELECT COALESCE(MAX(CAST(name AS INTEGER)), 0)
				FROM tokens WHERE user_id = ? AND name GLOB '[0-9]*'
			`, accountID).Scan(&maxName)
			name = strconv.Itoa(maxName + 1)
		}

		key, err := generateAPIKey()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate api key"})
			return
		}

		now := time.Now()
		result, err := database.Exec(`
			INSERT INTO tokens (user_id, name, key, status, remain_quota, unlimited_quota, created_at)
			VALUES (?, ?, ?, 1, -1, 1, ?)
		`, accountID, name, key, now)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create token"})
			return
		}

		id, _ := result.LastInsertId()
		c.JSON(http.StatusOK, gin.H{"message": "token created", "id": id, "key": key})
	}
}

func generateAPIKey() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "sk-" + hex.EncodeToString(b), nil
}

func handleDeleteToken(database *db.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}

		tokenID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid token id"})
			return
		}

		var owner int64
		database.QueryRow("SELECT user_id FROM tokens WHERE id = ?", tokenID).Scan(&owner)
		if owner != accountID {
			c.JSON(http.StatusNotFound, gin.H{"error": "token not found"})
			return
		}

		result, err := database.Exec("DELETE FROM tokens WHERE id = ? AND user_id = ?", tokenID, accountID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete token"})
			return
		}
		if affected, _ := result.RowsAffected(); affected == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "token not found"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "token deleted"})
	}
}

func handleUpdateToken(database *db.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}

		tokenID, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid token id"})
			return
		}

		var req struct {
			Status *int   `json:"status"`
			Name   *string `json:"name"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		var owner int64
		database.QueryRow("SELECT user_id FROM tokens WHERE id = ?", tokenID).Scan(&owner)
		if owner != accountID {
			c.JSON(http.StatusNotFound, gin.H{"error": "token not found"})
			return
		}

		query := "UPDATE tokens SET"
		args := []interface{}{}
		sep := " "
		if req.Status != nil {
			query += sep + "status = ?"
			args = append(args, *req.Status)
			sep = ", "
		}
		if req.Name != nil && strings.TrimSpace(*req.Name) != "" {
			query += sep + "name = ?"
			args = append(args, strings.TrimSpace(*req.Name))
			sep = ", "
		}
		if len(args) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "nothing to update"})
			return
		}
		query += " WHERE id = ? AND user_id = ?"
		args = append(args, tokenID, accountID)

		if _, err := database.Exec(query, args...); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update token"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "token updated"})
	}
}

// ========== Message Processing ==========

func processMessage(database *db.DB, cfg *config.Config, sessionID string, message string, accountID int64, tagSvc *tags.Service) string {
	modelName := cfg.Agent.DefaultModel
	var channel *model.Channel
	var err error
	if modelName != "" {
		channel, err = findChannelForModel(database, accountID, modelName)
	}
	if channel == nil || err != nil {
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
	saveMessage(database, accountID, sessionID, "user", message, "", "", 0, tagSvc)
	saveMessage(database, accountID, sessionID, "assistant", responseContent, modelName, channel.Name, resp.Usage.TotalTokens, tagSvc)

	return responseContent
}

func saveMessage(database *db.DB, accountID int64, sessionID, role, content, model, provider string, tokens int, tagSvc *tags.Service) string {
	// Ensure session exists for this account
	var count int
	database.QueryRow("SELECT COUNT(*) FROM sessions WHERE id = ?", sessionID).Scan(&count)
	if count == 0 {
		database.Exec(`
			INSERT INTO sessions (id, user_id, title, platform, message_count, created_at, updated_at)
			VALUES (?, ?, ?, 'web', 0, ?, ?)
		`, sessionID, accountID, content[:min(50, len(content))], time.Now(), time.Now())
	}

	msgID := uuid.New().String()
	database.Exec(`
		INSERT INTO messages (id, session_id, role, content, model, provider, tokens, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, msgID, sessionID, role, content, model, provider, tokens, time.Now())

	database.Exec(`
		UPDATE sessions SET message_count = message_count + 1, updated_at = ? WHERE id = ?
	`, time.Now(), sessionID)

	if role == "user" && tagSvc != nil && strings.TrimSpace(content) != "" {
		if _, err := tagSvc.TagMessage(accountID, msgID, content); err != nil {
			log.Printf("[tags] failed to tag message %s: %v", msgID, err)
		}
	}
	return msgID
}

// ========== Gateway Endpoint Handlers ==========

// (endpoint management now uses the tokens table for API keys;
// the gateway URL is derived from the running server address)
