package server

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/alex/codegateway/internal/config"
	"github.com/alex/codegateway/internal/db"
	"github.com/gin-gonic/gin"
)

func Run() error {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Initialize database
	database, err := db.Init(cfg.Database)
	if err != nil {
		return fmt.Errorf("failed to init database: %w", err)
	}
	defer database.Close()

	// Run migrations
	if err := db.Migrate(database); err != nil {
		return fmt.Errorf("failed to migrate database: %w", err)
	}

	// Setup Gin router
	r := gin.Default()

	// Setup routes
	setupRoutes(r, database, cfg)

	// Start server
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	log.Printf("Starting CodeGateway server on %s", addr)

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := r.Run(addr); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	<-quit
	log.Println("Shutting down server...")
	return nil
}

func setupRoutes(r *gin.Engine, database *db.DB, cfg *config.Config) {
	// Health check
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	// API v1
	v1 := r.Group("/v1")
	{
		// Gateway endpoints
		gateway := v1.Group("/gateway")
		{
			gateway.POST("/chat/completions", handleChatCompletions(database, cfg))
			gateway.POST("/messages", handleClaudeMessages(database, cfg))
			gateway.POST("/v1beta/*path", handleGemini(database, cfg))
		}

		// Agent endpoints
		agent := v1.Group("/agent")
		{
			agent.POST("/chat", handleAgentChat(database, cfg))
			agent.GET("/sessions", handleListSessions(database))
			agent.GET("/sessions/:id", handleGetSession(database))
		}

		// Admin endpoints
		admin := v1.Group("/admin")
		{
			admin.GET("/channels", handleListChannels(database))
			admin.POST("/channels", handleCreateChannel(database))
			admin.PUT("/channels/:id", handleUpdateChannel(database))
			admin.DELETE("/channels/:id", handleDeleteChannel(database))

			admin.GET("/users", handleListUsers(database))
			admin.POST("/users", handleCreateUser(database))

			admin.GET("/tokens", handleListTokens(database))
			admin.POST("/tokens", handleCreateToken(database))
		}
	}
}
