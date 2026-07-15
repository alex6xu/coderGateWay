package githubvcs

import (
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/alex/codegateway/internal/config"
	"github.com/alex/codegateway/internal/db"
)

func TestAuthorizeURLAndConfigured(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Init(config.DatabaseConfig{Driver: "sqlite", DSN: filepath.Join(dir, "t.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}

	svc := NewService(database.DB, config.GitHubConfig{
		Enabled:      true,
		ClientID:     "cid",
		ClientSecret: "sec",
		RedirectURL:  "http://localhost:8080/v1/github/callback",
		FrontendURL:  "/code",
		Scopes:       "read:user repo",
	})
	if !svc.Configured() {
		t.Fatal("expected configured")
	}

	// Need a user row for FK
	_, err = database.Exec(`INSERT INTO users (username, password_hash, role, created_at, updated_at) VALUES ('u1', 'x', 'user', ?, ?)`, time.Now(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var uid int64
	_ = database.QueryRow(`SELECT id FROM users WHERE username = 'u1'`).Scan(&uid)

	authURL, err := svc.AuthorizeURL(uid)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	if u.Host != "github.com" {
		t.Fatalf("unexpected host %s", u.Host)
	}
	q := u.Query()
	if q.Get("client_id") != "cid" || q.Get("state") == "" {
		t.Fatalf("bad query: %v", q)
	}

	redir := svc.FrontendRedirect(url.Values{"github": []string{"connected"}})
	if redir != "/code?github=connected" {
		t.Fatalf("redirect=%s", redir)
	}

	conn, err := svc.GetConnection(uid)
	if err != nil || conn.Connected {
		t.Fatalf("expected disconnected: %+v %v", conn, err)
	}
}
