package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/alex/codegateway/internal/db"
	"github.com/alex/codegateway/internal/model"
	"github.com/alex/codegateway/internal/provider"
	"github.com/gin-gonic/gin"
)

// openaiModel is the OpenAI-compatible model object.
// https://platform.openai.com/docs/api-reference/models/object
type openaiModel struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

type openaiModelList struct {
	Object string        `json:"object"`
	Data   []openaiModel `json:"data"`
}

func handleListModels(database *db.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}

		models, err := collectAvailableModels(c.Request.Context(), database, accountID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, openaiAPIError(
				"failed to list models",
				"server_error",
				"",
				"models_list_failed",
			))
			return
		}

		c.JSON(http.StatusOK, openaiModelList{
			Object: "list",
			Data:   models,
		})
	}
}

// handleRetrieveModel implements OpenAI GET /v1/models/{model}.
func handleRetrieveModel(database *db.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}

		modelID := strings.TrimPrefix(c.Param("model"), "/")
		modelID = strings.TrimSpace(modelID)
		if modelID == "" {
			c.JSON(http.StatusBadRequest, openaiAPIError(
				"model id is required",
				"invalid_request_error",
				"model",
				"invalid_model",
			))
			return
		}

		models, err := collectAvailableModels(c.Request.Context(), database, accountID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, openaiAPIError(
				"failed to list models",
				"server_error",
				"",
				"models_list_failed",
			))
			return
		}

		for _, m := range models {
			if strings.EqualFold(m.ID, modelID) {
				c.JSON(http.StatusOK, m)
				return
			}
		}

		c.JSON(http.StatusNotFound, openaiAPIError(
			"The model '"+modelID+"' does not exist",
			"invalid_request_error",
			"model",
			"model_not_found",
		))
	}
}

func openaiAPIError(message, errType, param, code string) gin.H {
	errObj := gin.H{
		"message": message,
		"type":    errType,
		"code":    code,
	}
	if param != "" {
		errObj["param"] = param
	} else {
		errObj["param"] = nil
	}
	return gin.H{"error": errObj}
}

func collectAvailableModels(ctx context.Context, database *db.DB, accountID int64) ([]openaiModel, error) {
	rows, err := database.Query(`
		SELECT id, name, type, key, base_url, models, created_at
		FROM channels
		WHERE status = 1 AND user_id = ?
		ORDER BY priority DESC, weight DESC, id ASC
	`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	seen := make(map[string]bool)
	out := make([]openaiModel, 0)

	addModel := func(id, ownedBy string, created int64) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		if created <= 0 {
			created = time.Now().Unix()
		}
		if ownedBy == "" {
			ownedBy = "codegateway"
		}
		out = append(out, openaiModel{
			ID:      id,
			Object:  "model",
			Created: created,
			OwnedBy: ownedBy,
		})
	}

	for rows.Next() {
		var (
			ch          model.Channel
			createdAt   time.Time
			modelsJSON  string
			baseURL     string
			key         string
		)
		if err := rows.Scan(&ch.ID, &ch.Name, &ch.Type, &key, &baseURL, &modelsJSON, &createdAt); err != nil {
			continue
		}
		ch.Key = key
		ch.BaseURL = baseURL
		ch.Models = modelsJSON
		ch.CreatedAt = createdAt

		ownedBy := ownedByForChannelType(ch.Type)
		created := createdAt.Unix()

		for _, id := range modelsForChannel(ctx, &ch) {
			addModel(id, ownedBy, created)
		}
	}

	return out, nil
}

func modelsForChannel(ctx context.Context, ch *model.Channel) []string {
	switch ch.Type {
	case model.ChannelTypeMiMoFree:
		return provider.MiMoFreeAdvertisedModels()
	}

	if ids := parseModelsJSON(ch.Models); len(ids) > 0 {
		return ids
	}

	// Fall back to upstream OpenAI-compatible /models when channel has no explicit list.
	if !supportsUpstreamModelList(ch.Type) || strings.TrimSpace(ch.Key) == "" {
		return nil
	}

	prov, err := createProviderFromChannel(ch)
	if err != nil {
		return nil
	}

	listCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	ids, err := prov.ListModels(listCtx)
	if err != nil {
		return nil
	}
	return ids
}

func parseModelsJSON(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	var ids []string
	if err := json.Unmarshal([]byte(raw), &ids); err == nil {
		return sanitizeModelIDs(ids)
	}

	// Tolerate a single model string stored without JSON array wrapping.
	if !strings.HasPrefix(raw, "[") {
		return sanitizeModelIDs([]string{raw})
	}
	return nil
}

func sanitizeModelIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" {
			out = append(out, id)
		}
	}
	return out
}

func supportsUpstreamModelList(channelType int) bool {
	switch channelType {
	case model.ChannelTypeOpenAI, model.ChannelTypeDeepSeek, model.ChannelTypeMiMo, model.ChannelTypeOllama,
		model.ChannelTypeAgnes, model.ChannelTypeGLM:
		return true
	default:
		return false
	}
}

func ownedByForChannelType(channelType int) string {
	switch channelType {
	case model.ChannelTypeOpenAI:
		return "openai"
	case model.ChannelTypeClaude:
		return "anthropic"
	case model.ChannelTypeGemini:
		return "google"
	case model.ChannelTypeDeepSeek:
		return "deepseek"
	case model.ChannelTypeOllama:
		return "ollama"
	case model.ChannelTypeMiMo, model.ChannelTypeMiMoFree:
		return "mimo"
	case model.ChannelTypeAgnes:
		return "agnes"
	case model.ChannelTypeGLM:
		return "zhipu"
	case model.ChannelTypeCustom:
		return "custom"
	default:
		return "codegateway"
	}
}
