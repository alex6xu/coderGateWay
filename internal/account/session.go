package account

import (
	"database/sql"
	"fmt"
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
	// SessionContextKey stores the session token in gin context.
	SessionContextKey = "session_token"
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
