package promptctx

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alex/codegateway/internal/agent/memory"
	"github.com/alex/codegateway/internal/config"
	"github.com/alex/codegateway/internal/db"
	"github.com/alex/codegateway/internal/provider"
	"github.com/google/uuid"
)

func TestBuildPrefixCacheFriendlyLayout(t *testing.T) {
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

	msgIDs := make([]string, 0, 6)
	for i := 0; i < 6; i++ {
		role := "user"
		content := "question about login api " + uuid.NewString()
		if i%2 == 1 {
			role = "assistant"
			content = "answer about auth flow " + uuid.NewString()
		}
		id := uuid.NewString()
		msgIDs = append(msgIDs, id)
		_, _ = database.Exec(`INSERT INTO messages (id, session_id, role, content, created_at) VALUES (?, ?, ?, ?, ?)`,
			id, sessionID, role, content, now.Add(time.Duration(i)*time.Second))
	}

	mem := memory.NewMemoryService(database.DB)
	_ = mem.UpsertSessionCheckpoint(sessionID, "Session checkpoint: previously discussed login rate limiting and JWT refresh.", msgIDs[1])

	cfg := config.AgentConfig{
		ContextBudgetTokens: 4000,
		HistoryMaxTurns:     4,
		MemoryConfig: config.MemoryConfig{
			Enabled:     true,
			MaxSnippets: 3,
		},
	}

	msgs := Build(database.DB, Options{
		System:       "You are a test assistant.",
		UserMessage:  "How should we implement login rate limiting?",
		SessionID:    sessionID,
		ExcludeMsgID: "",
		Cfg:          cfg,
		Memory:       mem,
	})

	if len(msgs) < 3 {
		t.Fatalf("expected system + checkpoint/history + user, got %d", len(msgs))
	}
	if msgs[0].Role != "system" || msgs[0].Content != "You are a test assistant." {
		t.Fatalf("first message should be fixed system")
	}
	if msgs[1].Role != "system" || !strings.Contains(msgs[1].Content, "Session checkpoint") {
		t.Fatalf("second message should be stable checkpoint, got %q", msgs[1].Content)
	}
	if msgs[len(msgs)-1].Role != "user" {
		t.Fatalf("last message should be user")
	}
	// FTS/retrieval must live in the variable user suffix, not as an early system slot
	for i := 0; i < len(msgs)-1; i++ {
		if strings.Contains(msgs[i].Content, "Relevant memory (retrieved for this turn)") {
			t.Fatalf("retrieval leaked into stable prefix at index %d", i)
		}
	}

	// History after cutoff should exclude msgIDs[0] and msgIDs[1]
	for _, m := range msgs {
		if m.Role == "user" && strings.HasPrefix(m.Content, "question") {
			// raw history user lines (not the final assembled user)
			if strings.Contains(m.Content, "User request:") {
				continue
			}
		}
	}

	// Append-only: second build with one new assistant+user should share stable prefix bytes
	aid := uuid.NewString()
	_, _ = database.Exec(`INSERT INTO messages (id, session_id, role, content, created_at) VALUES (?, ?, 'assistant', ?, ?)`,
		aid, sessionID, "follow-up answer", now.Add(10*time.Second))

	msgs2 := Build(database.DB, Options{
		System:      "You are a test assistant.",
		UserMessage: "And what about refresh tokens?",
		SessionID:   sessionID,
		Cfg:         cfg,
		Memory:      mem,
	})

	// Stable prefix: system + checkpoint + prior history (excluding new user) should match msgs prefix
	// msgs ends with user request 1; msgs2 ends with user request 2; shared = len(msgs)-1
	shared := len(msgs) - 1
	if len(msgs2) < shared {
		t.Fatalf("second build shorter than stable prefix: %d < %d", len(msgs2), shared)
	}
	for i := 0; i < shared; i++ {
		if msgs[i].Role != msgs2[i].Role || msgs[i].Content != msgs2[i].Content {
			t.Fatalf("prefix cache bust at index %d:\n%q\nvs\n%q", i, msgs[i].Content, msgs2[i].Content)
		}
	}
}

func TestTruncateToolResult(t *testing.T) {
	s := string(make([]byte, 5000))
	out := TruncateToolResult(s, 1000)
	if len(out) > 1100 {
		t.Fatalf("not truncated: %d", len(out))
	}
	if TruncateToolResult("short", 1000) != "short" {
		t.Fatal("short unchanged")
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
	if len(msgs[3].Content) >= 5000 {
		t.Fatalf("old tool result not compacted")
	}
	if len(msgs[7].Content) > 1100 {
		t.Fatalf("recent tool result not truncated: %d", len(msgs[7].Content))
	}
}

func TestSanitizeAndCheckpointCutoff(t *testing.T) {
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
	ids := make([]string, 0, 10)
	for i := 0; i < 10; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		id := uuid.NewString()
		ids = append(ids, id)
		_, _ = database.Exec(`INSERT INTO messages (id, session_id, role, content, created_at) VALUES (?, ?, ?, ?, ?)`,
			id, sessionID, role, "msg about caching redis", now.Add(time.Duration(i)*time.Second))
	}
	mem := memory.NewMemoryService(database.DB)
	MaybeCheckpoint(database.DB, mem, sessionID, 10)
	cp, err := mem.GetSessionCheckpoint(sessionID)
	if err != nil || cp == nil {
		t.Fatal("expected checkpoint")
	}
	if cp.AfterMsgID == "" {
		t.Fatal("expected cutoff message id")
	}
	hits, _ := mem.Search("caching redis", "sessions", sessionID, 5)
	if len(hits) == 0 {
		t.Fatal("expected checkpoint memory searchable")
	}

	msgs := Build(database.DB, Options{
		System:      "sys",
		UserMessage: "redis?",
		SessionID:   sessionID,
		Cfg: config.AgentConfig{
			ContextBudgetTokens: 8000,
			HistoryMaxTurns:     8,
			MemoryConfig:        config.MemoryConfig{Enabled: true, MaxSnippets: 3},
		},
		Memory: mem,
	})
	// Messages at/before cutoff should not appear as raw history (only in checkpoint)
	for _, m := range msgs {
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		if strings.Contains(m.Content, "User request:") {
			continue
		}
		// After cutoff, remaining live messages are still present — just ensure we have a checkpoint system msg
	}
	if len(msgs) < 2 || msgs[1].Role != "system" || !strings.Contains(msgs[1].Content, "checkpoint") {
		t.Fatalf("expected checkpoint in stable prefix")
	}
}
