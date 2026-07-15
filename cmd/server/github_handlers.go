package server

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/alex/codegateway/internal/githubvcs"
	"github.com/alex/codegateway/internal/workspace"
	"github.com/gin-gonic/gin"
)

func handleGitHubStatus(gh *githubvcs.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}
		configured := gh != nil && gh.Configured()
		out := gin.H{
			"configured": configured,
			"connected":  false,
		}
		if !configured {
			c.JSON(http.StatusOK, out)
			return
		}
		conn, err := gh.GetConnection(accountID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		out["connected"] = conn.Connected
		out["github_login"] = conn.GitHubLogin
		out["github_user_id"] = conn.GitHubUserID
		out["scope"] = conn.Scope
		out["updated_at"] = conn.UpdatedAt
		c.JSON(http.StatusOK, out)
	}
}

func handleGitHubAuthorize(gh *githubvcs.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}
		if gh == nil || !gh.Configured() {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "GitHub OAuth 未配置。请在 codegateway.yaml 的 github 段或环境变量 GITHUB_CLIENT_ID / GITHUB_CLIENT_SECRET 中设置。",
			})
			return
		}
		authURL, err := gh.AuthorizeURL(accountID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if c.Query("redirect") == "1" {
			c.Redirect(http.StatusFound, authURL)
			return
		}
		c.JSON(http.StatusOK, gin.H{"authorize_url": authURL})
	}
}

func handleGitHubCallback(gh *githubvcs.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if gh == nil || !gh.Configured() {
			c.String(http.StatusServiceUnavailable, "GitHub OAuth not configured")
			return
		}
		if errMsg := c.Query("error"); errMsg != "" {
			q := url.Values{}
			q.Set("github", "error")
			q.Set("message", errMsg)
			c.Redirect(http.StatusFound, gh.FrontendRedirect(q))
			return
		}
		code := c.Query("code")
		state := c.Query("state")
		if code == "" || state == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "missing code or state"})
			return
		}
		conn, err := gh.ExchangeCode(c.Request.Context(), code, state)
		q := url.Values{}
		if err != nil {
			q.Set("github", "error")
			q.Set("message", err.Error())
			c.Redirect(http.StatusFound, gh.FrontendRedirect(q))
			return
		}
		q.Set("github", "connected")
		q.Set("login", conn.GitHubLogin)
		c.Redirect(http.StatusFound, gh.FrontendRedirect(q))
	}
}

func handleGitHubDisconnect(gh *githubvcs.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}
		if gh == nil {
			c.JSON(http.StatusOK, gin.H{"message": "disconnected"})
			return
		}
		if err := gh.Disconnect(accountID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "disconnected"})
	}
}

func handleGitHubListRepos(gh *githubvcs.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}
		if gh == nil || !gh.Configured() {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "GitHub OAuth 未配置"})
			return
		}
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "30"))
		repos, err := gh.ListRepos(c.Request.Context(), accountID, page, perPage)
		if err != nil {
			status := http.StatusBadRequest
			if strings.Contains(err.Error(), "not connected") {
				status = http.StatusUnauthorized
			}
			c.JSON(status, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"repos": repos})
	}
}

func handleGitHubImportRepo(gh *githubvcs.Service, workspaceMgr *workspace.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}
		if gh == nil || !gh.Configured() {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "GitHub OAuth 未配置"})
			return
		}

		var req struct {
			Owner  string `json:"owner" binding:"required"`
			Repo   string `json:"repo" binding:"required"`
			Branch string `json:"branch"`
			Name   string `json:"name"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		req.Owner = strings.TrimSpace(req.Owner)
		req.Repo = strings.TrimSpace(req.Repo)
		req.Branch = strings.TrimSpace(req.Branch)
		req.Name = strings.TrimSpace(req.Name)
		if req.Owner == "" || req.Repo == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "owner and repo are required"})
			return
		}
		fullName := req.Owner + "/" + req.Repo
		if req.Name == "" {
			req.Name = req.Repo
		}

		ws, err := workspaceMgr.CreateFromGitHub(accountID, req.Name, fullName, req.Branch)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		tmp, err := os.CreateTemp("", "gh-zipball-*.zip")
		if err != nil {
			_ = workspaceMgr.Delete(accountID, ws.ID)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create temp file"})
			return
		}
		tmpPath := tmp.Name()
		defer os.Remove(tmpPath)

		ref := req.Branch
		if ref == "" {
			ref = "HEAD"
		}
		if err := gh.DownloadZipball(c.Request.Context(), accountID, req.Owner, req.Repo, ref, tmp); err != nil {
			tmp.Close()
			_ = workspaceMgr.Delete(accountID, ws.ID)
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		tmp.Close()

		if err := unzipGitHubZipball(tmpPath, ws.RootPath); err != nil {
			_ = workspaceMgr.Delete(accountID, ws.ID)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to extract repository: " + err.Error()})
			return
		}
		_ = workspaceMgr.RefreshStats(ws)

		c.JSON(http.StatusOK, gin.H{"workspace": ws})
	}
}

// unzipGitHubZipball extracts a GitHub zipball, stripping the top-level folder
// (e.g. owner-repo-sha/).
func unzipGitHubZipball(zipPath, dest string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	destAbs, err := filepath.Abs(dest)
	if err != nil {
		return err
	}

	prefix := ""
	for _, f := range r.File {
		name := filepath.ToSlash(f.Name)
		if name == "" || strings.HasPrefix(name, "__MACOSX/") {
			continue
		}
		parts := strings.SplitN(name, "/", 2)
		if len(parts) == 0 {
			continue
		}
		prefix = parts[0] + "/"
		break
	}

	for _, f := range r.File {
		name := filepath.ToSlash(f.Name)
		if name == "" || strings.HasPrefix(name, "__MACOSX/") {
			continue
		}
		rel := name
		if prefix != "" && strings.HasPrefix(name, prefix) {
			rel = strings.TrimPrefix(name, prefix)
		}
		if rel == "" {
			continue
		}

		target := filepath.Join(destAbs, filepath.FromSlash(rel))
		targetAbs, err := filepath.Abs(target)
		if err != nil {
			return err
		}
		if targetAbs != destAbs && !strings.HasPrefix(targetAbs, destAbs+string(os.PathSeparator)) {
			return fmt.Errorf("zip path escapes destination: %s", name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(targetAbs, 0755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(targetAbs), 0755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(targetAbs)
		if err != nil {
			rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, rc)
		out.Close()
		rc.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	return nil
}
