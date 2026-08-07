package server

import (
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/alex/codegateway/internal/claudeoauth"
	"github.com/gin-gonic/gin"
)

func handleClaudeOAuthStatus(svc *claudeoauth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}
		configured := svc != nil && svc.Configured()
		out := gin.H{
			"configured": configured,
			"connected":  false,
		}
		if !configured {
			c.JSON(http.StatusOK, out)
			return
		}
		conn, err := svc.GetConnection(accountID)
		if err != nil {
			log.Printf("[claude-oauth-status] GetConnection failed: accountID=%d err=%v", accountID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		out["connected"] = conn.Connected
		out["scopes"] = conn.Scopes
		out["subscription_type"] = conn.SubscriptionType
		out["email"] = conn.Email
		out["expires_at"] = conn.ExpiresAt
		out["updated_at"] = conn.UpdatedAt
		c.JSON(http.StatusOK, out)
	}
}

func handleClaudeOAuthAuthorize(svc *claudeoauth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}
		if svc == nil || !svc.Configured() {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "Claude OAuth 未启用。请在 codegateway.yaml 设置 claude_oauth.enabled: true",
			})
			return
		}
		mode := c.DefaultQuery("mode", "paste")
		authURL, err := svc.AuthorizeURL(accountID, mode)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if c.Query("redirect") == "1" {
			c.Redirect(http.StatusFound, authURL)
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"authorize_url": authURL,
			"mode":          "paste",
			"hint":          "在新标签页授权后，把页面显示的 code#state 粘贴回 Settings / Channels",
		})
	}
}

func handleClaudeOAuthCallback(svc *claudeoauth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if svc == nil || !svc.Configured() {
			c.String(http.StatusServiceUnavailable, "Claude OAuth not configured")
			return
		}
		if errMsg := c.Query("error"); errMsg != "" {
			q := url.Values{}
			q.Set("claude_oauth", "error")
			q.Set("message", errMsg)
			c.Redirect(http.StatusFound, svc.FrontendRedirect(q))
			return
		}
		code := c.Query("code")
		state := c.Query("state")
		if code == "" || state == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "missing code or state"})
			return
		}
		_, err := svc.ExchangeCode(c.Request.Context(), code, state)
		q := url.Values{}
		if err != nil {
			q.Set("claude_oauth", "error")
			q.Set("message", err.Error())
			c.Redirect(http.StatusFound, svc.FrontendRedirect(q))
			return
		}
		q.Set("claude_oauth", "connected")
		c.Redirect(http.StatusFound, svc.FrontendRedirect(q))
	}
}

func handleClaudeOAuthExchange(svc *claudeoauth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}
		if svc == nil || !svc.Configured() {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Claude OAuth 未启用"})
			return
		}
		var req struct {
			Code string `json:"code"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		conn, err := svc.ExchangePastCode(c.Request.Context(), accountID, req.Code)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"message":    "connected",
			"expires_at": conn.ExpiresAt,
			"scopes":     conn.Scopes,
		})
	}
}

func handleClaudeOAuthDisconnect(svc *claudeoauth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		accountID, ok := requireAccountID(c)
		if !ok {
			return
		}
		if svc == nil {
			c.JSON(http.StatusOK, gin.H{"message": "disconnected"})
			return
		}
		if err := svc.Disconnect(accountID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "disconnected"})
	}
}

func normalizeProviderAuthMode(mode string, channelType int) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "oauth" {
		return "oauth"
	}
	return "api_key"
}
