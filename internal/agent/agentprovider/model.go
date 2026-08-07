package agentprovider

import "github.com/alex/codegateway/internal/agent/agentcore"

// Model is provider-agnostic metadata describing a single model.
type Model struct {
	Provider         string                     `json:"provider"`
	ID               string                     `json:"id"`
	DisplayName      string                     `json:"displayName,omitempty"`
	ContextWindow    int                        `json:"contextWindow,omitempty"`
	MaxOutputTokens  int                        `json:"maxOutputTokens,omitempty"`
	SupportsThinking bool                       `json:"supportsThinking,omitempty"`
	SupportsTools    bool                       `json:"supportsTools,omitempty"`
	SupportsImages   bool                       `json:"supportsImages,omitempty"`
	ThinkingLevels   agentcore.ThinkingLevelMap `json:"-"`
}