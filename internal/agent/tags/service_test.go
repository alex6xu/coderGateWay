package tags

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/alex/codegateway/internal/config"
	"github.com/alex/codegateway/internal/db"
	"github.com/google/uuid"
)

func TestTagMessageAndOverview(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Init(config.DatabaseConfig{Driver: "sqlite", DSN: filepath.Join(dir, "t.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	_, err = database.Exec(`INSERT INTO users (username, password_hash, role, created_at, updated_at) VALUES ('u', 'x', 'user', ?, ?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	var uid int64
	_ = database.QueryRow(`SELECT id FROM users WHERE username='u'`).Scan(&uid)

	sid := uuid.NewString()
	_, _ = database.Exec(`INSERT INTO sessions (id, user_id, title, platform, message_count, created_at, updated_at) VALUES (?, ?, 't', 'web', 0, ?, ?)`, sid, uid, now, now)
	mid := uuid.NewString()
	_, _ = database.Exec(`INSERT INTO messages (id, session_id, role, content, created_at) VALUES (?, ?, 'user', ?, ?)`, mid, sid, "帮我排查这个 Go API 报错 bug", now)

	svc := NewService(database.DB)
	hits, err := svc.TagMessage(uid, mid, "帮我排查这个 Go API 报错 bug")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("expected hits")
	}

	list, err := svc.ListTags(uid, "", 20)
	if err != nil || len(list) == 0 {
		t.Fatalf("list=%v err=%v", list, err)
	}
	group, err := svc.ListMessagesByTag(uid, list[0].Slug, 10)
	if err != nil || len(group.Messages) == 0 {
		t.Fatalf("group=%v err=%v", group, err)
	}
	overview, err := svc.Overview(uid, 5, 3)
	if err != nil || len(overview) == 0 {
		t.Fatalf("overview=%v err=%v", overview, err)
	}
}
