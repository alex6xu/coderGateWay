package account

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/alex/codegateway/internal/model"
	"github.com/google/uuid"
)

const (
	// SessionTTL is how long a login session remains valid.
	SessionTTL = 7 * 24 * time.Hour
	// AuthHeader is the preferred header for session tokens.
	AuthHeader = "Authorization"
	// SessionHeader is an alternative header for session tokens.
	SessionHeader = "X-Session-Token"
	// APIKeyHeader is used by OpenAI/Anthropic-style clients for gateway API keys.
	APIKeyHeader = "X-API-Key"
	// SessionContextKey stores the session token in gin context.
	SessionContextKey = "session_token"
	// APITokenIDContextKey stores the tokens.id when authenticated via API key.
	APITokenIDContextKey = "api_token_id"
	// AuthUserContextKey stores the authenticated user id.
	AuthUserContextKey = "auth_user_id"
	// AuthRoleContextKey stores the authenticated user role.
	AuthRoleContextKey = "auth_role"
)

// Session is a persisted login session.
type Session struct {
	Token     string
	UserID    int64
	ExpiresAt time.Time
	CreatedAt time.Time
}

// CreateSession issues a new session token for a user.
func (m *Manager) CreateSession(userID int64) (*Session, error) {
	token := uuid.NewString()
	now := time.Now()
	expires := now.Add(SessionTTL)

	_, err := m.db.Exec(`
		INSERT INTO auth_sessions (token, user_id, expires_at, created_at)
		VALUES (?, ?, ?, ?)
	`, token, userID, expires, now)
	if err != nil {
		return nil, fmt.Errorf("failed to create session: %w", err)
	}

	return &Session{
		Token:     token,
		UserID:    userID,
		ExpiresAt: expires,
		CreatedAt: now,
	}, nil
}

// GetSessionUser returns the user for a valid (non-expired) session token.
func (m *Manager) GetSessionUser(token string) (*model.User, error) {
	if token == "" {
		return nil, fmt.Errorf("session token required")
	}

	var userID int64
	var expiresAt time.Time
	err := m.db.QueryRow(`
		SELECT user_id, expires_at FROM auth_sessions WHERE token = ?
	`, token).Scan(&userID, &expiresAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("invalid session")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to lookup session: %w", err)
	}
	if time.Now().After(expiresAt) {
		_, _ = m.db.Exec("DELETE FROM auth_sessions WHERE token = ?", token)
		return nil, fmt.Errorf("session expired")
	}

	return m.Get(userID)
}

// DeleteSession removes a session token (logout).
func (m *Manager) DeleteSession(token string) error {
	_, err := m.db.Exec("DELETE FROM auth_sessions WHERE token = ?", token)
	if err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}
	return nil
}

// DeleteUserSessions removes all sessions for a user.
func (m *Manager) DeleteUserSessions(userID int64) error {
	_, err := m.db.Exec("DELETE FROM auth_sessions WHERE user_id = ?", userID)
	return err
}

// CleanupExpiredSessions deletes expired session rows.
func (m *Manager) CleanupExpiredSessions() error {
	_, err := m.db.Exec("DELETE FROM auth_sessions WHERE expires_at < ?", time.Now())
	return err
}

// APIToken is a resolved gateway API key ownership record.
type APIToken struct {
	ID     int64
	UserID int64
	Name   string
}

// LookupAPIToken finds an enabled, non-expired API key and returns its owner.
func (m *Manager) LookupAPIToken(key string) (*APIToken, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, fmt.Errorf("api key required")
	}
	var t APIToken
	var expiredAt sql.NullTime
	err := m.db.QueryRow(`
		SELECT id, user_id, COALESCE(name, ''), expired_at
		FROM tokens
		WHERE key = ? AND status = 1
	`, key).Scan(&t.ID, &t.UserID, &t.Name, &expiredAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("invalid api key")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to lookup api key: %w", err)
	}
	if expiredAt.Valid && time.Now().After(expiredAt.Time) {
		return nil, fmt.Errorf("api key expired")
	}
	return &t, nil
}
