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

-- Indexes
CREATE INDEX IF NOT EXISTS idx_tokens_user_id ON tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_tokens_key ON tokens(key);
CREATE INDEX IF NOT EXISTS idx_channels_user_id ON channels(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_platform ON sessions(platform);
CREATE INDEX IF NOT EXISTS idx_messages_session_id ON messages(session_id);
CREATE INDEX IF NOT EXISTS idx_messages_role ON messages(role);
CREATE INDEX IF NOT EXISTS idx_tasks_parent_id ON tasks(parent_id);
CREATE INDEX IF NOT EXISTS idx_tasks_session_id ON tasks(session_id);
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_usage_logs_user_id ON usage_logs(user_id);
CREATE INDEX IF NOT EXISTS idx_usage_logs_channel_id ON usage_logs(channel_id);
CREATE INDEX IF NOT EXISTS idx_usage_logs_created_at ON usage_logs(created_at);
`

func Migrate(db *DB) error {
	log.Println("Running database migrations...")
	_, err := db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	if err := migrateChannelsUserID(db); err != nil {
		return err
	}

	log.Println("Database migrations completed")
	return nil
}

// migrateChannelsUserID adds user_id to existing channels tables created before account isolation.
func migrateChannelsUserID(db *DB) error {
	rows, err := db.Query("PRAGMA table_info(channels)")
	if err != nil {
		return fmt.Errorf("failed to inspect channels table: %w", err)
	}
	defer rows.Close()

	hasUserID := false
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt interface{}
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return fmt.Errorf("failed to scan pragma: %w", err)
		}
		if name == "user_id" {
			hasUserID = true
			break
		}
	}
	if hasUserID {
		return nil
	}

	log.Println("Migrating channels: adding user_id column for per-account isolation")
	if _, err := db.Exec("ALTER TABLE channels ADD COLUMN user_id INTEGER REFERENCES users(id)"); err != nil {
		return fmt.Errorf("failed to add channels.user_id: %w", err)
	}
	if _, err := db.Exec("CREATE INDEX IF NOT EXISTS idx_channels_user_id ON channels(user_id)"); err != nil {
		return fmt.Errorf("failed to create channels.user_id index: %w", err)
	}

	// Assign orphaned channels to the default admin account when present
	var adminID int64
	err = db.QueryRow("SELECT id FROM users WHERE username = 'admin' LIMIT 1").Scan(&adminID)
	if err == nil {
		_, _ = db.Exec("UPDATE channels SET user_id = ? WHERE user_id IS NULL", adminID)
	}

	return nil
}
