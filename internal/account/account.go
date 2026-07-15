package account

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/alex/codegateway/internal/model"
)

const (
	// ContextKey is the gin context key for the active account ID.
	ContextKey = "account_id"
	// HeaderName is the HTTP header clients use to select an account.
	HeaderName = "X-Account-ID"
	// DefaultUsername is seeded on first boot for local use.
	DefaultUsername = "admin"
)

// Manager handles account (user) persistence.
type Manager struct {
	db *sql.DB
}

// NewManager creates an account manager.
func NewManager(db *sql.DB) *Manager {
	return &Manager{db: db}
}

// CreateRequest holds fields for creating an account.
type CreateRequest struct {
	Username string
	Email    string
	Role     string
	Quota    int64
	Password string
}

// Create inserts a new account.
func (m *Manager) Create(req *CreateRequest) (*model.User, error) {
	username := strings.TrimSpace(req.Username)
	if username == "" {
		return nil, fmt.Errorf("username is required")
	}

	role := req.Role
	if role == "" {
		role = "user"
	}

	var passwordHash interface{}
	if strings.TrimSpace(req.Password) != "" {
		hash, err := HashPassword(req.Password)
		if err != nil {
			return nil, err
		}
		passwordHash = hash
	}

	now := time.Now()
	result, err := m.db.Exec(`
		INSERT INTO users (username, email, password_hash, role, quota, used_quota, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 0, ?, ?)
	`, username, nullIfEmpty(req.Email), passwordHash, role, req.Quota, now, now)
	if err != nil {
		return nil, fmt.Errorf("failed to create account: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get account id: %w", err)
	}

	return m.Get(id)
}

// Get returns an account by ID.
func (m *Manager) Get(id int64) (*model.User, error) {
	var u model.User
	var email, passwordHash sql.NullString
	err := m.db.QueryRow(`
		SELECT id, username, email, password_hash, role, quota, used_quota, created_at, updated_at
		FROM users WHERE id = ?
	`, id).Scan(
		&u.ID, &u.Username, &email, &passwordHash, &u.Role,
		&u.Quota, &u.UsedQuota, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("account not found: %w", err)
	}
	if email.Valid {
		u.Email = email.String
	}
	if passwordHash.Valid {
		u.PasswordHash = passwordHash.String
	}
	return &u, nil
}

// GetByUsername returns an account by username.
func (m *Manager) GetByUsername(username string) (*model.User, error) {
	var u model.User
	var email, passwordHash sql.NullString
	err := m.db.QueryRow(`
		SELECT id, username, email, password_hash, role, quota, used_quota, created_at, updated_at
		FROM users WHERE username = ?
	`, username).Scan(
		&u.ID, &u.Username, &email, &passwordHash, &u.Role,
		&u.Quota, &u.UsedQuota, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("account not found: %w", err)
	}
	if email.Valid {
		u.Email = email.String
	}
	if passwordHash.Valid {
		u.PasswordHash = passwordHash.String
	}
	return &u, nil
}

// List returns all accounts.
func (m *Manager) List() ([]*model.User, error) {
	rows, err := m.db.Query(`
		SELECT id, username, email, role, quota, used_quota, created_at, updated_at
		FROM users ORDER BY id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list accounts: %w", err)
	}
	defer rows.Close()

	accounts := make([]*model.User, 0)
	for rows.Next() {
		var u model.User
		var email sql.NullString
		if err := rows.Scan(
			&u.ID, &u.Username, &email, &u.Role,
			&u.Quota, &u.UsedQuota, &u.CreatedAt, &u.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan account: %w", err)
		}
		if email.Valid {
			u.Email = email.String
		}
		accounts = append(accounts, &u)
	}
	return accounts, nil
}

// UpdateRequest holds optional account fields to update.
type UpdateRequest struct {
	Username *string
	Email    *string
	Role     *string
	Quota    *int64
}

// Update updates an account.
func (m *Manager) Update(id int64, req *UpdateRequest) (*model.User, error) {
	if _, err := m.Get(id); err != nil {
		return nil, err
	}

	query := "UPDATE users SET updated_at = ?"
	args := []interface{}{time.Now()}

	if req.Username != nil {
		username := strings.TrimSpace(*req.Username)
		if username == "" {
			return nil, fmt.Errorf("username cannot be empty")
		}
		query += ", username = ?"
		args = append(args, username)
	}
	if req.Email != nil {
		query += ", email = ?"
		args = append(args, nullIfEmpty(*req.Email))
	}
	if req.Role != nil {
		query += ", role = ?"
		args = append(args, *req.Role)
	}
	if req.Quota != nil {
		query += ", quota = ?"
		args = append(args, *req.Quota)
	}

	query += " WHERE id = ?"
	args = append(args, id)

	if _, err := m.db.Exec(query, args...); err != nil {
		return nil, fmt.Errorf("failed to update account: %w", err)
	}
	return m.Get(id)
}

// Delete removes an account and its owned channel/session data.
// Default admin (id=1 / username=admin) cannot be deleted.
func (m *Manager) Delete(id int64) error {
	u, err := m.Get(id)
	if err != nil {
		return err
	}
	if u.Username == DefaultUsername {
		return fmt.Errorf("cannot delete the default admin account")
	}

	tx, err := m.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Delete messages belonging to this account's sessions
	if _, err := tx.Exec(`
		DELETE FROM messages WHERE session_id IN (SELECT id FROM sessions WHERE user_id = ?)
	`, id); err != nil {
		return fmt.Errorf("failed to delete messages: %w", err)
	}

	if _, err := tx.Exec("DELETE FROM sessions WHERE user_id = ?", id); err != nil {
		return fmt.Errorf("failed to delete sessions: %w", err)
	}

	if _, err := tx.Exec("DELETE FROM channels WHERE user_id = ?", id); err != nil {
		return fmt.Errorf("failed to delete channels: %w", err)
	}

	if _, err := tx.Exec("DELETE FROM auth_sessions WHERE user_id = ?", id); err != nil {
		return fmt.Errorf("failed to delete auth sessions: %w", err)
	}

	if _, err := tx.Exec("DELETE FROM tokens WHERE user_id = ?", id); err != nil {
		return fmt.Errorf("failed to delete tokens: %w", err)
	}

	if _, err := tx.Exec("DELETE FROM usage_logs WHERE user_id = ?", id); err != nil {
		return fmt.Errorf("failed to delete usage logs: %w", err)
	}

	if _, err := tx.Exec("DELETE FROM users WHERE id = ?", id); err != nil {
		return fmt.Errorf("failed to delete account: %w", err)
	}

	return tx.Commit()
}

func nullIfEmpty(s string) interface{} {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}
