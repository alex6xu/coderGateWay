package githubvcs

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// SyncResult summarizes a pull/clone operation.
type SyncResult struct {
	Branch  string `json:"branch"`
	HEAD    string `json:"head"`
	Message string `json:"message"`
}

// PushResult summarizes a push operation.
type PushResult struct {
	Branch  string `json:"branch"`
	HEAD    string `json:"head"`
	Pushed  bool   `json:"pushed"`
	Message string `json:"message"`
}

func (s *Service) accessToken(accountID int64) (string, error) {
	conn, err := s.GetConnection(accountID)
	if err != nil {
		return "", err
	}
	if !conn.Connected || strings.TrimSpace(conn.AccessToken) == "" {
		return "", fmt.Errorf("github not connected")
	}
	return conn.AccessToken, nil
}

func authenticatedCloneURL(token, owner, repo string) string {
	u := url.URL{
		Scheme: "https",
		User:   url.UserPassword("x-access-token", token),
		Host:   "github.com",
		Path:   fmt.Sprintf("/%s/%s.git", owner, repo),
	}
	return u.String()
}

func publicRemoteURL(owner, repo string) string {
	return fmt.Sprintf("https://github.com/%s/%s.git", owner, repo)
}

func parseOwnerRepo(fullName string) (owner, repo string, err error) {
	parts := strings.Split(strings.Trim(strings.TrimSpace(fullName), "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid github full name %q", fullName)
	}
	return parts[0], parts[1], nil
}

func gitAvailable() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

func runGit(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=echo")
	cmd.Env = append(cmd.Env, env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func (s *Service) withAuthRemote(ctx context.Context, dir, token, owner, repo string, fn func() error) error {
	authURL := authenticatedCloneURL(token, owner, repo)
	publicURL := publicRemoteURL(owner, repo)
	if _, err := runGit(ctx, dir, nil, "remote", "set-url", "origin", authURL); err != nil {
		// origin may not exist yet
		if _, err2 := runGit(ctx, dir, nil, "remote", "add", "origin", authURL); err2 != nil {
			return err
		}
	}
	defer func() {
		_, _ = runGit(context.Background(), dir, nil, "remote", "set-url", "origin", publicURL)
	}()
	return fn()
}

// CloneRepo clones a GitHub repository into destDir (must not exist or be empty).
func (s *Service) CloneRepo(ctx context.Context, accountID int64, owner, repo, branch, destDir string) (*SyncResult, error) {
	if !gitAvailable() {
		return nil, fmt.Errorf("git is not installed on the server")
	}
	token, err := s.accessToken(accountID)
	if err != nil {
		return nil, err
	}
	owner = strings.TrimSpace(owner)
	repo = strings.TrimSpace(repo)
	branch = strings.TrimSpace(branch)
	if owner == "" || repo == "" {
		return nil, fmt.Errorf("owner and repo are required")
	}

	if err := os.MkdirAll(filepath.Dir(destDir), 0755); err != nil {
		return nil, err
	}
	// git clone refuses non-empty directories
	if entries, err := os.ReadDir(destDir); err == nil && len(entries) > 0 {
		return nil, fmt.Errorf("destination is not empty: %s", destDir)
	}
	_ = os.RemoveAll(destDir)

	args := []string{"clone", "--depth", "1"}
	if branch != "" {
		args = append(args, "--branch", branch)
	}
	args = append(args, authenticatedCloneURL(token, owner, repo), destDir)

	if _, err := runGit(ctx, "", nil, args...); err != nil {
		_ = os.RemoveAll(destDir)
		return nil, err
	}
	// Strip credentials from remote URL.
	_, _ = runGit(ctx, destDir, nil, "remote", "set-url", "origin", publicRemoteURL(owner, repo))

	if branch == "" {
		branch, _ = runGit(ctx, destDir, nil, "rev-parse", "--abbrev-ref", "HEAD")
	}
	head, _ := runGit(ctx, destDir, nil, "rev-parse", "--short", "HEAD")
	return &SyncResult{
		Branch:  branch,
		HEAD:    head,
		Message: fmt.Sprintf("cloned %s/%s@%s", owner, repo, branch),
	}, nil
}

// EnsureGitRepo makes sure workspaceRoot is a git checkout of fullName.
// If .git is missing (legacy zipball import), re-clones into a temp dir and swaps content.
func (s *Service) EnsureGitRepo(ctx context.Context, accountID int64, workspaceRoot, fullName, branch string) error {
	gitDir := filepath.Join(workspaceRoot, ".git")
	if st, err := os.Stat(gitDir); err == nil && st.IsDir() {
		return nil
	}
	owner, repo, err := parseOwnerRepo(fullName)
	if err != nil {
		return err
	}
	tmp := workspaceRoot + ".gitclone-" + fmt.Sprintf("%d", time.Now().UnixNano())
	defer os.RemoveAll(tmp)
	if _, err := s.CloneRepo(ctx, accountID, owner, repo, branch, tmp); err != nil {
		return fmt.Errorf("initialize git repo: %w", err)
	}
	// Preserve local (possibly modified) files: copy .git into workspace, then set remote.
	srcGit := filepath.Join(tmp, ".git")
	dstGit := filepath.Join(workspaceRoot, ".git")
	if err := copyDir(srcGit, dstGit); err != nil {
		return err
	}
	token, err := s.accessToken(accountID)
	if err != nil {
		return err
	}
	return s.withAuthRemote(ctx, workspaceRoot, token, owner, repo, func() error {
		_, _ = runGit(ctx, workspaceRoot, nil, "fetch", "origin")
		if branch == "" {
			branch, _ = runGit(ctx, workspaceRoot, nil, "rev-parse", "--abbrev-ref", "HEAD")
		}
		if branch != "" {
			_, _ = runGit(ctx, workspaceRoot, nil, "checkout", "-B", branch)
			_, _ = runGit(ctx, workspaceRoot, nil, "branch", "--set-upstream-to=origin/"+branch, branch)
		}
		return nil
	})
}

// PullWorkspace fetches and fast-forwards (with autostash) the workspace from GitHub.
func (s *Service) PullWorkspace(ctx context.Context, accountID int64, workspaceRoot, fullName, branch string) (*SyncResult, error) {
	if !gitAvailable() {
		return nil, fmt.Errorf("git is not installed on the server")
	}
	owner, repo, err := parseOwnerRepo(fullName)
	if err != nil {
		return nil, err
	}
	if err := s.EnsureGitRepo(ctx, accountID, workspaceRoot, fullName, branch); err != nil {
		return nil, err
	}
	token, err := s.accessToken(accountID)
	if err != nil {
		return nil, err
	}
	if branch == "" {
		branch, _ = runGit(ctx, workspaceRoot, nil, "rev-parse", "--abbrev-ref", "HEAD")
	}

	var head string
	err = s.withAuthRemote(ctx, workspaceRoot, token, owner, repo, func() error {
		if _, err := runGit(ctx, workspaceRoot, nil, "fetch", "origin"); err != nil {
			return err
		}
		ref := "origin/" + branch
		if branch == "" || branch == "HEAD" {
			ref = "origin/HEAD"
		}
		if _, err := runGit(ctx, workspaceRoot, nil, "pull", "--ff-only", "--autostash", "origin", branch); err != nil {
			// fallback: merge fetched tip
			if _, err2 := runGit(ctx, workspaceRoot, nil, "merge", "--ff-only", ref); err2 != nil {
				return err
			}
		}
		head, _ = runGit(ctx, workspaceRoot, nil, "rev-parse", "--short", "HEAD")
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &SyncResult{
		Branch:  branch,
		HEAD:    head,
		Message: fmt.Sprintf("pulled latest from %s (%s)", fullName, branch),
	}, nil
}

// PushWorkspace stages all changes, commits if needed, and pushes to GitHub.
func (s *Service) PushWorkspace(ctx context.Context, accountID int64, workspaceRoot, fullName, branch, message string) (*PushResult, error) {
	if !gitAvailable() {
		return nil, fmt.Errorf("git is not installed on the server")
	}
	owner, repo, err := parseOwnerRepo(fullName)
	if err != nil {
		return nil, err
	}
	if err := s.EnsureGitRepo(ctx, accountID, workspaceRoot, fullName, branch); err != nil {
		return nil, err
	}
	token, err := s.accessToken(accountID)
	if err != nil {
		return nil, err
	}
	message = strings.TrimSpace(message)
	if message == "" {
		message = "Update from CodeGateway"
	}
	if branch == "" {
		branch, _ = runGit(ctx, workspaceRoot, nil, "rev-parse", "--abbrev-ref", "HEAD")
	}
	if branch == "" || branch == "HEAD" {
		branch = "main"
	}

	if _, err := runGit(ctx, workspaceRoot, nil, "add", "-A"); err != nil {
		return nil, err
	}
	status, err := runGit(ctx, workspaceRoot, nil, "status", "--porcelain")
	if err != nil {
		return nil, err
	}

	committed := false
	if status != "" {
		commitEnv := []string{
			"GIT_AUTHOR_NAME=CodeGateway",
			"GIT_AUTHOR_EMAIL=codegateway@local",
			"GIT_COMMITTER_NAME=CodeGateway",
			"GIT_COMMITTER_EMAIL=codegateway@local",
		}
		if _, err := runGit(ctx, workspaceRoot, commitEnv, "commit", "-m", message); err != nil {
			return nil, err
		}
		committed = true
	}

	var head string
	err = s.withAuthRemote(ctx, workspaceRoot, token, owner, repo, func() error {
		if _, err := runGit(ctx, workspaceRoot, nil, "push", "-u", "origin", "HEAD:"+branch); err != nil {
			return err
		}
		head, _ = runGit(ctx, workspaceRoot, nil, "rev-parse", "--short", "HEAD")
		return nil
	})
	if err != nil {
		return nil, err
	}

	msg := fmt.Sprintf("pushed to %s@%s", fullName, branch)
	if !committed {
		msg = fmt.Sprintf("no local changes; ensured remote %s@%s is up to date", fullName, branch)
	}
	return &PushResult{
		Branch:  branch,
		HEAD:    head,
		Pushed:  true,
		Message: msg,
	}, nil
}

func copyDir(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	// Prefer cp -a for .git (handles permissions/symlinks better than Walk+WriteFile).
	if _, err := exec.LookPath("cp"); err == nil {
		cmd := exec.Command("cp", "-a", src, dst)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("cp -a: %w (%s)", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}
