package db

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// legacySchema mimics a pre-rename, pre-account-isolation database
// (channels table, no user_id on channels/sessions/usage_logs).
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

CREATE TABLE IF NOT EXISTS gateway_request_logs (
    id TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL,
    channel_id INTEGER,
    channel_name TEXT DEFAULT '',
    model TEXT DEFAULT '',
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
	for _, table := range []string{"providers", "sessions", "usage_logs"} {
		ok, err := tableHasColumn(database, table, "user_id")
		if err != nil || !ok {
			t.Fatalf("%s.user_id missing after fresh migrate: ok=%v err=%v", table, ok, err)
		}
	}
	hasChannels, err := tableExists(database, "channels")
	if err != nil || hasChannels {
		t.Fatalf("fresh DB should not have channels table: has=%v err=%v", hasChannels, err)
	}
	ok, err := tableHasColumn(database, "usage_logs", "provider_id")
	if err != nil || !ok {
		t.Fatalf("usage_logs.provider_id missing: ok=%v err=%v", ok, err)
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
		INSERT INTO gateway_request_logs (id, user_id, channel_id, channel_name, model) VALUES ('r1', 1, 1, 'openai', 'gpt-4o');
	`); err != nil {
		t.Fatalf("seed legacy data: %v", err)
	}

	// Confirm pre-migration state
	hasChannels, err := tableExists(database, "channels")
	if err != nil || !hasChannels {
		t.Fatalf("expected legacy channels table: has=%v err=%v", hasChannels, err)
	}
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

	hasChannels, err = tableExists(database, "channels")
	if err != nil || hasChannels {
		t.Fatalf("channels should be renamed away: has=%v err=%v", hasChannels, err)
	}
	hasProviders, err := tableExists(database, "providers")
	if err != nil || !hasProviders {
		t.Fatalf("providers missing after rename: has=%v err=%v", hasProviders, err)
	}

	for _, table := range []string{"providers", "sessions", "usage_logs"} {
		ok, err := tableHasColumn(database, table, "user_id")
		if err != nil || !ok {
			t.Fatalf("%s.user_id missing after upgrade: ok=%v err=%v", table, ok, err)
		}
	}
	ok, err := tableHasColumn(database, "usage_logs", "provider_id")
	if err != nil || !ok {
		t.Fatalf("usage_logs.provider_id missing after rename: ok=%v err=%v", ok, err)
	}
	ok, err = tableHasColumn(database, "gateway_request_logs", "provider_name")
	if err != nil || !ok {
		t.Fatalf("gateway_request_logs.provider_name missing after rename: ok=%v err=%v", ok, err)
	}

	var providerUserID, sessionUserID, usageUserID sql.NullInt64
	if err := database.QueryRow("SELECT user_id FROM providers WHERE id = 1").Scan(&providerUserID); err != nil {
		t.Fatalf("read provider user_id: %v", err)
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
	if !providerUserID.Valid || providerUserID.Int64 != adminID {
		t.Fatalf("provider not assigned to admin: %#v want %d", providerUserID, adminID)
	}
	if !sessionUserID.Valid || sessionUserID.Int64 != adminID {
		t.Fatalf("session not assigned to admin: %#v want %d", sessionUserID, adminID)
	}
	if !usageUserID.Valid || usageUserID.Int64 != adminID {
		t.Fatalf("usage_log not assigned to admin: %#v want %d", usageUserID, adminID)
	}

	var providerName string
	if err := database.QueryRow("SELECT provider_name FROM gateway_request_logs WHERE id = 'r1'").Scan(&providerName); err != nil {
		t.Fatalf("read gateway log provider_name: %v", err)
	}
	if providerName != "openai" {
		t.Fatalf("gateway log provider_name=%q want openai", providerName)
	}

	// Idempotent
	if err := Migrate(database); err != nil {
		t.Fatalf("second migrate failed: %v", err)
	}
}
