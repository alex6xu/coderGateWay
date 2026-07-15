package server

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/alex/codegateway/internal/account"
	"github.com/alex/codegateway/internal/agent/memory"
	"github.com/alex/codegateway/internal/config"
	"github.com/alex/codegateway/internal/db"
	"github.com/alex/codegateway/internal/githubvcs"
	"github.com/alex/codegateway/internal/workspace"
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

	// Ensure default admin account exists, then assign orphaned data
	accountMgr := account.NewManager(database.DB)
	defaultAccount, err := accountMgr.EnsureDefault()
	if err != nil {
		return fmt.Errorf("failed to ensure default account: %w", err)
	}
	if err := assignOrphanedData(database, defaultAccount.ID); err != nil {
		log.Printf("Warning: failed to assign orphaned data: %v", err)
	}
	log.Printf("Default account ready: %s (id=%d)", defaultAccount.Username, defaultAccount.ID)
	log.Printf("Auth: login with username=%s (default password from CODEGATEWAY_ADMIN_PASSWORD or %q)", account.DefaultUsername, account.DefaultAdminPassword)

	workspaceMgr := workspace.NewManager(database.DB, "./data/workspaces")
	memSvc := memory.NewMemoryService(database.DB)
	ghSvc := githubvcs.NewService(database.DB, cfg.GitHub)
	if ghSvc.Configured() {
		log.Printf("GitHub OAuth enabled (client_id=%s…)", trimID(cfg.GitHub.ClientID))
	} else {
		log.Printf("GitHub OAuth disabled (set github.client_id/secret or GITHUB_CLIENT_ID/SECRET)")
	}

	// Initialize default channels for the default account
	initDefaultChannels(database, cfg, defaultAccount.ID)

	// Setup Gin router
	r := gin.Default()

	// Create WebSocket hub
	hub := newWSHub()
	go hub.run()

	// Setup routes
	setupRoutes(r, database, cfg, hub, accountMgr, workspaceMgr, memSvc, ghSvc)

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

func setupRoutes(r *gin.Engine, database *db.DB, cfg *config.Config, hub *WSHub, accountMgr *account.Manager, workspaceMgr *workspace.Manager, memSvc *memory.MemoryService, ghSvc *githubvcs.Service) {
	// CORS middleware
	r.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Account-ID, X-Session-Token")
		c.Header("Access-Control-Allow-Credentials", "true")
		// CSP header for development
		c.Header("Content-Security-Policy", "script-src 'self' 'unsafe-eval' 'unsafe-inline'; style-src 'self' 'unsafe-inline'")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	// Resolve active account for all requests
	r.Use(accountMiddleware(accountMgr))

	// Health check
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	// WebSocket endpoint
	r.GET("/ws", handleWebSocket(database, cfg, hub))

	// API v1
	v1 := r.Group("/v1")
	{
		// Public auth endpoints
		auth := v1.Group("/auth")
		{
			auth.POST("/register", handleRegister(accountMgr))
			auth.POST("/login", handleLogin(accountMgr))
			auth.POST("/logout", handleLogout(accountMgr))
			auth.GET("/me", requireAuth(accountMgr), handleMe(accountMgr))
			auth.POST("/change-password", requireAuth(accountMgr), handleChangePassword(accountMgr))
		}

		// GitHub OAuth callback must be public (browser redirect); state binds to user.
		v1.GET("/github/callback", handleGitHubCallback(ghSvc))

		protected := v1.Group("")
		protected.Use(requireAuth(accountMgr))
		{
			protected.POST("/chat/completions", handleChatCompletions(database, cfg))
			protected.GET("/models", handleListModels(database))
			protected.GET("/models/*model", handleRetrieveModel(database))

			gateway := protected.Group("/gateway")
			{
				gateway.POST("/chat/completions", handleChatCompletions(database, cfg))
				gateway.GET("/models", handleListModels(database))
				gateway.GET("/models/*model", handleRetrieveModel(database))
				gateway.POST("/messages", handleClaudeMessages(database, cfg))
				gateway.POST("/v1beta/*path", handleGemini(database, cfg))
			}

			agent := protected.Group("/agent")
			{
				agent.POST("/chat", handleAgentChat(database, cfg, workspaceMgr, memSvc))
				agent.GET("/sessions", handleListSessions(database))
				agent.GET("/sessions/:id", handleGetSession(database))
			}

			wsAPI := protected.Group("/workspaces")
			{
				wsAPI.GET("", handleListWorkspaces(workspaceMgr))
				wsAPI.POST("/upload", handleUploadWorkspace(workspaceMgr))
				wsAPI.GET("/:id", handleGetWorkspace(workspaceMgr))
				wsAPI.DELETE("/:id", handleDeleteWorkspace(workspaceMgr))
				wsAPI.GET("/:id/tree", handleWorkspaceTree(workspaceMgr))
				wsAPI.GET("/:id/download", handleDownloadWorkspace(workspaceMgr))
			}

			ghAPI := protected.Group("/github")
			{
				ghAPI.GET("/status", handleGitHubStatus(ghSvc))
				ghAPI.GET("/authorize", handleGitHubAuthorize(ghSvc))
				ghAPI.DELETE("/disconnect", handleGitHubDisconnect(ghSvc))
				ghAPI.GET("/repos", handleGitHubListRepos(ghSvc))
				ghAPI.POST("/import", handleGitHubImportRepo(ghSvc, workspaceMgr))
			}

			protected.GET("/asr/status", handleASRStatus(cfg))
			protected.POST("/asr", handleASR(cfg))

			admin := protected.Group("/admin")
			{
				admin.GET("/stats", handleGetStats(database))

				admin.GET("/channels", handleListChannels(database))
				admin.POST("/channels", handleCreateChannel(database))
				admin.PUT("/channels/:id", handleUpdateChannel(database))
				admin.DELETE("/channels/:id", handleDeleteChannel(database))

				admin.GET("/accounts/current", handleGetCurrentAccount(accountMgr))

				// Account management is admin-only
				accounts := admin.Group("")
				accounts.Use(requireAdmin())
				{
					accounts.GET("/accounts", handleListAccounts(accountMgr))
					accounts.POST("/accounts", handleCreateAccount(accountMgr))
					accounts.GET("/accounts/:id", handleGetAccount(accountMgr))
					accounts.PUT("/accounts/:id", handleUpdateAccount(accountMgr))
					accounts.DELETE("/accounts/:id", handleDeleteAccount(accountMgr))
					accounts.GET("/users", handleListUsers(accountMgr))
					accounts.POST("/users", handleCreateUser(accountMgr))
				}

				admin.GET("/tokens", handleListTokens(database))
				admin.POST("/tokens", handleCreateToken(database))
			}
		}
	}
}

func trimID(id string) string {
	if len(id) <= 6 {
		return id
	}
	return id[:6]
}

func initDefaultChannels(database *db.DB, cfg *config.Config, accountID int64) {
	for _, ch := range cfg.Gateway.DefaultChannels {
		var exists int
		err := database.QueryRow(
			"SELECT COUNT(*) FROM channels WHERE name = ? AND user_id = ?",
			ch.Name, accountID,
		).Scan(&exists)
		if err != nil {
			log.Printf("Failed to check channel %s: %v", ch.Name, err)
			continue
		}
		if exists > 0 {
			continue
		}

		_, err = database.Exec(`
			INSERT INTO channels (user_id, name, type, key, base_url, models, weight, priority, status, groups, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, 'default', datetime('now'), datetime('now'))
		`, accountID, ch.Name, ch.Type, ch.Key, ch.BaseURL, ch.Models, ch.Weight, ch.Priority)
		if err != nil {
			log.Printf("Failed to create default channel %s: %v", ch.Name, err)
		} else {
			log.Printf("Created default channel: %s (account=%d)", ch.Name, accountID)
		}
	}
}

func assignOrphanedData(database *db.DB, accountID int64) error {
	if _, err := database.Exec("UPDATE channels SET user_id = ? WHERE user_id IS NULL", accountID); err != nil {
		return err
	}
	if _, err := database.Exec("UPDATE sessions SET user_id = ? WHERE user_id IS NULL", accountID); err != nil {
		return err
	}
	if _, err := database.Exec("UPDATE usage_logs SET user_id = ? WHERE user_id IS NULL", accountID); err != nil {
		return err
	}
	return nil
}
