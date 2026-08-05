package claudeoauth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/alex/codegateway/internal/config"
)

// Constants aligned with OmniRoute CLAUDE_CONFIG / Claude Code OAuth.
const (
	DefaultClientID     = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	AuthorizeURL        = "https://claude.ai/oauth/authorize"
	TokenURL            = "https://api.anthropic.com/v1/oauth/token"
	PlatformRedirectURI = "https://platform.claude.com/oauth/code/callback"
	BootstrapURL        = "https://api.anthropic.com/api/claude_cli/bootstrap"
	DefaultScopes       = "org:create_api_key user:profile user:inference user:sessions:claude_code user:mcp_servers"
	// Pinned to OmniRoute CLAUDE_CODE_CLIENT_VERSION.
	ClaudeCodeVersion   = "2.1.219"
	ClaudeCodeUserAgent = "claude-cli/" + ClaudeCodeVersion + " (external, cli)"
	tokenSkew           = 5 * time.Minute
)

// Service manages Claude Code subscription OAuth per account.
type Service struct {
	db     *sql.DB
	cfg    config.ClaudeOAuthConfig
	client *http.Client
}

// Connection is a stored Claude OAuth link (tokens redacted in JSON).
type Connection struct {
	UserID           int64     `json:"user_id"`
	Connected        bool      `json:"connected"`
	Scopes           string    `json:"scopes"`
	SubscriptionType string    `json:"subscription_type,omitempty"`
	Email            string    `json:"email,omitempty"`
	DeviceID         string    `json:"-"`
	AccountUUID      string    `json:"-"`
	ExpiresAt        time.Time `json:"expires_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	AccessToken      string    `json:"-"`
	RefreshToken     string    `json:"-"`
}

// NewService creates a Claude OAuth service.
func NewService(db *sql.DB, cfg config.ClaudeOAuthConfig) *Service {
	if cfg.ClientID == "" {
		cfg.ClientID = DefaultClientID
	}
	if cfg.Scopes == "" {
		cfg.Scopes = DefaultScopes
	}
	if cfg.FrontendURL == "" {
		cfg.FrontendURL = "/settings"
	}
	// OmniRoute default: official platform callback (paste flow).
	if strings.TrimSpace(cfg.RedirectURL) == "" {
		cfg.RedirectURL = PlatformRedirectURI
	}
	return &Service{
		db:     db,
		cfg:    cfg,
		client: &http.Client{Timeout: 45 * time.Second},
	}
}

// Configured reports whether OAuth is enabled (public client needs no secret).
func (s *Service) Configured() bool {
	return s != nil && s.cfg.Enabled && s.cfg.ClientID != ""
}

// AuthorizeURL starts PKCE and returns the claude.ai authorize URL.
// OmniRoute-aligned: always uses the official platform redirect (paste code#state).
func (s *Service) AuthorizeURL(accountID int64, mode string) (string, error) {
	if !s.Configured() {
		return "", fmt.Errorf("claude oauth is not configured")
	}
	_ = mode
	redirectURI := PlatformRedirectURI
	if custom := strings.TrimSpace(s.cfg.RedirectURL); custom != "" && custom != PlatformRedirectURI {
		// Allow override via CLAUDE_CODE_REDIRECT_URI / yaml for advanced setups.
		redirectURI = custom
	}

	verifier, challenge, err := generatePKCE()
	if err != nil {
		return "", err
	}
	state, err := randomHex(16)
	if err != nil {
		return "", err
	}
	expires := time.Now().Add(15 * time.Minute)
	_, err = s.db.Exec(`
		INSERT INTO claude_oauth_states (state, user_id, code_verifier, redirect_uri, mode, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, state, accountID, verifier, redirectURI, "paste", expires, time.Now())
	if err != nil {
		return "", fmt.Errorf("failed to save oauth state: %w", err)
	}

	// Match OmniRoute buildAuthUrl params (incl. code=true, prompt=login).
	q := url.Values{}
	q.Set("code", "true")
	q.Set("client_id", s.cfg.ClientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", s.cfg.Scopes)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	q.Set("prompt", "login")
	return AuthorizeURL + "?" + q.Encode(), nil
}

// ExchangeCode validates state, exchanges for tokens, bootstraps profile, and upserts connection.
func (s *Service) ExchangeCode(ctx context.Context, code, state string) (*Connection, error) {
	if !s.Configured() {
		return nil, fmt.Errorf("claude oauth is not configured")
	}
	code, state = normalizePastCode(code, state)
	if code == "" || state == "" {
		return nil, fmt.Errorf("missing code or state")
	}

	var (
		userID      int64
		verifier    string
		redirectURI string
		expires     time.Time
	)
	err := s.db.QueryRow(`
		SELECT user_id, code_verifier, redirect_uri, expires_at
		FROM claude_oauth_states WHERE state = ?
	`, state).Scan(&userID, &verifier, &redirectURI, &expires)
	if err != nil {
		return nil, fmt.Errorf("invalid oauth state")
	}
	_, _ = s.db.Exec(`DELETE FROM claude_oauth_states WHERE state = ?`, state)
	if time.Now().After(expires) {
		return nil, fmt.Errorf("oauth state expired")
	}

	tok, err := s.exchangeAuthorizationCode(ctx, code, state, redirectURI, verifier)
	if err != nil {
		return nil, err
	}
	bs := s.fetchBootstrap(ctx, tok.AccessToken)
	return s.saveTokens(userID, tok, s.cfg.Scopes, bs)
}

// ExchangePastCode is used when the user pastes "code" or "code#state" after authorize.
func (s *Service) ExchangePastCode(ctx context.Context, accountID int64, pasted string) (*Connection, error) {
	pasted = strings.TrimSpace(pasted)
	code, state := normalizePastCode(pasted, "")
	if code == "" {
		return nil, fmt.Errorf("empty authorization code")
	}
	if state == "" {
		return nil, fmt.Errorf("paste must include state (format: code#state)")
	}

	var (
		userID      int64
		verifier    string
		redirectURI string
		expires     time.Time
	)
	err := s.db.QueryRow(`
		SELECT user_id, code_verifier, redirect_uri, expires_at
		FROM claude_oauth_states WHERE state = ?
	`, state).Scan(&userID, &verifier, &redirectURI, &expires)
	if err != nil {
		return nil, fmt.Errorf("invalid oauth state — start authorize first")
	}
	if userID != accountID {
		return nil, fmt.Errorf("oauth state does not belong to this account")
	}
	_, _ = s.db.Exec(`DELETE FROM claude_oauth_states WHERE state = ?`, state)
	if time.Now().After(expires) {
		return nil, fmt.Errorf("oauth state expired")
	}

	tok, err := s.exchangeAuthorizationCode(ctx, code, state, redirectURI, verifier)
	if err != nil {
		return nil, err
	}
	bs := s.fetchBootstrap(ctx, tok.AccessToken)
	return s.saveTokens(userID, tok, s.cfg.Scopes, bs)
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Scope        string `json:"scope"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

type bootstrapInfo struct {
	Email            string
	SubscriptionType string
	AccountUUID      string
}

func (s *Service) exchangeAuthorizationCode(ctx context.Context, code, state, redirectURI, verifier string) (*tokenResponse, error) {
	body := map[string]string{
		"grant_type":    "authorization_code",
		"code":          code,
		"state":         state,
		"redirect_uri":  redirectURI,
		"client_id":     s.cfg.ClientID,
		"code_verifier": verifier,
	}
	return s.postTokenJSON(ctx, body)
}

// fetchBootstrap mirrors OmniRoute postExchange (best-effort).
func (s *Service) fetchBootstrap(ctx context.Context, accessToken string) *bootstrapInfo {
	if accessToken == "" {
		return nil
	}
	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, BootstrapURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", ClaudeCodeUserAgent)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var data struct {
		OAuthAccount *struct {
			AccountUUID                string `json:"account_uuid"`
			AccountEmail               string `json:"account_email"`
			OrganizationType           string `json:"organization_type"`
			OrganizationRateLimitTier string `json:"organization_rate_limit_tier"`
		} `json:"oauth_account"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil || data.OAuthAccount == nil {
		return nil
	}
	acct := data.OAuthAccount
	sub := firstNonEmpty(acct.OrganizationRateLimitTier, acct.OrganizationType)
	return &bootstrapInfo{
		Email:            acct.AccountEmail,
		SubscriptionType: sub,
		AccountUUID:      acct.AccountUUID,
	}
}

// GetConnection returns connection metadata (without requiring a live token refresh).
func (s *Service) GetConnection(accountID int64) (*Connection, error) {
	log.Printf("[claude-oauth] GetConnection called: accountID=%d", accountID)
	var c Connection
	var expiresAt, updatedAt sql.NullTime
	err := s.db.QueryRow(`
		SELECT user_id, access_token, refresh_token, COALESCE(scopes,''), COALESCE(subscription_type,''),
		       COALESCE(device_id,''), COALESCE(account_uuid,''), expires_at, updated_at
		FROM claude_connections WHERE user_id = ?
	`, accountID).Scan(
		&c.UserID, &c.AccessToken, &c.RefreshToken, &c.Scopes, &c.SubscriptionType,
		&c.DeviceID, &c.AccountUUID, &expiresAt, &updatedAt,
	)
	if err != nil {
		log.Printf("[claude-oauth] GetConnection query/scan error: accountID=%d err=%v", accountID, err)
	}
	if err == sql.ErrNoRows {
		return &Connection{UserID: accountID, Connected: false}, nil
	}
	if err != nil {
		return nil, err
	}
	c.Connected = c.AccessToken != ""
	if expiresAt.Valid {
		c.ExpiresAt = expiresAt.Time
	}
	if updatedAt.Valid {
		c.UpdatedAt = updatedAt.Time
	}
	// Email may be embedded as "email|tier" in subscription_type for display.
	if parts := strings.SplitN(c.SubscriptionType, "|", 2); len(parts) == 2 && strings.Contains(parts[0], "@") {
		c.Email = parts[0]
		c.SubscriptionType = parts[1]
	}
	return &c, nil
}

// ValidAccessToken returns a non-expired access token, refreshing if needed.
func (s *Service) ValidAccessToken(ctx context.Context, accountID int64) (string, error) {
	creds, err := s.ValidCreds(ctx, accountID)
	if err != nil {
		return "", err
	}
	return creds.AccessToken, nil
}

// Creds is an OAuth access token plus Claude Code identity fields.
type Creds struct {
	AccessToken string
	DeviceID    string
	AccountUUID string
}

// ValidCreds returns a usable access token and persisted CLI identity.
func (s *Service) ValidCreds(ctx context.Context, accountID int64) (*Creds, error) {
	conn, err := s.GetConnection(accountID)
	if err != nil {
		return nil, err
	}
	if !conn.Connected {
		return nil, fmt.Errorf("claude oauth not connected")
	}
	if time.Until(conn.ExpiresAt) > tokenSkew {
		creds := &Creds{
			AccessToken: conn.AccessToken,
			DeviceID:    conn.DeviceID,
			AccountUUID: conn.AccountUUID,
		}
		s.ensureIdentity(accountID, creds, conn.AccessToken)
		return creds, nil
	}
	if conn.RefreshToken == "" {
		return nil, fmt.Errorf("claude oauth token expired; reconnect required")
	}
	tok, err := s.refresh(ctx, conn.RefreshToken)
	if err != nil {
		return nil, err
	}
	saved, err := s.saveTokens(accountID, tok, firstNonEmpty(tok.Scope, conn.Scopes), &bootstrapInfo{
		Email:            conn.Email,
		SubscriptionType: conn.SubscriptionType,
		AccountUUID:      conn.AccountUUID,
	})
	if err != nil {
		return nil, err
	}
	return &Creds{
		AccessToken: saved.AccessToken,
		DeviceID:    saved.DeviceID,
		AccountUUID: saved.AccountUUID,
	}, nil
}

// ensureIdentity backfills device_id / account_uuid for connections created before identity columns.
func (s *Service) ensureIdentity(accountID int64, creds *Creds, accessToken string) {
	changed := false
	if creds.DeviceID == "" {
		if id, err := randomHex(32); err == nil {
			creds.DeviceID = id
			changed = true
		}
	}
	if creds.AccountUUID == "" {
		creds.AccountUUID = uuidV4FromSeed("account:" + accessToken)
		changed = true
	}
	if !changed {
		return
	}
	_, _ = s.db.Exec(`
		UPDATE claude_connections
		SET device_id = CASE WHEN device_id = '' OR device_id IS NULL THEN ? ELSE device_id END,
		    account_uuid = CASE WHEN account_uuid = '' OR account_uuid IS NULL THEN ? ELSE account_uuid END,
		    updated_at = ?
		WHERE user_id = ?
	`, creds.DeviceID, creds.AccountUUID, time.Now(), accountID)
}

func (s *Service) refresh(ctx context.Context, refreshToken string) (*tokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", s.cfg.ClientID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token refresh failed: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token refresh status %d: %s", resp.StatusCode, string(raw))
	}
	var tok tokenResponse
	if err := json.Unmarshal(raw, &tok); err != nil {
		return nil, fmt.Errorf("invalid refresh response: %w", err)
	}
	if tok.Error != "" || tok.AccessToken == "" {
		return nil, fmt.Errorf("token refresh error: %s %s", tok.Error, tok.ErrorDesc)
	}
	if tok.RefreshToken == "" {
		tok.RefreshToken = refreshToken
	}
	if tok.ExpiresIn == 0 {
		tok.ExpiresIn = 3600
	}
	return &tok, nil
}

func (s *Service) postTokenJSON(ctx context.Context, body map[string]string) (*tokenResponse, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, TokenURL, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token exchange failed: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange status %d: %s", resp.StatusCode, string(raw))
	}
	var tok tokenResponse
	if err := json.Unmarshal(raw, &tok); err != nil {
		return nil, fmt.Errorf("invalid token response: %w", err)
	}
	if tok.Error != "" || tok.AccessToken == "" {
		return nil, fmt.Errorf("oauth error: %s %s", tok.Error, tok.ErrorDesc)
	}
	if tok.ExpiresIn == 0 {
		tok.ExpiresIn = 3600
	}
	return &tok, nil
}

func (s *Service) saveTokens(userID int64, tok *tokenResponse, scopes string, bs *bootstrapInfo) (*Connection, error) {
	now := time.Now()
	expires := now.Add(time.Duration(tok.ExpiresIn) * time.Second)
	if scopes == "" {
		scopes = tok.Scope
	}
	subType := ""
	email := ""
	accountUUID := ""
	if bs != nil {
		email = bs.Email
		subType = bs.SubscriptionType
		accountUUID = strings.TrimSpace(bs.AccountUUID)
		if email != "" && subType != "" {
			subType = email + "|" + subType
		} else if email != "" {
			subType = email
		}
	}

	// Preserve existing identity on refresh; generate device_id once (OmniRoute cliUserID).
	var existingDevice, existingAccount string
	_ = s.db.QueryRow(`
		SELECT COALESCE(device_id,''), COALESCE(account_uuid,'') FROM claude_connections WHERE user_id = ?
	`, userID).Scan(&existingDevice, &existingAccount)
	deviceID := existingDevice
	if deviceID == "" {
		var err error
		deviceID, err = randomHex(32) // 64 hex chars
		if err != nil {
			return nil, err
		}
	}
	if accountUUID == "" {
		accountUUID = existingAccount
	}
	if accountUUID == "" {
		accountUUID = uuidV4FromSeed("account:" + tok.AccessToken)
	}

	_, err := s.db.Exec(`
		INSERT INTO claude_connections (
			user_id, access_token, refresh_token, scopes, subscription_type,
			device_id, account_uuid, expires_at, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			access_token = excluded.access_token,
			refresh_token = excluded.refresh_token,
			scopes = excluded.scopes,
			subscription_type = CASE
				WHEN excluded.subscription_type != '' THEN excluded.subscription_type
				ELSE claude_connections.subscription_type
			END,
			device_id = CASE
				WHEN claude_connections.device_id != '' THEN claude_connections.device_id
				ELSE excluded.device_id
			END,
			account_uuid = CASE
				WHEN excluded.account_uuid != '' THEN excluded.account_uuid
				WHEN claude_connections.account_uuid != '' THEN claude_connections.account_uuid
				ELSE excluded.account_uuid
			END,
			expires_at = excluded.expires_at,
			updated_at = excluded.updated_at
	`, userID, tok.AccessToken, tok.RefreshToken, scopes, subType, deviceID, accountUUID, expires, now, now)
	if err != nil {
		return nil, fmt.Errorf("failed to save claude connection: %w", err)
	}
	conn := &Connection{
		UserID:           userID,
		Connected:        true,
		Scopes:           scopes,
		SubscriptionType: subType,
		Email:            email,
		DeviceID:         deviceID,
		AccountUUID:      accountUUID,
		ExpiresAt:        expires,
		UpdatedAt:        now,
		AccessToken:      tok.AccessToken,
		RefreshToken:     tok.RefreshToken,
	}
	if parts := strings.SplitN(conn.SubscriptionType, "|", 2); len(parts) == 2 && strings.Contains(parts[0], "@") {
		conn.Email = parts[0]
		conn.SubscriptionType = parts[1]
	}
	if saved, err := s.GetConnection(userID); err == nil && saved != nil {
		conn.DeviceID = firstNonEmpty(saved.DeviceID, deviceID)
		conn.AccountUUID = firstNonEmpty(saved.AccountUUID, accountUUID)
	}
	return conn, nil
}

// Disconnect removes the stored OAuth connection.
func (s *Service) Disconnect(accountID int64) error {
	_, err := s.db.Exec(`DELETE FROM claude_connections WHERE user_id = ?`, accountID)
	return err
}

// FrontendRedirect builds the post-OAuth redirect URL.
func (s *Service) FrontendRedirect(q url.Values) string {
	base := s.cfg.FrontendURL
	if base == "" {
		base = "/settings"
	}
	if strings.Contains(base, "?") {
		return base + "&" + q.Encode()
	}
	return base + "?" + q.Encode()
}

func normalizePastCode(code, state string) (string, string) {
	code = strings.TrimSpace(code)
	state = strings.TrimSpace(state)
	if strings.Contains(code, "#") {
		parts := strings.SplitN(code, "#", 2)
		code = strings.TrimSpace(parts[0])
		if state == "" && len(parts) > 1 {
			state = strings.TrimSpace(parts[1])
		}
	}
	return code, state
}

func generatePKCE() (verifier, challenge string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// uuidV4FromSeed mirrors OmniRoute uuidV4FromHash fallback for account_uuid.
func uuidV4FromSeed(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	hex64 := hex.EncodeToString(sum[:])
	nibble := 0
	if _, err := fmt.Sscanf(string(hex64[16]), "%x", &nibble); err != nil {
		nibble = 0
	}
	variant := fmt.Sprintf("%x", (nibble&0x3)|0x8)
	return fmt.Sprintf("%s-%s-4%s-%s%s-%s",
		hex64[0:8], hex64[8:12], hex64[13:16], variant, hex64[17:20], hex64[20:32])
}
