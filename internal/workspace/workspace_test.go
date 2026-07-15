package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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

	mgr := NewManager(database.DB, filepath.Join(dir, "ws"))
	ws, err := mgr.CreateEmpty(1, "demo")
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
