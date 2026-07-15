package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/alex/codegateway/internal/account"
	"github.com/gin-gonic/gin"
)

// accountMiddleware resolves the active account.
// Priority: authenticated session user > X-Account-ID (admin impersonation) > default admin.
func accountMiddleware(mgr *account.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		if token := extractSessionToken(c); token != "" {
			if user, err := mgr.GetSessionUser(token); err == nil {
				c.Set(account.SessionContextKey, token)
				c.Set(account.AuthUserContextKey, user.ID)
				c.Set(account.AuthRoleContextKey, user.Role)
				c.Set(account.ContextKey, user.ID)

				// Admins may impersonate another account via X-Account-ID.
				if user.Role == "admin" {
					if raw := c.GetHeader(account.HeaderName); raw != "" {
						if id, err := strconv.ParseInt(raw, 10, 64); err == nil && id > 0 {
							if _, err := mgr.Get(id); err == nil {
								c.Set(account.ContextKey, id)
							}
						}
					} else if raw := c.Query("account_id"); raw != "" {
						if id, err := strconv.ParseInt(raw, 10, 64); err == nil && id > 0 {
							if _, err := mgr.Get(id); err == nil {
								c.Set(account.ContextKey, id)
							}
						}
					}
				}

				c.Next()
				return
			}
		}

		// Unauthenticated fallback (legacy / public health paths).
		accountID := resolveAccountID(c, mgr)
		c.Set(account.ContextKey, accountID)
		c.Next()
	}
}

func resolveAccountID(c *gin.Context, mgr *account.Manager) int64 {
	raw := c.GetHeader(account.HeaderName)
	if raw == "" {
		raw = c.Query("account_id")
	}
	if raw != "" {
		if id, err := strconv.ParseInt(raw, 10, 64); err == nil && id > 0 {
			if _, err := mgr.Get(id); err == nil {
				return id
			}
		}
	}

	if admin, err := mgr.GetByUsername(account.DefaultUsername); err == nil {
		return admin.ID
	}
	return 0
}

func getAccountID(c *gin.Context) int64 {
	if v, ok := c.Get(account.ContextKey); ok {
		if id, ok := v.(int64); ok {
			return id
		}
	}
	return 0
}

func requireAccountID(c *gin.Context) (int64, bool) {
	id := getAccountID(c)
	if id == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no account selected; set X-Account-ID header or login"})
		return 0, false
	}
	return id, true
}

func extractSessionToken(c *gin.Context) string {
	if h := c.GetHeader(account.SessionHeader); h != "" {
		return strings.TrimSpace(h)
	}
	auth := c.GetHeader(account.AuthHeader)
	if auth != "" {
		const prefix = "Bearer "
		if strings.HasPrefix(auth, prefix) {
			return strings.TrimSpace(auth[len(prefix):])
		}
		return strings.TrimSpace(auth)
	}
	if q := c.Query("token"); q != "" {
		return strings.TrimSpace(q)
	}
	return ""
}

func requireAuthUserID(c *gin.Context) (int64, bool) {
	if v, ok := c.Get(account.AuthUserContextKey); ok {
		if id, ok := v.(int64); ok && id > 0 {
			return id, true
		}
	}
	c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
	return 0, false
}

func requireAuth(mgr *account.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := extractSessionToken(c)
		if token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
			return
		}
		user, err := mgr.GetSessionUser(token)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid or expired session"})
			return
		}
		c.Set(account.SessionContextKey, token)
		c.Set(account.AuthUserContextKey, user.ID)
		c.Set(account.AuthRoleContextKey, user.Role)
		// Keep active account aligned with auth user unless already set by middleware.
		if getAccountID(c) == 0 {
			c.Set(account.ContextKey, user.ID)
		}
		c.Next()
	}
}

func requireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		role, _ := c.Get(account.AuthRoleContextKey)
		roleStr, _ := role.(string)
		if roleStr != "admin" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "admin role required"})
			return
		}
		c.Next()
	}
}
