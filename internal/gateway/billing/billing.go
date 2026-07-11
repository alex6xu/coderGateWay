package billing

import (
	"fmt"
	"sync"
	"time"

	"github.com/alex/codegateway/internal/model"
)

// BillingService handles billing and quota management
type BillingService struct {
	pricing map[string]ModelPricing
	mu      sync.RWMutex
}

// ModelPricing represents pricing for a model
type ModelPricing struct {
	Input  float64 `json:"input"`  // Price per 1M input tokens
	Output float64 `json:"output"` // Price per 1M output tokens
}

// NewBillingService creates a new billing service
func NewBillingService(pricing map[string]ModelPricing) *BillingService {
	return &BillingService{
		pricing: pricing,
	}
}

// CalculateCost calculates the cost for token usage
func (s *BillingService) CalculateCost(model string, promptTokens, completionTokens int) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	pricing, ok := s.pricing[model]
	if !ok {
		// Default pricing if model not found
		pricing = ModelPricing{
			Input:  3.0,
			Output: 15.0,
		}
	}

	// Calculate cost (prices are per 1M tokens)
	inputCost := float64(promptTokens) * pricing.Input / 1000000
	outputCost := float64(completionTokens) * pricing.Output / 1000000

	return inputCost + outputCost
}

// CheckQuota checks if user has enough quota
func (s *BillingService) CheckQuota(user *model.User, estimatedCost float64) bool {
	if user.Quota == 0 {
		return true // Unlimited quota
	}

	remaining := float64(user.Quota - user.UsedQuota)
	return remaining >= estimatedCost
}

// DeductQuota deducts quota from user
func (s *BillingService) DeductQuota(user *model.User, cost float64) error {
	if user.Quota > 0 {
		newUsed := user.UsedQuota + int64(cost*1000000) // Convert to micro-units
		if newUsed > user.Quota {
			return fmt.Errorf("insufficient quota")
		}
		user.UsedQuota = newUsed
	}
	return nil
}

// CheckTokenQuota checks if token has enough quota
func (s *BillingService) CheckTokenQuota(token *model.Token, estimatedCost float64) bool {
	if token.UnlimitedQuota {
		return true
	}

	remaining := float64(token.RemainQuota)
	return remaining >= estimatedCost
}

// DeductTokenQuota deducts quota from token
func (s *BillingService) DeductTokenQuota(token *model.Token, cost float64) error {
	if !token.UnlimitedQuota {
		newRemain := token.RemainQuota - int64(cost*1000000) // Convert to micro-units
		if newRemain < 0 {
			return fmt.Errorf("insufficient token quota")
		}
		token.RemainQuota = newRemain
	}
	return nil
}

// CreateUsageLog creates a usage log entry
func (s *BillingService) CreateUsageLog(userID, tokenID, channelID *int64, modelName string, promptTokens, completionTokens int, cost float64, latency int, status int) *model.UsageLog {
	return &model.UsageLog{
		UserID:           userID,
		TokenID:          tokenID,
		ChannelID:        channelID,
		Model:            modelName,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		Cost:             cost,
		Latency:          latency,
		Status:           status,
		CreatedAt:        time.Now(),
	}
}

// GetPricing returns pricing for a model
func (s *BillingService) GetPricing(modelName string) (ModelPricing, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	pricing, ok := s.pricing[modelName]
	return pricing, ok
}

// SetPricing sets pricing for a model
func (s *BillingService) SetPricing(modelName string, pricing ModelPricing) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.pricing[modelName] = pricing
}

// EstimateCost estimates the cost for a request
func (s *BillingService) EstimateCost(modelName string, estimatedTokens int) float64 {
	// Estimate 50% input, 50% output
	promptTokens := estimatedTokens / 2
	completionTokens := estimatedTokens / 2

	return s.CalculateCost(modelName, promptTokens, completionTokens)
}
