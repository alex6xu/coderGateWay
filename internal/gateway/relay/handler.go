package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/alex/codegateway/internal/gateway/billing"
	"github.com/alex/codegateway/internal/gateway/proxy"
	"github.com/alex/codegateway/internal/gateway/relay/convert"
	"github.com/alex/codegateway/internal/gateway/relay/router"
	"github.com/alex/codegateway/internal/model"
	"github.com/alex/codegateway/internal/provider"
	"github.com/gin-gonic/gin"
)

// RelayHandler handles API relay requests
type RelayHandler struct {
	providerRegistry *provider.Registry
	channelRepo      *ChannelRepository
	router           *router.SmartRouter
	converter        *convert.FormatConverter
	billing          *billing.BillingService
	freeProxy        *proxy.FreeProxy
}

// NewRelayHandler creates a new relay handler
func NewRelayHandler(
	providerRegistry *provider.Registry,
	channelRepo *ChannelRepository,
	router *router.SmartRouter,
	billing *billing.BillingService,
	freeProxy *proxy.FreeProxy,
) *RelayHandler {
	return &RelayHandler{
		providerRegistry: providerRegistry,
		channelRepo:      channelRepo,
		router:           router,
		converter:        convert.NewFormatConverter(),
		billing:          billing,
		freeProxy:        freeProxy,
	}
}

// HandleChatCompletions handles OpenAI-compatible chat completion requests
func (h *RelayHandler) HandleChatCompletions(c *gin.Context) {
	var req provider.ChatCompletionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	// Try to find a channel first
	channel, err := h.channelRepo.GetBestChannel(req.Model)
	if err != nil {
		// If no channel found, try free proxy
		if h.freeProxy != nil {
			h.handleFreeProxy(c, &req)
			return
		}
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no available channel"})
		return
	}

	// Get provider for this channel
	prov, err := h.providerRegistry.Get(channel.Name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "provider not found"})
		return
	}

	// Handle streaming
	if req.Stream {
		h.handleStream(c, prov, &req)
		return
	}

	// Handle non-streaming
	start := time.Now()
	resp, err := prov.ChatCompletion(c.Request.Context(), &req)
	latency := time.Since(start)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Calculate cost
	_ = h.billing.CalculateCost(req.Model, resp.Usage.PromptTokens, resp.Usage.CompletionTokens)

	// Update channel stats
	h.router.UpdateChannelHealth(int(channel.ID), true, latency)

	c.JSON(http.StatusOK, resp)
}

// HandleClaudeMessages handles Claude-compatible message requests
func (h *RelayHandler) HandleClaudeMessages(c *gin.Context) {
	// Parse Claude format request
	var claudeReq struct {
		Model     string `json:"model"`
		Messages  []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
		MaxTokens  int     `json:"max_tokens"`
		Temperature float64 `json:"temperature"`
	}

	if err := c.ShouldBindJSON(&claudeReq); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	// Convert to OpenAI format
	openaiReq := &provider.ChatCompletionRequest{
		Model: claudeReq.Model,
		Temperature: &claudeReq.Temperature,
		MaxTokens:   &claudeReq.MaxTokens,
	}

	for _, msg := range claudeReq.Messages {
		openaiReq.Messages = append(openaiReq.Messages, provider.Message{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	// Find channel and process
	channel, err := h.channelRepo.GetBestChannel(openaiReq.Model)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no available channel"})
		return
	}

	prov, err := h.providerRegistry.Get(channel.Name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "provider not found"})
		return
	}

	// Convert response back to Claude format
	resp, err := prov.ChatCompletion(c.Request.Context(), openaiReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Convert to Claude response format
	claudeResp := map[string]interface{}{
		"id":   resp.ID,
		"type": "message",
		"role": "assistant",
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": resp.Choices[0].Message.Content,
			},
		},
		"model":      resp.Model,
		"stop_reason": resp.Choices[0].FinishReason,
		"usage": map[string]interface{}{
			"input_tokens":  resp.Usage.PromptTokens,
			"output_tokens": resp.Usage.CompletionTokens,
		},
	}

	c.JSON(http.StatusOK, claudeResp)
}

// HandleGemini handles Gemini-compatible requests
func (h *RelayHandler) HandleGemini(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "gemini format not implemented yet"})
}

func (h *RelayHandler) handleStream(c *gin.Context, prov provider.Provider, req *provider.ChatCompletionRequest) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Minute)
	defer cancel()

	chunks, err := prov.ChatCompletionStream(ctx, req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
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

func (h *RelayHandler) handleFreeProxy(c *gin.Context, req *provider.ChatCompletionRequest) {
	if h.freeProxy == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no free proxy available"})
		return
	}

	proxy, err := h.freeProxy.GetProxy(req.Model)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}

	// Update stats
	proxy.Stats.TotalRequests++

	// Serve through proxy
	h.freeProxy.ServeHTTP(c.Writer, c.Request, req.Model)
}

// ChannelRepository manages channels
type ChannelRepository struct {
	channels []*model.Channel
}

// NewChannelRepository creates a new channel repository
func NewChannelRepository() *ChannelRepository {
	return &ChannelRepository{
		channels: make([]*model.Channel, 0),
	}
}

// AddChannel adds a channel
func (r *ChannelRepository) AddChannel(channel *model.Channel) {
	r.channels = append(r.channels, channel)
}

// GetBestChannel returns the best channel for a model
func (r *ChannelRepository) GetBestChannel(modelName string) (*model.Channel, error) {
	var bestChannel *model.Channel
	bestScore := -1

	for _, channel := range r.channels {
		if channel.Status != 1 {
			continue
		}

		if !r.supportsModel(channel, modelName) {
			continue
		}

		score := channel.Weight*10 + channel.Priority
		if score > bestScore {
			bestScore = score
			bestChannel = channel
		}
	}

	if bestChannel == nil {
		return nil, fmt.Errorf("no available channel for model: %s", modelName)
	}

	return bestChannel, nil
}

func (r *ChannelRepository) supportsModel(channel *model.Channel, modelName string) bool {
	if channel.Models == "" {
		return true
	}

	var models []string
	if err := json.Unmarshal([]byte(channel.Models), &models); err != nil {
		return false
	}

	for _, m := range models {
		if strings.EqualFold(m, modelName) {
			return true
		}
	}

	return false
}
