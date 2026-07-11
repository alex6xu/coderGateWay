package model

import (
	"time"
)

type User struct {
	ID          int64     `json:"id"`
	Username    string    `json:"username"`
	Email       string    `json:"email"`
	PasswordHash string   `json:"-"`
	Role        string    `json:"role"`
	Quota       int64     `json:"quota"`
	UsedQuota   int64     `json:"used_quota"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Token struct {
	ID             int64      `json:"id"`
	UserID         int64      `json:"user_id"`
	Key            string     `json:"key"`
	Name           string     `json:"name"`
	Status         int        `json:"status"`
	ExpiredAt      *time.Time `json:"expired_at"`
	RemainQuota    int64      `json:"remain_quota"`
	UnlimitedQuota bool       `json:"unlimited_quota"`
	ModelLimits    string     `json:"model_limits"`
	CreatedAt      time.Time  `json:"created_at"`
}

type Channel struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	Type         int       `json:"type"`
	Key          string    `json:"key"`
	BaseURL      string    `json:"base_url"`
	Models       string    `json:"models"`
	Weight       int       `json:"weight"`
	Priority     int       `json:"priority"`
	Status       int       `json:"status"`
	Balance      float64   `json:"balance"`
	UsedQuota    int64     `json:"used_quota"`
	ModelMapping string    `json:"model_mapping"`
	Groups       string    `json:"groups"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Session struct {
	ID                string    `json:"id"`
	UserID            *int64    `json:"user_id"`
	Title             string    `json:"title"`
	Platform          string    `json:"platform"`
	PlatformSessionID string    `json:"platform_session_id"`
	MessageCount      int       `json:"message_count"`
	PromptTokens      int64     `json:"prompt_tokens"`
	CompletionTokens  int64     `json:"completion_tokens"`
	Cost              float64   `json:"cost"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type Message struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Model     string    `json:"model"`
	Provider  string    `json:"provider"`
	Tokens    int       `json:"tokens"`
	Cost      float64   `json:"cost"`
	CreatedAt time.Time `json:"created_at"`
}

type Task struct {
	ID           string     `json:"id"`
	ParentID     *string    `json:"parent_id"`
	SessionID    *string    `json:"session_id"`
	Summary      string     `json:"summary"`
	Status       string     `json:"status"`
	EventSummary string     `json:"event_summary"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type CronJob struct {
	ID        string     `json:"id"`
	Cron      string     `json:"cron"`
	Prompt    string     `json:"prompt"`
	Enabled   bool       `json:"enabled"`
	LastRun   *time.Time `json:"last_run"`
	NextRun   *time.Time `json:"next_run"`
	CreatedAt time.Time  `json:"created_at"`
}

type Skill struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Content     string    `json:"content"`
	Triggers    string    `json:"triggers"`
	Source      string    `json:"source"`
	UsageCount  int       `json:"usage_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type UsageLog struct {
	ID               int64     `json:"id"`
	UserID           *int64    `json:"user_id"`
	TokenID          *int64    `json:"token_id"`
	ChannelID        *int64    `json:"channel_id"`
	Model            string    `json:"model"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	Cost             float64   `json:"cost"`
	Latency          int       `json:"latency"`
	Status           int       `json:"status"`
	CreatedAt        time.Time `json:"created_at"`
}

// Channel types
const (
	ChannelTypeOpenAI   = 1
	ChannelTypeClaude   = 2
	ChannelTypeGemini   = 3
	ChannelTypeDeepSeek = 4
	ChannelTypeOllama   = 5
	ChannelTypeCustom   = 99
)

// Token status
const (
	TokenStatusEnabled  = 1
	TokenStatusDisabled = 0
)

// Task status
const (
	TaskStatusOpen       = "open"
	TaskStatusInProgress = "in_progress"
	TaskStatusBlocked    = "blocked"
	TaskStatusDone       = "done"
	TaskStatusAbandoned  = "abandoned"
)

// Message roles
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
	RoleSystem    = "system"
)
