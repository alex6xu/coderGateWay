package account

import (
	"testing"
	"time"
)

func TestChannelAndSessionIsolation(t *testing.T) {
	mgr := setupTestDB(t)
	admin, err := mgr.EnsureDefault()
	if err != nil {
		t.Fatalf("ensure default: %v", err)
	}
	alice, err := mgr.Create(&CreateRequest{Username: "alice"})
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}

	now := time.Now()
	_, err = mgr.db.Exec(`
		INSERT INTO channels (user_id, name, type, key, status, created_at, updated_at)
		VALUES (?, 'admin-ch', 1, 'sk-admin', 1, ?, ?), (?, 'alice-ch', 1, 'sk-alice', 1, ?, ?)
	`, admin.ID, now, now, alice.ID, now, now)
	if err != nil {
		t.Fatalf("insert channels: %v", err)
	}

	_, err = mgr.db.Exec(`
		INSERT INTO sessions (id, user_id, title, platform, created_at, updated_at)
		VALUES ('s-admin', ?, 'admin session', 'web', ?, ?), ('s-alice', ?, 'alice session', 'web', ?, ?)
	`, admin.ID, now, now, alice.ID, now, now)
	if err != nil {
		t.Fatalf("insert sessions: %v", err)
	}

	var adminChannels, aliceChannels int
	mgr.db.QueryRow("SELECT COUNT(*) FROM channels WHERE user_id = ?", admin.ID).Scan(&adminChannels)
	mgr.db.QueryRow("SELECT COUNT(*) FROM channels WHERE user_id = ?", alice.ID).Scan(&aliceChannels)
	if adminChannels != 1 || aliceChannels != 1 {
		t.Fatalf("channel isolation broken: admin=%d alice=%d", adminChannels, aliceChannels)
	}

	var adminSessions, aliceSessions int
	mgr.db.QueryRow("SELECT COUNT(*) FROM sessions WHERE user_id = ?", admin.ID).Scan(&adminSessions)
	mgr.db.QueryRow("SELECT COUNT(*) FROM sessions WHERE user_id = ?", alice.ID).Scan(&aliceSessions)
	if adminSessions != 1 || aliceSessions != 1 {
		t.Fatalf("session isolation broken: admin=%d alice=%d", adminSessions, aliceSessions)
	}

	// Deleting alice should remove only her data
	if err := mgr.Delete(alice.ID); err != nil {
		t.Fatalf("delete alice: %v", err)
	}
	mgr.db.QueryRow("SELECT COUNT(*) FROM channels WHERE user_id = ?", alice.ID).Scan(&aliceChannels)
	mgr.db.QueryRow("SELECT COUNT(*) FROM sessions WHERE user_id = ?", alice.ID).Scan(&aliceSessions)
	if aliceChannels != 0 || aliceSessions != 0 {
		t.Fatalf("alice data not cleaned: channels=%d sessions=%d", aliceChannels, aliceSessions)
	}
	mgr.db.QueryRow("SELECT COUNT(*) FROM channels WHERE user_id = ?", admin.ID).Scan(&adminChannels)
	if adminChannels != 1 {
		t.Fatalf("admin channel should remain, got %d", adminChannels)
	}
}
