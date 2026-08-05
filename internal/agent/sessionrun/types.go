package sessionrun

import (
	"encoding/json"
	"time"
)

type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

type EventType string

const (
	EventMeta         EventType = "meta"
	EventDelta        EventType = "delta"
	EventToolStep     EventType = "tool_step"
	EventUserInjected EventType = "user_injected"
	EventDone         EventType = "done"
	EventError        EventType = "error"
)

type InboxStatus string

const (
	InboxPending         InboxStatus = "pending"
	InboxInjected        InboxStatus = "injected"
	InboxConsumedAsRun   InboxStatus = "consumed_as_run"
)

// Run is one durable agent turn for a chat/coder session.
type Run struct {
	ID               string     `json:"id"`
	SessionID        string     `json:"session_id"`
	UserID           int64      `json:"user_id"`
	WorkspaceID      string     `json:"workspace_id"`
	Mode             string     `json:"mode"`
	Model            string     `json:"model"`
	Status           Status     `json:"status"`
	TriggerMessageID string     `json:"trigger_message_id"`
	Error            string     `json:"error"`
	LastSeq          int64      `json:"last_seq"`
	CancelRequested  bool       `json:"cancel_requested"`
	CreatedAt        time.Time  `json:"created_at"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	FinishedAt       *time.Time `json:"finished_at,omitempty"`
}

type Event struct {
	RunID     string          `json:"run_id"`
	Seq       int64           `json:"seq"`
	Type      EventType       `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

type InboxItem struct {
	ID        string      `json:"id"`
	SessionID string      `json:"session_id"`
	RunID     string      `json:"run_id"`
	MessageID string      `json:"message_id"`
	Content   string      `json:"content"`
	Status    InboxStatus `json:"status"`
	CreatedAt time.Time   `json:"created_at"`
}

type CreateRunInput struct {
	SessionID        string
	UserID           int64
	WorkspaceID      string
	Mode             string
	Model            string
	TriggerMessageID string
}
