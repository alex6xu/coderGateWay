package promptctx

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/alex/codegateway/internal/agent/memory"
	"github.com/alex/codegateway/internal/config"
	"github.com/alex/codegateway/internal/db"
	"github.com/alex/codegateway/internal/provider"
	"github.com/google/uuid"
)

func TestBuildSlidingWindowAndMemory(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Init(config.DatabaseConfig{Driver: "sqlite", DSN: filepath.Join(dir, "t.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}

	sessionID := uuid.NewString()
	now := time.Now()
	_, _ = database.Exec(`INSERT INTO sessions (id, user_id, title, platform, message_count, created_at, updated_at) VALUES (?, 1, 't', 'web', 0, ?, ?)`, sessionID, now, now)

	for i := 0; i < 6; i++ {
		role := "user"
		content := "question about login api " + uuid.NewString()
		if i%2 == 1 {
			role = "assistant"
			content = "answer about auth flow " + uuid.NewString()
		}
		_, _ = database.Exec(`INSERT INTO messages (id, session_id, role, content, created_at) VALUES (?, ?, ?, ?, ?)`,
			uuid.NewString(), sessionID, role, content, now.Add(time.Duration(i)*time.Second))
	}

	mem := memory.NewMemoryService(database.DB)
	_ = mem.UpsertSessionMemory(sessionID, "Session checkpoint: previously discussed login rate limiting and JWT refresh.")

	msgs := Build(database.DB, Options{
		System:       "You are a test assistant.",
		UserMessage:  "How should we implement login rate limiting?",
		SessionID:    sessionID,
		ExcludeMsgID: "",
		Cfg: config.AgentConfig{
			ContextBudgetTokens: 4000,
			HistoryMaxTurns:     4,
			MemoryConfig: config.MemoryConfig{
				Enabled:     true,
				MaxSnippets: 3,
			},
		},
		Memory: mem,
	})

	if len(msgs) < 3 {
		t.Fatalf("expected system + history/memory + user, got %d", len(msgs))
	}
	if msgs[0].Role != "system" {
		t.Fatalf("first message should be system")
	}
	if msgs[len(msgs)-1].Role != "user" {
		t.Fatalf("last message should be user")
	}

	// Ensure we did not dump unbounded history (6 prior + system + user would be a lot if untrimmed with tiny turns)
	if len(msgs) > 12 {
		t.Fatalf("context too large: %d messages", len(msgs))
	}
}

func TestCompactToolMessages(t *testing.T) {
	msgs := []provider.Message{
		{Role: "system", Content: "s"},
		{Role: "user", Content: "u"},
		{Role: "assistant", Content: "a", ToolCalls: []provider.ToolCall{{ID: "1"}}},
		{Role: "tool", Content: string(make([]byte, 5000)), ToolCallID: "1"},
		{Role: "assistant", Content: "a2", ToolCalls: []provider.ToolCall{{ID: "2"}}},
		{Role: "tool", Content: string(make([]byte, 5000)), ToolCallID: "2"},
		{Role: "assistant", Content: "a3", ToolCalls: []provider.ToolCall{{ID: "3"}}},
		{Role: "tool", Content: string(make([]byte, 5000)), ToolCallID: "3"},
	}
	CompactToolMessages(msgs, 1, 1000)
	// oldest tools compacted heavily
	if len(msgs[3].Content) >= 5000 {
		t.Fatalf("old tool result not compacted")
	}
	if len(msgs[7].Content) > 1100 {
		t.Fatalf("recent tool result not truncated: %d", len(msgs[7].Content))
	}
}

func TestSanitizeAndCheckpoint(t *testing.T) {
	q := memory.SanitizeFTSQuery("login!!! rate-limit 限流 API")
	if q == "" {
		t.Fatal("expected sanitized query")
	}

	dir := t.TempDir()
	database, err := db.Init(config.DatabaseConfig{Driver: "sqlite", DSN: filepath.Join(dir, "t.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}
	sessionID := uuid.NewString()
	now := time.Now()
	_, _ = database.Exec(`INSERT INTO sessions (id, user_id, title, platform, message_count, created_at, updated_at) VALUES (?, 1, 't', 'web', 0, ?, ?)`, sessionID, now, now)
	for i := 0; i < 10; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		_, _ = database.Exec(`INSERT INTO messages (id, session_id, role, content, created_at) VALUES (?, ?, ?, ?, ?)`,
			uuid.NewString(), sessionID, role, "msg about caching redis", now.Add(time.Duration(i)*time.Second))
	}
	mem := memory.NewMemoryService(database.DB)
	MaybeCheckpoint(database.DB, mem, sessionID, 10)
	hits, _ := mem.Search("caching redis", "sessions", sessionID, 5)
	if len(hits) == 0 {
		t.Fatal("expected checkpoint memory searchable")
	}
}
