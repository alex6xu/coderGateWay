package session

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/alex/codegateway/internal/model"
	"github.com/google/uuid"
)

// SessionManager manages sessions
type SessionManager struct {
	db *sql.DB
}

// NewSessionManager creates a new session manager
func NewSessionManager(db *sql.DB) *SessionManager {
	return &SessionManager{db: db}
}

// Create creates a new session
func (m *SessionManager) Create(userID *int64, platform string) (*model.Session, error) {
	session := &model.Session{
		ID:        uuid.New().String(),
		UserID:    userID,
		Platform:  platform,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	_, err := m.db.Exec(
		"INSERT INTO sessions (id, user_id, platform, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
		session.ID, session.UserID, session.Platform, session.CreatedAt, session.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	return session, nil
}

// Get returns a session by ID
func (m *SessionManager) Get(id string) (*model.Session, error) {
	var session model.Session
	err := m.db.QueryRow(
		"SELECT id, user_id, title, platform, platform_session_id, message_count, prompt_tokens, completion_tokens, cost, created_at, updated_at FROM sessions WHERE id = ?",
		id,
	).Scan(
		&session.ID, &session.UserID, &session.Title, &session.Platform,
		&session.PlatformSessionID, &session.MessageCount, &session.PromptTokens,
		&session.CompletionTokens, &session.Cost, &session.CreatedAt, &session.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("session not found: %w", err)
	}

	return &session, nil
}

// List returns all sessions for a user
func (m *SessionManager) List(userID *int64, limit, offset int) ([]*model.Session, error) {
	var rows *sql.Rows
	var err error

	if userID != nil {
		rows, err = m.db.Query(
			"SELECT id, user_id, title, platform, message_count, prompt_tokens, completion_tokens, cost, created_at, updated_at FROM sessions WHERE user_id = ? ORDER BY updated_at DESC LIMIT ? OFFSET ?",
			userID, limit, offset,
		)
	} else {
		rows, err = m.db.Query(
			"SELECT id, user_id, title, platform, message_count, prompt_tokens, completion_tokens, cost, created_at, updated_at FROM sessions ORDER BY updated_at DESC LIMIT ? OFFSET ?",
			limit, offset,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}
	defer rows.Close()

	sessions := make([]*model.Session, 0)
	for rows.Next() {
		var session model.Session
		err := rows.Scan(
			&session.ID, &session.UserID, &session.Title, &session.Platform,
			&session.MessageCount, &session.PromptTokens, &session.CompletionTokens,
			&session.Cost, &session.CreatedAt, &session.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan session: %w", err)
		}
		sessions = append(sessions, &session)
	}

	return sessions, nil
}

// Update updates a session
func (m *SessionManager) Update(session *model.Session) error {
	session.UpdatedAt = time.Now()

	_, err := m.db.Exec(
		"UPDATE sessions SET title = ?, message_count = ?, prompt_tokens = ?, completion_tokens = ?, cost = ?, updated_at = ? WHERE id = ?",
		session.Title, session.MessageCount, session.PromptTokens, session.CompletionTokens, session.Cost, session.UpdatedAt, session.ID,
	)
	if err != nil {
		return fmt.Errorf("failed to update session: %w", err)
	}

	return nil
}

// Delete deletes a session
func (m *SessionManager) Delete(id string) error {
	// Delete messages first
	_, err := m.db.Exec("DELETE FROM messages WHERE session_id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete messages: %w", err)
	}

	// Delete session
	_, err = m.db.Exec("DELETE FROM sessions WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}

	return nil
}

// AddMessage adds a message to a session
func (m *SessionManager) AddMessage(sessionID, role, content string, modelName string, provider string, tokens int, cost float64) (*model.Message, error) {
	message := &model.Message{
		ID:        uuid.New().String(),
		SessionID: sessionID,
		Role:      role,
		Content:   content,
		Model:     modelName,
		Provider:  provider,
		Tokens:    tokens,
		Cost:      cost,
		CreatedAt: time.Now(),
	}

	_, err := m.db.Exec(
		"INSERT INTO messages (id, session_id, role, content, model, provider, tokens, cost, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		message.ID, message.SessionID, message.Role, message.Content, message.Model, message.Provider, message.Tokens, message.Cost, message.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to add message: %w", err)
	}

	// Update session message count
	_, err = m.db.Exec(
		"UPDATE sessions SET message_count = message_count + 1, updated_at = ? WHERE id = ?",
		time.Now(), sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update session: %w", err)
	}

	return message, nil
}

// GetMessages returns all messages for a session
func (m *SessionManager) GetMessages(sessionID string, limit, offset int) ([]*model.Message, error) {
	rows, err := m.db.Query(
		"SELECT id, session_id, role, content, model, provider, tokens, cost, created_at FROM messages WHERE session_id = ? ORDER BY created_at ASC LIMIT ? OFFSET ?",
		sessionID, limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get messages: %w", err)
	}
	defer rows.Close()

	messages := make([]*model.Message, 0)
	for rows.Next() {
		var msg model.Message
		err := rows.Scan(
			&msg.ID, &msg.SessionID, &msg.Role, &msg.Content,
			&msg.Model, &msg.Provider, &msg.Tokens, &msg.Cost, &msg.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan message: %w", err)
		}
		messages = append(messages, &msg)
	}

	return messages, nil
}
