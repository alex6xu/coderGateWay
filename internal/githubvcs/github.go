package githubvcs

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/alex/codegateway/internal/config"
	"github.com/google/uuid"
)

// Service manages GitHub OAuth connections and API access per account.
type Service struct {
	db     *sql.DB
	cfg    config.GitHubConfig
	client *http.Client
}

// Connection is a stored GitHub OAuth link for an account.
type Connection struct {
	UserID        int64     `json:"user_id"`
	GitHubUserID  int64     `json:"github_user_id"`
	GitHubLogin   string    `json:"github_login"`
	Scope         string    `json:"scope"`
	Connected     bool      `json:"connected"`
	UpdatedAt     time.Time `json:"updated_at"`
	AccessToken   string    `json:"-"`
}

// Repo is a GitHub repository summary.
type Repo struct {
	ID            int64  `json:"id"`
	FullName      string `json:"full_name"`
	Name          string `json:"name"`
	Owner         string `json:"owner"`
	Private       bool   `json:"private"`
	Description   string `json:"description"`
	DefaultBranch string `json:"default_branch"`
	HTMLURL       string `json:"html_url"`
	CloneURL      string `json:"clone_url"`
	UpdatedAt     string `json:"updated_at"`
}

// NewService creates a GitHub VCS service.
func NewService(db *sql.DB, cfg config.GitHubConfig) *Service {
	if cfg.Scopes == "" {
		cfg.Scopes = "read:user repo"
	}
	return &Service{
		db:     db,
		cfg:    cfg,
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

// Configured reports whether OAuth credentials are present.
func (s *Service) Configured() bool {
	return s != nil && s.cfg.Enabled && s.cfg.ClientID != "" && s.cfg.ClientSecret != "" && s.cfg.RedirectURL != ""
}

// AuthorizeURL creates a CSRF state and returns the GitHub authorize URL.
func (s *Service) AuthorizeURL(accountID int64) (string, error) {
	if !s.Configured() {
		return "", fmt.Errorf("github oauth is not configured")
	}
	state := uuid.NewString()
	expires := time.Now().Add(10 * time.Minute)
	_, err := s.db.Exec(`
		INSERT INTO github_oauth_states (state, user_id, expires_at, created_at)
		VALUES (?, ?, ?, ?)
	`, state, accountID, expires, time.Now())
	if err != nil {
		return "", fmt.Errorf("failed to save oauth state: %w", err)
	}

	q := url.Values{}
	q.Set("client_id", s.cfg.ClientID)
	q.Set("redirect_uri", s.cfg.RedirectURL)
	q.Set("scope", s.cfg.Scopes)
	q.Set("state", state)
	q.Set("allow_signup", "true")
	return "https://github.com/login/oauth/authorize?" + q.Encode(), nil
}

// ExchangeCode validates state, exchanges code for token, and stores the connection.
func (s *Service) ExchangeCode(ctx context.Context, code, state string) (*Connection, error) {
	if !s.Configured() {
		return nil, fmt.Errorf("github oauth is not configured")
	}
	var userID int64
	var expires time.Time
	err := s.db.QueryRow(`
		SELECT user_id, expires_at FROM github_oauth_states WHERE state = ?
	`, state).Scan(&userID, &expires)
	if err != nil {
		return nil, fmt.Errorf("invalid oauth state")
	}
	_, _ = s.db.Exec(`DELETE FROM github_oauth_states WHERE state = ?`, state)
	if time.Now().After(expires) {
		return nil, fmt.Errorf("oauth state expired")
	}

	form := url.Values{}
	form.Set("client_id", s.cfg.ClientID)
	form.Set("client_secret", s.cfg.ClientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", s.cfg.RedirectURL)

	req, err := http.NewRequestWithContext(ctx, "POST", "https://github.com/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token exchange failed: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange status %d: %s", resp.StatusCode, string(body))
	}

	var tok struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		Scope       string `json:"scope"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return nil, fmt.Errorf("invalid token response: %w", err)
	}
	if tok.Error != "" || tok.AccessToken == "" {
		return nil, fmt.Errorf("github oauth error: %s %s", tok.Error, tok.ErrorDesc)
	}

	user, err := s.fetchUser(ctx, tok.AccessToken)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	_, err = s.db.Exec(`
		INSERT INTO github_connections (user_id, access_token, token_type, scope, github_user_id, github_login, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			access_token = excluded.access_token,
			token_type = excluded.token_type,
			scope = excluded.scope,
			github_user_id = excluded.github_user_id,
			github_login = excluded.github_login,
			updated_at = excluded.updated_at
	`, userID, tok.AccessToken, tok.TokenType, tok.Scope, user.ID, user.Login, now, now)
	if err != nil {
		return nil, fmt.Errorf("failed to save github connection: %w", err)
	}

	return &Connection{
		UserID:       userID,
		GitHubUserID: user.ID,
		GitHubLogin:  user.Login,
		Scope:        tok.Scope,
		Connected:    true,
		UpdatedAt:    now,
		AccessToken:  tok.AccessToken,
	}, nil
}

type ghUser struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
}

func (s *Service) fetchUser(ctx context.Context, token string) (*ghUser, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/user", nil)
	if err != nil {
		return nil, err
	}
	s.setAuth(req, token)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github user api %d: %s", resp.StatusCode, string(raw))
	}
	var u ghUser
	if err := json.Unmarshal(raw, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// GetConnection returns the connection status for an account.
func (s *Service) GetConnection(accountID int64) (*Connection, error) {
	var c Connection
	var token string
	err := s.db.QueryRow(`
		SELECT user_id, access_token, scope, github_user_id, github_login, updated_at
		FROM github_connections WHERE user_id = ?
	`, accountID).Scan(&c.UserID, &token, &c.Scope, &c.GitHubUserID, &c.GitHubLogin, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return &Connection{UserID: accountID, Connected: false}, nil
	}
	if err != nil {
		return nil, err
	}
	c.AccessToken = token
	c.Connected = token != ""
	return &c, nil
}

// Disconnect removes the stored token for an account.
func (s *Service) Disconnect(accountID int64) error {
	_, err := s.db.Exec(`DELETE FROM github_connections WHERE user_id = ?`, accountID)
	return err
}

// ListRepos returns repositories visible to the connected account.
func (s *Service) ListRepos(ctx context.Context, accountID int64, page, perPage int) ([]Repo, error) {
	conn, err := s.GetConnection(accountID)
	if err != nil {
		return nil, err
	}
	if !conn.Connected {
		return nil, fmt.Errorf("github not connected")
	}
	if page <= 0 {
		page = 1
	}
	if perPage <= 0 || perPage > 100 {
		perPage = 30
	}

	apiURL := fmt.Sprintf("https://api.github.com/user/repos?sort=updated&per_page=%d&page=%d&affiliation=owner,collaborator,organization_member", perPage, page)
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	s.setAuth(req, conn.AccessToken)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list repos failed (%d): %s", resp.StatusCode, string(raw))
	}

	var items []struct {
		ID            int64  `json:"id"`
		Name          string `json:"name"`
		FullName      string `json:"full_name"`
		Private       bool   `json:"private"`
		Description   string `json:"description"`
		DefaultBranch string `json:"default_branch"`
		HTMLURL       string `json:"html_url"`
		CloneURL      string `json:"clone_url"`
		UpdatedAt     string `json:"updated_at"`
		Owner         struct {
			Login string `json:"login"`
		} `json:"owner"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}

	out := make([]Repo, 0, len(items))
	for _, it := range items {
		out = append(out, Repo{
			ID:            it.ID,
			FullName:      it.FullName,
			Name:          it.Name,
			Owner:         it.Owner.Login,
			Private:       it.Private,
			Description:   it.Description,
			DefaultBranch: it.DefaultBranch,
			HTMLURL:       it.HTMLURL,
			CloneURL:      it.CloneURL,
			UpdatedAt:     it.UpdatedAt,
		})
	}
	return out, nil
}

// DownloadZipball downloads a repository archive into w.
func (s *Service) DownloadZipball(ctx context.Context, accountID int64, owner, repo, ref string, w io.Writer) error {
	conn, err := s.GetConnection(accountID)
	if err != nil {
		return err
	}
	if !conn.Connected {
		return fmt.Errorf("github not connected")
	}
	if ref == "" {
		ref = "HEAD"
	}
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/zipball/%s", url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(ref))
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return err
	}
	s.setAuth(req, conn.AccessToken)
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("zipball download failed (%d): %s", resp.StatusCode, string(raw))
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

// FrontendRedirect builds the post-OAuth redirect URL.
func (s *Service) FrontendRedirect(query url.Values) string {
	base := s.cfg.FrontendURL
	if base == "" {
		base = "/code"
	}
	if strings.HasPrefix(base, "http://") || strings.HasPrefix(base, "https://") {
		u, err := url.Parse(base)
		if err != nil {
			return base
		}
		q := u.Query()
		for k, vs := range query {
			for _, v := range vs {
				q.Add(k, v)
			}
		}
		u.RawQuery = q.Encode()
		return u.String()
	}
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	return base + sep + query.Encode()
}

func (s *Service) setAuth(req *http.Request, token string) {
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "CodeGateway")
}
