package githubvcs

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/alex/codegateway/internal/config"
	"github.com/alex/codegateway/internal/db"
)

func TestParseOwnerRepo(t *testing.T) {
	owner, repo, err := parseOwnerRepo("alex/demo")
	if err != nil || owner != "alex" || repo != "demo" {
		t.Fatalf("got %s/%s err=%v", owner, repo, err)
	}
	if _, _, err := parseOwnerRepo("bad"); err == nil {
		t.Fatal("expected error")
	}
}

func TestAuthenticatedCloneURL(t *testing.T) {
	u := authenticatedCloneURL("token", "o", "r")
	want := "https://x-access-token:token@github.com/o/r.git"
	if u != want {
		t.Fatalf("got %s want %s", u, want)
	}
}

func TestLocalClonePullPushRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	seed := filepath.Join(root, "seed")
	cloneDest := filepath.Join(root, "ws")

	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
			"GIT_TERMINAL_PROMPT=0",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}

	run("", "init", "--bare", "-b", "main", remote)
	run("", "init", "-b", "main", seed)
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run(seed, "add", ".")
	run(seed, "commit", "-m", "init")
	run(seed, "remote", "add", "origin", remote)
	run(seed, "push", "-u", "origin", "main")

	run("", "clone", remote, cloneDest)

	dir := t.TempDir()
	database, err := db.Init(config.DatabaseConfig{Driver: "sqlite", DSN: filepath.Join(dir, "t.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}
	_, err = database.Exec(`INSERT INTO users (username, password_hash, role, created_at, updated_at) VALUES ('u1', 'x', 'user', ?, ?)`, time.Now(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var uid int64
	_ = database.QueryRow(`SELECT id FROM users WHERE username = 'u1'`).Scan(&uid)
	_, err = database.Exec(`
		INSERT INTO github_connections (user_id, access_token, token_type, scope, github_user_id, github_login, created_at, updated_at)
		VALUES (?, 'fake-token', 'bearer', 'repo', 1, 'u1', ?, ?)
	`, uid, time.Now(), time.Now())
	if err != nil {
		t.Fatal(err)
	}

	svc := NewService(database.DB, config.GitHubConfig{
		Enabled: true, ClientID: "c", ClientSecret: "s", RedirectURL: "http://x/cb",
	})
	if err := svc.EnsureGitRepo(context.Background(), uid, cloneDest, "owner/repo", "main"); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(cloneDest, "README.md"), []byte("hello world\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := runGit(context.Background(), cloneDest, nil, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	commitEnv := []string{
		"GIT_AUTHOR_NAME=CodeGateway",
		"GIT_AUTHOR_EMAIL=codegateway@local",
		"GIT_COMMITTER_NAME=CodeGateway",
		"GIT_COMMITTER_EMAIL=codegateway@local",
	}
	if _, err := runGit(context.Background(), cloneDest, commitEnv, "commit", "-m", "update"); err != nil {
		t.Fatal(err)
	}
	if _, err := runGit(context.Background(), cloneDest, nil, "push", "origin", "HEAD:main"); err != nil {
		t.Fatal(err)
	}

	other := filepath.Join(root, "other")
	run("", "clone", remote, other)
	data, err := os.ReadFile(filepath.Join(other, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello world\n" {
		t.Fatalf("remote content mismatch: %q", data)
	}

	// Pull into a second workspace copy
	second := filepath.Join(root, "ws2")
	run("", "clone", remote, second)
	if err := os.WriteFile(filepath.Join(other, "README.md"), []byte("from other\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run(other, "add", ".")
	run(other, "commit", "-m", "other")
	run(other, "push", "origin", "main")

	if _, err := runGit(context.Background(), second, nil, "pull", "--ff-only", "origin", "main"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(second, "README.md"))
	if string(got) != "from other\n" {
		t.Fatalf("pull mismatch: %q", got)
	}
}
