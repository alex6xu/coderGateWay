package db

import (
	"fmt"
	"log"
)

const schema = `
-- Users table (accounts)
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT UNIQUE NOT NULL,
    email TEXT UNIQUE,
    password_hash TEXT,
    role TEXT DEFAULT 'user',
    quota INTEGER DEFAULT 0,
    used_quota INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- API Tokens table
CREATE TABLE IF NOT EXISTS tokens (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    key TEXT UNIQUE NOT NULL,
    name TEXT,
    status INTEGER DEFAULT 1,
    expired_at DATETIME,
    remain_quota INTEGER DEFAULT 0,
    unlimited_quota BOOLEAN DEFAULT FALSE,
    model_limits TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id)
);

-- Channels table (Provider configurations, scoped per account)
CREATE TABLE IF NOT EXISTS channels (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER,
    name TEXT NOT NULL,
    type INTEGER NOT NULL,
    key TEXT NOT NULL,
    base_url TEXT,
    models TEXT,
    weight INTEGER DEFAULT 1,
    priority INTEGER DEFAULT 0,
    status INTEGER DEFAULT 1,
    balance REAL DEFAULT 0,
    used_quota INTEGER DEFAULT 0,
    model_mapping TEXT,
    groups TEXT DEFAULT 'default',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id)
);

-- Sessions table
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    user_id INTEGER,
    title TEXT,
    platform TEXT,
    platform_session_id TEXT,
    message_count INTEGER DEFAULT 0,
    prompt_tokens INTEGER DEFAULT 0,
    completion_tokens INTEGER DEFAULT 0,
    cost REAL DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id)
);

-- Messages table
CREATE TABLE IF NOT EXISTS messages (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    role TEXT NOT NULL,
    content TEXT,
    model TEXT,
    provider TEXT,
    tokens INTEGER DEFAULT 0,
    cost REAL DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (session_id) REFERENCES sessions(id)
);

-- Tasks table
CREATE TABLE IF NOT EXISTS tasks (
    id TEXT PRIMARY KEY,
    parent_id TEXT,
    session_id TEXT,
    summary TEXT NOT NULL,
    status TEXT DEFAULT 'open',
    event_summary TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (parent_id) REFERENCES tasks(id),
    FOREIGN KEY (session_id) REFERENCES sessions(id)
);

-- Memory table (FTS5)
CREATE VIRTUAL TABLE IF NOT EXISTS memory_fts USING fts5(
    path,
    scope,
    scope_id,
    type,
    content,
    tokenize='porter unicode61'
);

-- Cron jobs table
CREATE TABLE IF NOT EXISTS cron_jobs (
    id TEXT PRIMARY KEY,
    cron TEXT NOT NULL,
    prompt TEXT NOT NULL,
    enabled BOOLEAN DEFAULT TRUE,
    last_run DATETIME,
    next_run DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Skills table
CREATE TABLE IF NOT EXISTS skills (
    id TEXT PRIMARY KEY,
    name TEXT UNIQUE NOT NULL,
    description TEXT,
    content TEXT,
    triggers TEXT,
    source TEXT DEFAULT 'builtin',
    usage_count INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Usage logs table
CREATE TABLE IF NOT EXISTS usage_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER,
    token_id INTEGER,
    channel_id INTEGER,
    model TEXT,
    prompt_tokens INTEGER,
    completion_tokens INTEGER,
    cost REAL,
    latency INTEGER,
    status INTEGER,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id),
    FOREIGN KEY (token_id) REFERENCES tokens(id),
    FOREIGN KEY (channel_id) REFERENCES channels(id)
);

-- Auth sessions (login tokens)
CREATE TABLE IF NOT EXISTS auth_sessions (
    token TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL,
    expires_at DATETIME NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id)
);

-- Indexes that do not depend on columns added by later upgrades
CREATE INDEX IF NOT EXISTS idx_tokens_user_id ON tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_tokens_key ON tokens(key);
CREATE INDEX IF NOT EXISTS idx_sessions_platform ON sessions(platform);
CREATE INDEX IF NOT EXISTS idx_messages_session_id ON messages(session_id);
CREATE INDEX IF NOT EXISTS idx_messages_session_created ON messages(session_id, created_at);
CREATE INDEX IF NOT EXISTS idx_messages_role ON messages(role);
CREATE INDEX IF NOT EXISTS idx_tasks_parent_id ON tasks(parent_id);
CREATE INDEX IF NOT EXISTS idx_tasks_session_id ON tasks(session_id);
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_usage_logs_channel_id ON usage_logs(channel_id);
CREATE INDEX IF NOT EXISTS idx_usage_logs_created_at ON usage_logs(created_at);
CREATE INDEX IF NOT EXISTS idx_auth_sessions_user_id ON auth_sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_auth_sessions_expires_at ON auth_sessions(expires_at);

-- Cloud workspaces for Coder agent projects
CREATE TABLE IF NOT EXISTS workspaces (
    id TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    root_path TEXT NOT NULL,
    file_count INTEGER DEFAULT 0,
    size_bytes INTEGER DEFAULT 0,
    source TEXT DEFAULT 'upload',
    github_full_name TEXT DEFAULT '',
    github_default_branch TEXT DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id)
);
CREATE INDEX IF NOT EXISTS idx_workspaces_user_id ON workspaces(user_id);

-- GitHub OAuth connections (one per account)
CREATE TABLE IF NOT EXISTS github_connections (
    user_id INTEGER PRIMARY KEY,
    access_token TEXT NOT NULL,
    token_type TEXT DEFAULT 'bearer',
    scope TEXT DEFAULT '',
    github_user_id INTEGER DEFAULT 0,
    github_login TEXT DEFAULT '',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id)
);

-- Short-lived OAuth CSRF states
CREATE TABLE IF NOT EXISTS github_oauth_states (
    state TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL,
    expires_at DATETIME NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id)
);
CREATE INDEX IF NOT EXISTS idx_github_oauth_states_expires ON github_oauth_states(expires_at);
`

// Indexes that require user_id columns. Created after upgrade migrations so existing
// databases created before account isolation do not fail on CREATE INDEX.
const userIDIndexes = `
CREATE INDEX IF NOT EXISTS idx_channels_user_id ON channels(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_usage_logs_user_id ON usage_logs(user_id);
`

func Migrate(db *DB) error {
	log.Println("Running database migrations...")
	_, err := db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	// Existing DBs used CREATE TABLE IF NOT EXISTS without user_id; add columns first.
	for _, table := range []string{"channels", "sessions", "usage_logs"} {
		if err := ensureUserIDColumn(db, table); err != nil {
			return err
		}
	}

	if _, err := db.Exec(userIDIndexes); err != nil {
		return fmt.Errorf("failed to create user_id indexes: %w", err)
	}

	if err := ensureWorkspaceGitColumns(db); err != nil {
		return err
	}

	log.Println("Database migrations completed")
	return nil
}

func ensureWorkspaceGitColumns(db *DB) error {
	for _, col := range []struct {
		name string
		ddl  string
	}{
		{"source", "ALTER TABLE workspaces ADD COLUMN source TEXT DEFAULT 'upload'"},
		{"github_full_name", "ALTER TABLE workspaces ADD COLUMN github_full_name TEXT DEFAULT ''"},
		{"github_default_branch", "ALTER TABLE workspaces ADD COLUMN github_default_branch TEXT DEFAULT ''"},
	} {
		has, err := tableHasColumn(db, "workspaces", col.name)
		if err != nil {
			return err
		}
		if has {
			continue
		}
		log.Printf("Migrating workspaces: adding %s", col.name)
		if _, err := db.Exec(col.ddl); err != nil {
			return fmt.Errorf("failed to add workspaces.%s: %w", col.name, err)
		}
	}
	return nil
}

// ensureUserIDColumn adds user_id to a table created before account isolation.
func ensureUserIDColumn(db *DB, table string) error {
	hasUserID, err := tableHasColumn(db, table, "user_id")
	if err != nil {
		return err
	}
	if hasUserID {
		return nil
	}

	log.Printf("Migrating %s: adding user_id column for per-account isolation", table)
	stmt := fmt.Sprintf("ALTER TABLE %s ADD COLUMN user_id INTEGER REFERENCES users(id)", table)
	if _, err := db.Exec(stmt); err != nil {
		return fmt.Errorf("failed to add %s.user_id: %w", table, err)
	}

	// Assign orphaned rows to the default admin account when present
	var adminID int64
	err = db.QueryRow("SELECT id FROM users WHERE username = 'admin' LIMIT 1").Scan(&adminID)
	if err == nil {
		update := fmt.Sprintf("UPDATE %s SET user_id = ? WHERE user_id IS NULL", table)
		_, _ = db.Exec(update, adminID)
	}

	return nil
}

func tableHasColumn(db *DB, table, column string) (bool, error) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, fmt.Errorf("failed to inspect %s table: %w", table, err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt interface{}
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return false, fmt.Errorf("failed to scan pragma: %w", err)
		}
		if name == column {
			return true, nil
		}
	}
	return false, nil
}
