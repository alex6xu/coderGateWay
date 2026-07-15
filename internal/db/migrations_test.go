package db

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// legacySchema mimics a pre-account-isolation database (no user_id on channels/sessions/usage_logs).
const legacySchema = `
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

CREATE TABLE IF NOT EXISTS tokens (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    key TEXT UNIQUE NOT NULL,
    name TEXT,
    status INTEGER DEFAULT 1,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS channels (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    type INTEGER NOT NULL,
    key TEXT NOT NULL,
    base_url TEXT,
    models TEXT,
    weight INTEGER DEFAULT 1,
    priority INTEGER DEFAULT 0,
    status INTEGER DEFAULT 1,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    title TEXT,
    platform TEXT,
    message_count INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS usage_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    token_id INTEGER,
    channel_id INTEGER,
    model TEXT,
    prompt_tokens INTEGER,
    completion_tokens INTEGER,
    cost REAL,
    latency INTEGER,
    status INTEGER,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
`

func openTestDB(t *testing.T) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return &DB{DB: sqlDB}
}

func TestMigrateFreshDatabase(t *testing.T) {
	database := openTestDB(t)
	if err := Migrate(database); err != nil {
		t.Fatalf("fresh migrate failed: %v", err)
	}
	for _, table := range []string{"channels", "sessions", "usage_logs"} {
		ok, err := tableHasColumn(database, table, "user_id")
		if err != nil || !ok {
			t.Fatalf("%s.user_id missing after fresh migrate: ok=%v err=%v", table, ok, err)
		}
	}
}

func TestMigrateUpgradesLegacyDatabase(t *testing.T) {
	database := openTestDB(t)
	if _, err := database.Exec(legacySchema); err != nil {
		t.Fatalf("seed legacy schema: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO users (username, email, role) VALUES ('admin', 'admin@codegateway.local', 'admin');
		INSERT INTO channels (name, type, key, status) VALUES ('openai', 1, 'sk-test', 1);
		INSERT INTO sessions (id, title, platform) VALUES ('s1', 'hello', 'web');
		INSERT INTO usage_logs (channel_id, model, prompt_tokens, completion_tokens) VALUES (1, 'gpt-4o', 1, 2);
	`); err != nil {
		t.Fatalf("seed legacy data: %v", err)
	}

	// Confirm pre-migration state
	for _, table := range []string{"channels", "sessions", "usage_logs"} {
		ok, err := tableHasColumn(database, table, "user_id")
		if err != nil {
			t.Fatalf("inspect %s: %v", table, err)
		}
		if ok {
			t.Fatalf("expected legacy %s without user_id", table)
		}
	}

	if err := Migrate(database); err != nil {
		t.Fatalf("legacy migrate failed: %v", err)
	}

	for _, table := range []string{"channels", "sessions", "usage_logs"} {
		ok, err := tableHasColumn(database, table, "user_id")
		if err != nil || !ok {
			t.Fatalf("%s.user_id missing after upgrade: ok=%v err=%v", table, ok, err)
		}
	}

	var channelUserID, sessionUserID, usageUserID sql.NullInt64
	if err := database.QueryRow("SELECT user_id FROM channels WHERE id = 1").Scan(&channelUserID); err != nil {
		t.Fatalf("read channel user_id: %v", err)
	}
	if err := database.QueryRow("SELECT user_id FROM sessions WHERE id = 's1'").Scan(&sessionUserID); err != nil {
		t.Fatalf("read session user_id: %v", err)
	}
	if err := database.QueryRow("SELECT user_id FROM usage_logs WHERE id = 1").Scan(&usageUserID); err != nil {
		t.Fatalf("read usage user_id: %v", err)
	}

	var adminID int64
	if err := database.QueryRow("SELECT id FROM users WHERE username = 'admin'").Scan(&adminID); err != nil {
		t.Fatalf("read admin: %v", err)
	}
	if !channelUserID.Valid || channelUserID.Int64 != adminID {
		t.Fatalf("channel not assigned to admin: %#v want %d", channelUserID, adminID)
	}
	if !sessionUserID.Valid || sessionUserID.Int64 != adminID {
		t.Fatalf("session not assigned to admin: %#v want %d", sessionUserID, adminID)
	}
	if !usageUserID.Valid || usageUserID.Int64 != adminID {
		t.Fatalf("usage_log not assigned to admin: %#v want %d", usageUserID, adminID)
	}

	// Idempotent
	if err := Migrate(database); err != nil {
		t.Fatalf("second migrate failed: %v", err)
	}
}
