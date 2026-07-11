package router

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/alex/codegateway/internal/model"
)

// Strategy represents a routing strategy
type Strategy string

const (
	StrategyAuto    Strategy = "auto"
	StrategyCost    Strategy = "cost"
	StrategyLatency Strategy = "latency"
	StrategyQuality Strategy = "quality"
)

// PromptType represents the type of prompt
type PromptType string

const (
	PromptTypeCode     PromptType = "code"
	PromptTypeChat     PromptType = "chat"
	PromptTypeReason   PromptType = "reasoning"
	PromptTypeTranslate PromptType = "translate"
	PromptTypeGeneral  PromptType = "general"
)

// SmartRouter handles intelligent routing
type SmartRouter struct {
	channels  []*ChannelState
	strategy  Strategy
	mu        sync.RWMutex
}

// ChannelState represents the state of a channel
type ChannelState struct {
	Channel      *model.Channel
	Healthy      bool
	Latency      time.Duration
	SuccessRate  float64
	LastChecked  time.Time
	TotalRequests int64
	FailedRequests int64
}

// NewSmartRouter creates a new smart router
func NewSmartRouter(strategy Strategy) *SmartRouter {
	return &SmartRouter{
		channels: make([]*ChannelState, 0),
		strategy: strategy,
	}
}

// AddChannel adds a channel to the router
func (r *SmartRouter) AddChannel(channel *model.Channel) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.channels = append(r.channels, &ChannelState{
		Channel: channel,
		Healthy: true,
	})
}

// SelectChannel selects the best channel for a model
func (r *SmartRouter) SelectChannel(modelName string) (*model.Channel, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Filter channels that support this model
	candidates := r.filterChannels(modelName)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no available channel for model: %s", modelName)
	}

	// Apply strategy
	switch r.strategy {
	case StrategyCost:
		return r.selectByCost(candidates), nil
	case StrategyLatency:
		return r.selectByLatency(candidates), nil
	case StrategyQuality:
		return r.selectByQuality(candidates), nil
	case StrategyAuto:
		return r.selectAuto(modelName, candidates), nil
	default:
		return r.selectByWeight(candidates), nil
	}
}

// filterChannels filters channels that support the model
func (r *SmartRouter) filterChannels(modelName string) []*ChannelState {
	candidates := make([]*ChannelState, 0)

	for _, cs := range r.channels {
		if !cs.Healthy {
			continue
		}
		if cs.Channel.Status != 1 {
			continue
		}
		if r.supportsModel(cs.Channel, modelName) {
			candidates = append(candidates, cs)
		}
	}

	return candidates
}

// supportsModel checks if a channel supports a model
func (r *SmartRouter) supportsModel(channel *model.Channel, modelName string) bool {
	if channel.Models == "" {
		return true
	}

	// Parse models list
	models := strings.Split(channel.Models, ",")
	for _, m := range models {
		if strings.EqualFold(strings.TrimSpace(m), modelName) {
			return true
		}
	}

	return false
}

// selectByCost selects the channel with lowest cost
func (r *SmartRouter) selectByCost(candidates []*ChannelState) *model.Channel {
	if len(candidates) == 0 {
		return nil
	}

	// For now, select by priority (lower priority = lower cost)
	best := candidates[0]
	for _, cs := range candidates[1:] {
		if cs.Channel.Priority < best.Channel.Priority {
			best = cs
		}
	}

	return best.Channel
}

// selectByLatency selects the channel with lowest latency
func (r *SmartRouter) selectByLatency(candidates []*ChannelState) *model.Channel {
	if len(candidates) == 0 {
		return nil
	}

	best := candidates[0]
	for _, cs := range candidates[1:] {
		if cs.Latency < best.Latency {
			best = cs
		}
	}

	return best.Channel
}

// selectByQuality selects the channel with highest quality
func (r *SmartRouter) selectByQuality(candidates []*ChannelState) *model.Channel {
	if len(candidates) == 0 {
		return nil
	}

	// Select by weight (higher weight = higher quality)
	best := candidates[0]
	for _, cs := range candidates[1:] {
		if cs.Channel.Weight > best.Channel.Weight {
			best = cs
		}
	}

	return best.Channel
}

// selectByWeight selects by weight
func (r *SmartRouter) selectByWeight(candidates []*ChannelState) *model.Channel {
	if len(candidates) == 0 {
		return nil
	}

	// Weighted random selection
	totalWeight := 0
	for _, cs := range candidates {
		totalWeight += cs.Channel.Weight
	}

	// Simple selection for now
	best := candidates[0]
	for _, cs := range candidates[1:] {
		if cs.Channel.Weight > best.Channel.Weight {
			best = cs
		}
	}

	return best.Channel
}

// selectAuto selects based on prompt type
func (r *SmartRouter) selectAuto(modelName string, candidates []*ChannelState) *model.Channel {
	// Detect prompt type from model name
	promptType := detectPromptType(modelName)

	switch promptType {
	case PromptTypeCode:
		// For code tasks, prefer Claude
		for _, cs := range candidates {
			if cs.Channel.Type == model.ChannelTypeClaude {
				return cs.Channel
			}
		}
	case PromptTypeReason:
		// For reasoning tasks, prefer DeepSeek
		for _, cs := range candidates {
			if cs.Channel.Type == model.ChannelTypeDeepSeek {
				return cs.Channel
			}
		}
	case PromptTypeChat:
		// For chat tasks, prefer OpenAI
		for _, cs := range candidates {
			if cs.Channel.Type == model.ChannelTypeOpenAI {
				return cs.Channel
			}
		}
	}

	// Default: select by weight
	return r.selectByWeight(candidates)
}

// detectPromptType detects the prompt type from model name
func detectPromptType(modelName string) PromptType {
	modelName = strings.ToLower(modelName)

	if strings.Contains(modelName, "code") || strings.Contains(modelName, "coder") {
		return PromptTypeCode
	}
	if strings.Contains(modelName, "reason") || strings.Contains(modelName, "think") {
		return PromptTypeReason
	}
	if strings.Contains(modelName, "chat") || strings.Contains(modelName, "gpt") {
		return PromptTypeChat
	}

	return PromptTypeGeneral
}

// UpdateChannelHealth updates the health status of a channel
func (r *SmartRouter) UpdateChannelHealth(channelID int, healthy bool, latency time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, cs := range r.channels {
		if cs.Channel.ID == int64(channelID) {
			cs.Healthy = healthy
			cs.Latency = latency
			cs.LastChecked = time.Now()
			if !healthy {
				cs.FailedRequests++
			}
			cs.TotalRequests++
			break
		}
	}
}

// GetChannelStats returns channel statistics
func (r *SmartRouter) GetChannelStats() []ChannelStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	stats := make([]ChannelStats, 0, len(r.channels))
	for _, cs := range r.channels {
		stats = append(stats, ChannelStats{
			ChannelID:      int(cs.Channel.ID),
			Name:           cs.Channel.Name,
			Healthy:        cs.Healthy,
			Latency:        cs.Latency,
			TotalRequests:  cs.TotalRequests,
			FailedRequests: cs.FailedRequests,
		})
	}

	return stats
}

// ChannelStats represents channel statistics
type ChannelStats struct {
	ChannelID      int           `json:"channel_id"`
	Name           string        `json:"name"`
	Healthy        bool          `json:"healthy"`
	Latency        time.Duration `json:"latency"`
	TotalRequests  int64         `json:"total_requests"`
	FailedRequests int64         `json:"failed_requests"`
}
