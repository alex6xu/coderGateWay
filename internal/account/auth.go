package account

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/alex/codegateway/internal/model"
)

// DefaultAdminPassword is used when CODEGATEWAY_ADMIN_PASSWORD is unset.
const DefaultAdminPassword = "admin123"

// EnsureDefault creates the default admin account if none exist.
func (m *Manager) EnsureDefault() (*model.User, error) {
	var count int
	if err := m.db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		return nil, fmt.Errorf("failed to count users: %w", err)
	}

	password := strings.TrimSpace(os.Getenv("CODEGATEWAY_ADMIN_PASSWORD"))
	if password == "" {
		password = DefaultAdminPassword
	}

	if count > 0 {
		admin, err := m.GetByUsername(DefaultUsername)
		if err != nil {
			return nil, err
		}
		if err := m.EnsurePassword(admin.ID, password); err != nil {
			return nil, err
		}
		return m.Get(admin.ID)
	}

	return m.Create(&CreateRequest{
		Username: DefaultUsername,
		Email:    "admin@codegateway.local",
		Role:     "admin",
		Password: password,
	})
}

// EnsurePassword sets a password when the account has none (legacy rows).
func (m *Manager) EnsurePassword(userID int64, plaintext string) error {
	u, err := m.Get(userID)
	if err != nil {
		return err
	}
	if u.PasswordHash != "" {
		return nil
	}
	hash, err := HashPassword(plaintext)
	if err != nil {
		return err
	}
	_, err = m.db.Exec(`
		UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?
	`, hash, time.Now(), userID)
	if err != nil {
		return fmt.Errorf("failed to set default password: %w", err)
	}
	return nil
}

// Authenticate verifies username/password and returns the user.
func (m *Manager) Authenticate(username, password string) (*model.User, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return nil, fmt.Errorf("invalid username or password")
	}

	u, err := m.GetByUsername(username)
	if err != nil {
		return nil, fmt.Errorf("invalid username or password")
	}
	if !CheckPassword(u.PasswordHash, password) {
		return nil, fmt.Errorf("invalid username or password")
	}
	return u, nil
}

// SetPassword updates a user's password and clears existing sessions.
func (m *Manager) SetPassword(userID int64, newPassword string) error {
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	_, err = m.db.Exec(`
		UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?
	`, hash, time.Now(), userID)
	if err != nil {
		return fmt.Errorf("failed to update password: %w", err)
	}
	_ = m.DeleteUserSessions(userID)
	return nil
}

// Register creates a normal user account with a password.
func (m *Manager) Register(username, email, password string) (*model.User, error) {
	return m.Create(&CreateRequest{
		Username: username,
		Email:    email,
		Role:     "user",
		Password: password,
	})
}
