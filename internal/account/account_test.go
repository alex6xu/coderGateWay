package account

import (
	"path/filepath"
	"testing"

	"github.com/alex/codegateway/internal/db"
	"github.com/alex/codegateway/internal/config"
)

func setupTestDB(t *testing.T) *Manager {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Init(config.DatabaseConfig{
		Driver: "sqlite",
		DSN:    filepath.Join(dir, "test.db"),
	})
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	if err := db.Migrate(database); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	return NewManager(database.DB)
}

func TestEnsureDefaultAndIsolationBasics(t *testing.T) {
	mgr := setupTestDB(t)

	admin, err := mgr.EnsureDefault()
	if err != nil {
		t.Fatalf("ensure default: %v", err)
	}
	if admin.Username != DefaultUsername {
		t.Fatalf("expected admin, got %s", admin.Username)
	}

	// Idempotent
	again, err := mgr.EnsureDefault()
	if err != nil {
		t.Fatalf("ensure default again: %v", err)
	}
	if again.ID != admin.ID {
		t.Fatalf("default account id changed: %d -> %d", admin.ID, again.ID)
	}

	alice, err := mgr.Create(&CreateRequest{Username: "alice", Email: "alice@example.com"})
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	if alice.ID == admin.ID {
		t.Fatal("alice should have distinct id")
	}

	list, err := mgr.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(list))
	}

	if err := mgr.Delete(admin.ID); err == nil {
		t.Fatal("should not delete default admin")
	}

	if err := mgr.Delete(alice.ID); err != nil {
		t.Fatalf("delete alice: %v", err)
	}

	list, err = mgr.List()
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 account after delete, got %d", len(list))
	}
}
