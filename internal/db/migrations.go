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

-- Gateway chat/completions request/response audit log
CREATE TABLE IF NOT EXISTS gateway_request_logs (
    id TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL,
    channel_id INTEGER,
    channel_name TEXT DEFAULT '',
    model TEXT DEFAULT '',
    stream INTEGER DEFAULT 0,
    status_code INTEGER DEFAULT 0,
    error TEXT DEFAULT '',
    request_body TEXT DEFAULT '',
    response_body TEXT DEFAULT '',
    prompt_tokens INTEGER DEFAULT 0,
    completion_tokens INTEGER DEFAULT 0,
    cached_tokens INTEGER DEFAULT 0,
    latency_ms INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id),
    FOREIGN KEY (channel_id) REFERENCES channels(id)
);
CREATE INDEX IF NOT EXISTS idx_gateway_request_logs_user_created ON gateway_request_logs(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_gateway_request_logs_model ON gateway_request_logs(user_id, model);

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

-- Ordered, tenant-scoped model candidates used by the cloud gateway.
CREATE TABLE IF NOT EXISTS route_profiles (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    purpose TEXT NOT NULL,
    models TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    UNIQUE(user_id, name),
    FOREIGN KEY (user_id) REFERENCES users(id)
);
CREATE INDEX IF NOT EXISTS idx_route_profiles_user_id ON route_profiles(user_id);

-- Durable task records for cloud workspace coding and documentation runs.
CREATE TABLE IF NOT EXISTS agent_tasks (
    id TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL,
    workspace_id TEXT NOT NULL,
    route_profile_id INTEGER NOT NULL,
    type TEXT NOT NULL,
    prompt TEXT NOT NULL,
    status TEXT NOT NULL,
    result TEXT NOT NULL DEFAULT "",
    error TEXT NOT NULL DEFAULT "",
    tool_steps TEXT NOT NULL DEFAULT "[]",
    created_at DATETIME NOT NULL,
    started_at DATETIME,
    finished_at DATETIME,
    FOREIGN KEY (user_id) REFERENCES users(id),
    FOREIGN KEY (workspace_id) REFERENCES workspaces(id),
    FOREIGN KEY (route_profile_id) REFERENCES route_profiles(id)
);
CREATE INDEX IF NOT EXISTS idx_agent_tasks_user_created ON agent_tasks(user_id, created_at DESC);

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

-- Claude Code subscription OAuth (one connection per account)
CREATE TABLE IF NOT EXISTS claude_connections (
    user_id INTEGER PRIMARY KEY,
    access_token TEXT NOT NULL,
    refresh_token TEXT DEFAULT '',
    scopes TEXT DEFAULT '',
    subscription_type TEXT DEFAULT '',
    device_id TEXT DEFAULT '',
    account_uuid TEXT DEFAULT '',
    expires_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id)
);

CREATE TABLE IF NOT EXISTS claude_oauth_states (
    state TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL,
    code_verifier TEXT NOT NULL,
    redirect_uri TEXT NOT NULL,
    mode TEXT DEFAULT 'auto',
    expires_at DATETIME NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id)
);
CREATE INDEX IF NOT EXISTS idx_claude_oauth_states_expires ON claude_oauth_states(expires_at);

-- Auto-classified tags for user questions
CREATE TABLE IF NOT EXISTS question_tags (
    id TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL,
    slug TEXT NOT NULL,
    name TEXT NOT NULL,
    kind TEXT DEFAULT 'category',
    use_count INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(user_id, slug),
    FOREIGN KEY (user_id) REFERENCES users(id)
);
CREATE INDEX IF NOT EXISTS idx_question_tags_user ON question_tags(user_id);
CREATE INDEX IF NOT EXISTS idx_question_tags_user_count ON question_tags(user_id, use_count DESC);

CREATE TABLE IF NOT EXISTS message_tags (
    message_id TEXT NOT NULL,
    tag_id TEXT NOT NULL,
    confidence REAL DEFAULT 0,
    source TEXT DEFAULT 'auto',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (message_id, tag_id),
    FOREIGN KEY (message_id) REFERENCES messages(id),
    FOREIGN KEY (tag_id) REFERENCES question_tags(id)
);
CREATE INDEX IF NOT EXISTS idx_message_tags_tag ON message_tags(tag_id);

-- Durable session agent runs (chat/coder) detached from HTTP
CREATE TABLE IF NOT EXISTS session_runs (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    user_id INTEGER NOT NULL,
    workspace_id TEXT NOT NULL DEFAULT '',
    mode TEXT NOT NULL DEFAULT 'chat',
    model TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    trigger_message_id TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    last_seq INTEGER NOT NULL DEFAULT 0,
    cancel_requested INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL,
    started_at DATETIME,
    finished_at DATETIME,
    FOREIGN KEY (session_id) REFERENCES sessions(id),
    FOREIGN KEY (user_id) REFERENCES users(id)
);
CREATE INDEX IF NOT EXISTS idx_session_runs_session ON session_runs(session_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_session_runs_status ON session_runs(status, created_at ASC);

CREATE TABLE IF NOT EXISTS session_run_events (
    run_id TEXT NOT NULL,
    seq INTEGER NOT NULL,
    type TEXT NOT NULL,
    payload TEXT NOT NULL DEFAULT '{}',
    created_at DATETIME NOT NULL,
    PRIMARY KEY (run_id, seq),
    FOREIGN KEY (run_id) REFERENCES session_runs(id)
);
CREATE INDEX IF NOT EXISTS idx_session_run_events_run ON session_run_events(run_id, seq);

CREATE TABLE IF NOT EXISTS session_run_inbox (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    run_id TEXT NOT NULL DEFAULT '',
    message_id TEXT NOT NULL,
    content TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    FOREIGN KEY (session_id) REFERENCES sessions(id),
    FOREIGN KEY (message_id) REFERENCES messages(id)
);
CREATE INDEX IF NOT EXISTS idx_session_run_inbox_run ON session_run_inbox(run_id, status, created_at ASC);
CREATE INDEX IF NOT EXISTS idx_session_run_inbox_session ON session_run_inbox(session_id, status, created_at ASC);
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

	if err := ensureChannelDefaultColumn(db); err != nil {
		return err
	}

	if err := ensureChannelAuthModeColumn(db); err != nil {
		return err
	}

	if err := ensureClaudeConnectionIdentityColumns(db); err != nil {
		return err
	}

	if err := ensureClaudeConnectionScopeColumns(db); err != nil {
		return err
	}

	if err := ensureClaudeOAuthStateColumns(db); err != nil {
		return err
	}

	log.Println("Database migrations completed")
	return nil
}

// ensureClaudeOAuthStateColumns adds columns that were introduced to
// claude_oauth_states after older databases were first created (the table is
// created with CREATE TABLE IF NOT EXISTS, which never alters an existing table).
func ensureClaudeOAuthStateColumns(db *DB) error {
	for _, col := range []struct {
		name string
		ddl  string
	}{
		{"code_verifier", "ALTER TABLE claude_oauth_states ADD COLUMN code_verifier TEXT DEFAULT ''"},
		{"redirect_uri", "ALTER TABLE claude_oauth_states ADD COLUMN redirect_uri TEXT DEFAULT ''"},
		{"mode", "ALTER TABLE claude_oauth_states ADD COLUMN mode TEXT DEFAULT 'auto'"},
	} {
		has, err := tableHasColumn(db, "claude_oauth_states", col.name)
		if err != nil {
			return err
		}
		if has {
			continue
		}
		log.Printf("Migrating claude_oauth_states: adding %s", col.name)
		if _, err := db.Exec(col.ddl); err != nil {
			return fmt.Errorf("failed to add claude_oauth_states.%s: %w", col.name, err)
		}
	}
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

func ensureChannelDefaultColumn(db *DB) error {
	has, err := tableHasColumn(db, "channels", "is_default")
	if err != nil {
		return err
	}
	if has {
		return nil
	}
	log.Println("Migrating channels: adding is_default")
	if _, err := db.Exec("ALTER TABLE channels ADD COLUMN is_default INTEGER DEFAULT 0"); err != nil {
		return fmt.Errorf("failed to add channels.is_default: %w", err)
	}
	return nil
}

func ensureChannelAuthModeColumn(db *DB) error {
	has, err := tableHasColumn(db, "channels", "auth_mode")
	if err != nil {
		return err
	}
	if has {
		return nil
	}
	log.Println("Migrating channels: adding auth_mode")
	if _, err := db.Exec("ALTER TABLE channels ADD COLUMN auth_mode TEXT DEFAULT 'api_key'"); err != nil {
		return fmt.Errorf("failed to add channels.auth_mode: %w", err)
	}
	return nil
}

func ensureClaudeConnectionIdentityColumns(db *DB) error {
	for _, col := range []string{"device_id", "account_uuid"} {
		has, err := tableHasColumn(db, "claude_connections", col)
		if err != nil {
			return err
		}
		if has {
			continue
		}
		log.Printf("Migrating claude_connections: adding %s", col)
		stmt := fmt.Sprintf("ALTER TABLE claude_connections ADD COLUMN %s TEXT DEFAULT ''", col)
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("failed to add claude_connections.%s: %w", col, err)
		}
	}
	return nil
}

// ensureClaudeConnectionScopeColumns reconciles claude_connections with the
// current code, which uses columns "scopes" and "subscription_type". Older
// databases created the table with "scope" (and lacked "subscription_type"),
// so we rename/extend rather than rely on CREATE TABLE IF NOT EXISTS.
func ensureClaudeConnectionScopeColumns(db *DB) error {
	hasScope, err := tableHasColumn(db, "claude_connections", "scope")
	if err != nil {
		return err
	}
	hasScopes, err := tableHasColumn(db, "claude_connections", "scopes")
	if err != nil {
		return err
	}
	if hasScope && !hasScopes {
		log.Println("Migrating claude_connections: renaming scope -> scopes")
		if _, err := db.Exec("ALTER TABLE claude_connections RENAME COLUMN scope TO scopes"); err != nil {
			return fmt.Errorf("failed to rename claude_connections.scope: %w", err)
		}
		hasScopes = true
	}
	if !hasScopes {
		log.Println("Migrating claude_connections: adding scopes")
		if _, err := db.Exec("ALTER TABLE claude_connections ADD COLUMN scopes TEXT DEFAULT ''"); err != nil {
			return fmt.Errorf("failed to add claude_connections.scopes: %w", err)
		}
	}

	hasSub, err := tableHasColumn(db, "claude_connections", "subscription_type")
	if err != nil {
		return err
	}
	if !hasSub {
		log.Println("Migrating claude_connections: adding subscription_type")
		if _, err := db.Exec("ALTER TABLE claude_connections ADD COLUMN subscription_type TEXT DEFAULT ''"); err != nil {
			return fmt.Errorf("failed to add claude_connections.subscription_type: %w", err)
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
