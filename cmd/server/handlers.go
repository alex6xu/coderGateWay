package server

import (
	"net/http"

	"github.com/alex/codegateway/internal/db"
	"github.com/alex/codegateway/internal/config"
	"github.com/gin-gonic/gin"
)

func handleChatCompletions(database *db.DB, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// TODO: Implement chat completions
		c.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented yet"})
	}
}

func handleClaudeMessages(database *db.DB, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// TODO: Implement Claude messages
		c.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented yet"})
	}
}

func handleGemini(database *db.DB, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// TODO: Implement Gemini
		c.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented yet"})
	}
}

func handleAgentChat(database *db.DB, cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// TODO: Implement agent chat
		c.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented yet"})
	}
}

func handleListSessions(database *db.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// TODO: Implement list sessions
		c.JSON(http.StatusOK, gin.H{"sessions": []interface{}{}})
	}
}

func handleGetSession(database *db.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// TODO: Implement get session
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
	}
}

func handleListChannels(database *db.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// TODO: Implement list channels
		c.JSON(http.StatusOK, gin.H{"channels": []interface{}{}})
	}
}

func handleCreateChannel(database *db.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// TODO: Implement create channel
		c.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented yet"})
	}
}

func handleUpdateChannel(database *db.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// TODO: Implement update channel
		c.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented yet"})
	}
}

func handleDeleteChannel(database *db.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// TODO: Implement delete channel
		c.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented yet"})
	}
}

func handleListUsers(database *db.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// TODO: Implement list users
		c.JSON(http.StatusOK, gin.H{"users": []interface{}{}})
	}
}

func handleCreateUser(database *db.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// TODO: Implement create user
		c.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented yet"})
	}
}

func handleListTokens(database *db.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// TODO: Implement list tokens
		c.JSON(http.StatusOK, gin.H{"tokens": []interface{}{}})
	}
}

func handleCreateToken(database *db.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		// TODO: Implement create token
		c.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented yet"})
	}
}
