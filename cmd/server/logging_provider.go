package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/alex/codegateway/internal/db"
	"github.com/alex/codegateway/internal/gatewaylog"
	"github.com/alex/codegateway/internal/model"
	"github.com/alex/codegateway/internal/provider"
)

// loggingProvider wraps a Provider and writes each LLM call into gateway_request_logs.
type loggingProvider struct {
	inner       provider.Provider
	database    *db.DB
	userID      int64
	channelID   int64
	channelName string
}

func wrapProviderWithRequestLog(database *db.DB, userID int64, channel *model.Channel, prov provider.Provider) provider.Provider {
	if database == nil || prov == nil || channel == nil || userID <= 0 {
		return prov
	}
	return &loggingProvider{
		inner:       prov,
		database:    database,
		userID:      userID,
		channelID:   channel.ID,
		channelName: channel.Name,
	}
}

func (p *loggingProvider) Name() string { return p.inner.Name() }

func (p *loggingProvider) ListModels(ctx context.Context) ([]string, error) {
	return p.inner.ListModels(ctx)
}

func (p *loggingProvider) ValidateModel(modelName string) bool {
	return p.inner.ValidateModel(modelName)
}

func (p *loggingProvider) ChatCompletion(ctx context.Context, req *provider.ChatCompletionRequest) (*provider.ChatCompletionResponse, error) {
	started := time.Now()
	requestJSON := gatewaylog.MarshalRequestJSON(req)
	modelName := ""
	if req != nil {
		modelName = req.Model
	}

	resp, err := p.inner.ChatCompletion(ctx, req)
	latency := time.Since(started).Milliseconds()
	if err != nil {
		saveGatewayRequestLog(p.database, &gatewaylog.Entry{
			UserID:      p.userID,
			ChannelID:   p.channelID,
			ChannelName: p.channelName,
			Model:       modelName,
			Stream:      false,
			StatusCode:  http.StatusInternalServerError,
			Error:       err.Error(),
			RequestBody: requestJSON,
			LatencyMs:   latency,
		})
		return nil, err
	}

	if resp != nil {
		resp.Usage.Normalize()
		if resp.Model != "" {
			modelName = resp.Model
		}
	}
	respJSON, _ := json.Marshal(resp)
	entry := &gatewaylog.Entry{
		UserID:       p.userID,
		ChannelID:    p.channelID,
		ChannelName:  p.channelName,
		Model:        modelName,
		Stream:       false,
		StatusCode:   http.StatusOK,
		RequestBody:  requestJSON,
		ResponseBody: string(respJSON),
		LatencyMs:    latency,
	}
	if resp != nil {
		entry.PromptTokens = resp.Usage.PromptTokens
		entry.CompletionTokens = resp.Usage.CompletionTokens
		entry.CachedTokens = resp.Usage.CachedTokens
	}
	saveGatewayRequestLog(p.database, entry)
	return resp, nil
}

func (p *loggingProvider) ChatCompletionStream(ctx context.Context, req *provider.ChatCompletionRequest) (<-chan *provider.ChatCompletionChunk, error) {
	started := time.Now()
	requestJSON := gatewaylog.MarshalRequestJSON(req)
	modelName := ""
	if req != nil {
		modelName = req.Model
	}

	chunks, err := p.inner.ChatCompletionStream(ctx, req)
	if err != nil {
		saveGatewayRequestLog(p.database, &gatewaylog.Entry{
			UserID:      p.userID,
			ChannelID:   p.channelID,
			ChannelName: p.channelName,
			Model:       modelName,
			Stream:      true,
			StatusCode:  http.StatusInternalServerError,
			Error:       err.Error(),
			RequestBody: requestJSON,
			LatencyMs:   time.Since(started).Milliseconds(),
		})
		return nil, err
	}

	out := make(chan *provider.ChatCompletionChunk, 64)
	go func() {
		defer close(out)
		agg := streamAggregator{model: modelName}
		for chunk := range chunks {
			agg.consume(chunk)
			select {
			case out <- chunk:
			case <-ctx.Done():
				saveGatewayRequestLog(p.database, &gatewaylog.Entry{
					UserID:      p.userID,
					ChannelID:   p.channelID,
					ChannelName: p.channelName,
					Model:       modelName,
					Stream:      true,
					StatusCode:  http.StatusRequestTimeout,
					Error:       ctx.Err().Error(),
					RequestBody: requestJSON,
					LatencyMs:   time.Since(started).Milliseconds(),
				})
				return
			}
		}
		resp := agg.toResponse()
		if resp.Model != "" {
			modelName = resp.Model
		}
		respJSON, _ := json.Marshal(resp)
		saveGatewayRequestLog(p.database, &gatewaylog.Entry{
			UserID:           p.userID,
			ChannelID:        p.channelID,
			ChannelName:      p.channelName,
			Model:            modelName,
			Stream:           true,
			StatusCode:       http.StatusOK,
			RequestBody:      requestJSON,
			ResponseBody:     string(respJSON),
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			CachedTokens:     resp.Usage.CachedTokens,
			LatencyMs:        time.Since(started).Milliseconds(),
		})
	}()
	return out, nil
}
