package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alex/codegateway/internal/config"
	"github.com/alex/codegateway/internal/db"
)

func TestSafeJoinAndUploadFlow(t *testing.T) {
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
	_, err = database.Exec(`INSERT INTO users (username, password_hash, role, created_at, updated_at) VALUES ('u1', 'x', 'user', ?, ?)`, now, now)
	if err != nil {
		t.Fatal(err)
	}
	var uid int64
	if err := database.QueryRow(`SELECT id FROM users WHERE username = 'u1'`).Scan(&uid); err != nil {
		t.Fatal(err)
	}

	mgr := NewManager(database.DB, filepath.Join(dir, "ws"))
	ws, err := mgr.CreateEmpty(uid, "demo")
	if err != nil {
		t.Fatal(err)
	}

	content := strings.NewReader("package main\n")
	if err := mgr.WriteRelativeFile(ws, "cmd/main.go", content); err != nil {
		t.Fatal(err)
	}
	if err := mgr.RefreshStats(ws); err != nil {
		t.Fatal(err)
	}
	if ws.FileCount != 1 {
		t.Fatalf("file count=%d", ws.FileCount)
	}

	if _, err := SafeJoin(ws.RootPath, "../etc/passwd"); err == nil {
		t.Fatal("expected escape to fail")
	}

	abs, err := SafeJoin(ws.RootPath, "cmd/main.go")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "package main") {
		t.Fatalf("unexpected content: %s", data)
	}
}
