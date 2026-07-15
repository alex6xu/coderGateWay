package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/alex/codegateway/internal/account"
	"github.com/gin-gonic/gin"
)

func handleListAccounts(mgr *account.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		accounts, err := mgr.List()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list accounts"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"accounts": accounts})
	}
}

func handleCreateAccount(mgr *account.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Username string `json:"username" binding:"required"`
			Email    string `json:"email"`
			Role     string `json:"role"`
			Quota    int64  `json:"quota"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		u, err := mgr.Create(&account.CreateRequest{
			Username: req.Username,
			Email:    req.Email,
			Role:     req.Role,
			Quota:    req.Quota,
		})
		if err != nil {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "account created",
			"account": u,
		})
	}
}

func handleGetAccount(mgr *account.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid account id"})
			return
		}

		u, err := mgr.Get(id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"account": u})
	}
}

func handleUpdateAccount(mgr *account.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid account id"})
			return
		}

		var req struct {
			Username *string `json:"username"`
			Email    *string `json:"email"`
			Role     *string `json:"role"`
			Quota    *int64  `json:"quota"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		u, err := mgr.Update(id, &account.UpdateRequest{
			Username: req.Username,
			Email:    req.Email,
			Role:     req.Role,
			Quota:    req.Quota,
		})
		if err != nil {
			if err.Error() == "username cannot be empty" {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"message": "account updated",
			"account": u,
		})
	}
}

func handleDeleteAccount(mgr *account.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid account id"})
			return
		}

		if err := mgr.Delete(id); err != nil {
			status := http.StatusInternalServerError
			switch {
			case err.Error() == "cannot delete the default admin account":
				status = http.StatusForbidden
			case strings.Contains(err.Error(), "account not found"):
				status = http.StatusNotFound
			}
			c.JSON(status, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "account deleted"})
	}
}

func handleGetCurrentAccount(mgr *account.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := requireAccountID(c)
		if !ok {
			return
		}
		u, err := mgr.Get(id)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "account not found"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"account": u})
	}
}

// Legacy aliases kept for compatibility with earlier stubs.
func handleListUsers(mgr *account.Manager) gin.HandlerFunc {
	return handleListAccounts(mgr)
}

func handleCreateUser(mgr *account.Manager) gin.HandlerFunc {
	return handleCreateAccount(mgr)
}
