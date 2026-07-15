package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/alex/codegateway/internal/account"
	"github.com/gin-gonic/gin"
)

func handleRegister(mgr *account.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Username string `json:"username" binding:"required"`
			Email    string `json:"email"`
			Password string `json:"password" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		u, err := mgr.Register(req.Username, req.Email, req.Password)
		if err != nil {
			status := http.StatusBadRequest
			msg := err.Error()
			if strings.Contains(msg, "UNIQUE") || strings.Contains(msg, "unique") {
				status = http.StatusConflict
				msg = "username or email already exists"
			}
			c.JSON(status, gin.H{"error": msg})
			return
		}

		sess, err := mgr.CreateSession(u.ID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "registered but failed to create session"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":    "registered",
			"token":      sess.Token,
			"expires_at": sess.ExpiresAt.Format(time.RFC3339),
			"account":    u,
		})
	}
}

func handleLogin(mgr *account.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Username string `json:"username" binding:"required"`
			Password string `json:"password" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		u, err := mgr.Authenticate(req.Username, req.Password)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			return
		}

		sess, err := mgr.CreateSession(u.ID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create session"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message":    "ok",
			"token":      sess.Token,
			"expires_at": sess.ExpiresAt.Format(time.RFC3339),
			"account":    u,
		})
	}
}

func handleLogout(mgr *account.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := extractSessionToken(c)
		if token != "" {
			_ = mgr.DeleteSession(token)
		}
		c.JSON(http.StatusOK, gin.H{"message": "logged out"})
	}
}

func handleMe(mgr *account.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := requireAuthUserID(c)
		if !ok {
			return
		}
		u, err := mgr.Get(userID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"account": u})
	}
}

func handleChangePassword(mgr *account.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, ok := requireAuthUserID(c)
		if !ok {
			return
		}

		var req struct {
			CurrentPassword string `json:"current_password" binding:"required"`
			NewPassword     string `json:"new_password" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		u, err := mgr.Get(userID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
			return
		}
		if !account.CheckPassword(u.PasswordHash, req.CurrentPassword) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "current password is incorrect"})
			return
		}

		if err := mgr.SetPassword(userID, req.NewPassword); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		sess, err := mgr.CreateSession(userID)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{"message": "password updated; please login again"})
			return
		}

		fresh, _ := mgr.Get(userID)
		c.JSON(http.StatusOK, gin.H{
			"message":    "password updated",
			"token":      sess.Token,
			"expires_at": sess.ExpiresAt.Format(time.RFC3339),
			"account":    fresh,
		})
	}
}
