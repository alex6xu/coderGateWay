package account

import (
	"testing"
)

func TestPasswordHashAndAuth(t *testing.T) {
	hash, err := HashPassword("secret1")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !CheckPassword(hash, "secret1") {
		t.Fatal("password should match")
	}
	if CheckPassword(hash, "wrong") {
		t.Fatal("wrong password should not match")
	}
	if err := ValidatePassword("123"); err == nil {
		t.Fatal("short password should fail")
	}
}

func TestRegisterLoginSession(t *testing.T) {
	mgr := setupTestDB(t)
	if _, err := mgr.EnsureDefault(); err != nil {
		t.Fatalf("ensure default: %v", err)
	}

	user, err := mgr.Register("bob", "bob@example.com", "bobpass")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if user.Role != "user" {
		t.Fatalf("expected role user, got %s", user.Role)
	}
	if user.PasswordHash == "" {
		t.Fatal("password hash should be set")
	}

	authed, err := mgr.Authenticate("bob", "bobpass")
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if authed.ID != user.ID {
		t.Fatalf("auth id mismatch")
	}
	if _, err := mgr.Authenticate("bob", "bad"); err == nil {
		t.Fatal("bad password should fail")
	}

	sess, err := mgr.CreateSession(user.ID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	got, err := mgr.GetSessionUser(sess.Token)
	if err != nil {
		t.Fatalf("get session user: %v", err)
	}
	if got.ID != user.ID {
		t.Fatalf("session user mismatch")
	}

	if err := mgr.DeleteSession(sess.Token); err != nil {
		t.Fatalf("delete session: %v", err)
	}
	if _, err := mgr.GetSessionUser(sess.Token); err == nil {
		t.Fatal("deleted session should be invalid")
	}
}

func TestAdminDefaultPassword(t *testing.T) {
	mgr := setupTestDB(t)
	admin, err := mgr.EnsureDefault()
	if err != nil {
		t.Fatalf("ensure default: %v", err)
	}
	if admin.PasswordHash == "" {
		t.Fatal("admin should have password hash")
	}
	if _, err := mgr.Authenticate(DefaultUsername, DefaultAdminPassword); err != nil {
		t.Fatalf("admin login with default password: %v", err)
	}
}

func TestLookupAPIToken(t *testing.T) {
	mgr := setupTestDB(t)
	admin, err := mgr.EnsureDefault()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.LookupAPIToken("sk-missing"); err == nil {
		t.Fatal("expected missing key error")
	}

	const key = "sk-test-gateway-key-001"
	_, err = mgr.db.Exec(`
		INSERT INTO tokens (user_id, name, key, status, remain_quota, unlimited_quota, created_at)
		VALUES (?, 't1', ?, 1, -1, 1, CURRENT_TIMESTAMP)
	`, admin.ID, key)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := mgr.LookupAPIToken(key)
	if err != nil {
		t.Fatal(err)
	}
	if tok.UserID != admin.ID {
		t.Fatalf("owner=%d want %d", tok.UserID, admin.ID)
	}

	_, _ = mgr.db.Exec(`UPDATE tokens SET status = 0 WHERE key = ?`, key)
	if _, err := mgr.LookupAPIToken(key); err == nil {
		t.Fatal("disabled key should fail")
	}
}
