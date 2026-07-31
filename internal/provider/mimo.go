package provider

import (
	"strings"
)

const mimoDefaultBaseURL = "https://api.xiaomimimo.com/v1"

// MiMoProvider is the official MiMo API (API-key, OpenAI-compatible).
type MiMoProvider struct {
	*OpenAIProvider
}

// NewMiMoProvider creates a MiMo provider backed by the OpenAI-compatible API.
func NewMiMoProvider(config *ProviderConfig) *MiMoProvider {
	cfg := *config
	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = mimoDefaultBaseURL
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	return &MiMoProvider{OpenAIProvider: NewOpenAIProvider(&cfg)}
}

// NormalizeModelAlias normalizes model names for channel matching.
func NormalizeModelAlias(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	if strings.HasPrefix(m, "mimo/") {
		return strings.TrimPrefix(m, "mimo/")
	}
	return m
}
