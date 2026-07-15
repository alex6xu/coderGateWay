package server

import (
	"net/http"
	"strconv"

	"github.com/alex/codegateway/internal/account"
	"github.com/gin-gonic/gin"
)

// accountMiddleware resolves the active account from X-Account-ID (or falls back to default admin).
func accountMiddleware(mgr *account.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
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

	// Fallback: default admin account
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
		c.JSON(http.StatusBadRequest, gin.H{"error": "no account selected; set X-Account-ID header"})
		return 0, false
	}
	return id, true
}
