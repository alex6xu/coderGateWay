package server

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/alex/codegateway/internal/account"
	"github.com/alex/codegateway/internal/agent/memory"
	sessionrun "github.com/alex/codegateway/internal/agent/sessionrun"
	"github.com/alex/codegateway/internal/agent/tags"
	"github.com/alex/codegateway/internal/claudeoauth"
	"github.com/alex/codegateway/internal/config"
	"github.com/alex/codegateway/internal/db"
	"github.com/alex/codegateway/internal/githubvcs"
	"github.com/alex/codegateway/internal/workspace"
	"github.com/gin-gonic/gin"
)

// claudeOAuthSvc is set in Run and used by createProviderFromChannel for subscription auth.
var claudeOAuthSvc *claudeoauth.Service

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
	tagSvc := tags.NewService(database.DB)
	ghSvc := githubvcs.NewService(database.DB, cfg.GitHub)
	if ghSvc.Configured() {
		log.Printf("GitHub OAuth enabled (client_id=%s…)", trimID(cfg.GitHub.ClientID))
	} else {
		log.Printf("GitHub OAuth disabled (set github.client_id/secret or GITHUB_CLIENT_ID/SECRET)")
	}

	claudeOAuthSvc = claudeoauth.NewService(database.DB, cfg.ClaudeOAuth)
	if claudeOAuthSvc.Configured() {
		log.Printf("Claude OAuth enabled (client_id=%s…)", trimID(cfg.ClaudeOAuth.ClientID))
	} else {
		log.Printf("Claude OAuth disabled (set claude_oauth.enabled: true)")
	}

	// Initialize default channels for the default account
	initDefaultChannels(database, cfg, defaultAccount.ID)
	taskWorker := newAgentTaskWorker(database, workspaceMgr, cfg)
	if _, err := taskWorker.RecoverInterrupted(); err != nil {
		return fmt.Errorf("failed to recover interrupted agent tasks: %w", err)
	}
	sessionRT := newSessionRunRuntime(database, cfg, workspaceMgr, memSvc)
	sessionRunWorker := sessionrun.NewWorker(sessionrun.WorkerConfig{
		Store:   sessionRT.store,
		Execute: sessionRT.execute,
	})
	if _, err := sessionRunWorker.RecoverInterrupted(); err != nil {
		return fmt.Errorf("failed to recover interrupted session runs: %w", err)
	}
	workerCtx, cancelWorker := context.WithCancel(context.Background())
	defer cancelWorker()
	go func() {
		if err := taskWorker.Run(workerCtx); err != nil && workerCtx.Err() == nil {
			log.Printf("Agent Task worker stopped: %v", err)
		}
	}()
	go func() {
		if err := sessionRunWorker.Run(workerCtx); err != nil && workerCtx.Err() == nil {
			log.Printf("Session Run worker stopped: %v", err)
		}
	}()

	// Setup Gin router
	r := gin.Default()

	// Create WebSocket hub
	hub := newWSHub()
	go hub.run()

	// Setup routes
	setupRoutes(r, database, cfg, hub, accountMgr, workspaceMgr, memSvc, ghSvc, claudeOAuthSvc, tagSvc, sessionRT)

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

func setupRoutes(r *gin.Engine, database *db.DB, cfg *config.Config, hub *WSHub, accountMgr *account.Manager, workspaceMgr *workspace.Manager, memSvc *memory.MemoryService, ghSvc *githubvcs.Service, claudeOAuth *claudeoauth.Service, tagSvc *tags.Service, sessionRT *sessionRunRuntime) {
	// CORS middleware
	r.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Account-ID, X-Session-Token, X-API-Key, api-key")
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
	r.GET("/ws", handleWebSocket(database, cfg, hub, tagSvc))

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

		// GitHub / Claude OAuth callbacks must be public (browser redirect); state binds to user.
		v1.GET("/github/callback", handleGitHubCallback(ghSvc))
		v1.GET("/claude/oauth/callback", handleClaudeOAuthCallback(claudeOAuth))

		// Gateway proxy: login session token OR user API key (either is enough).
		gatewayAuth := requireSessionOrAPIKey(accountMgr)
		v1.POST("/chat/completions", gatewayAuth, handleChatCompletions(database, cfg))
		v1.GET("/models", gatewayAuth, handleListModels(database))
		v1.GET("/models/*model", gatewayAuth, handleRetrieveModel(database))
		gateway := v1.Group("/gateway")
		gateway.Use(gatewayAuth)
		{
			gateway.POST("/chat/completions", handleChatCompletions(database, cfg))
			gateway.GET("/models", handleListModels(database))
			gateway.GET("/models/*model", handleRetrieveModel(database))
			gateway.POST("/messages", handleClaudeMessages(database, cfg))
			gateway.POST("/v1beta/*path", handleGemini(database, cfg))
		}

		protected := v1.Group("")
		protected.Use(requireAuth(accountMgr))
		{
			agent := protected.Group("/agent")
			{
				agent.POST("/chat", handleAgentChat(database, cfg, workspaceMgr, memSvc, tagSvc, sessionRT))
				agent.POST("/tasks", handleCreateAgentTask(database))
				agent.GET("/tasks", handleListAgentTasks(database))
				agent.GET("/tasks/:id", handleGetAgentTask(database))
				agent.GET("/runs/:id/events", handleSessionRunEvents(sessionRT))
				agent.POST("/runs/:id/cancel", handleCancelSessionRun(sessionRT))
				agent.GET("/sessions", handleListSessions(database))
				agent.POST("/sessions/import/preview", handleImportMDPreview())
				agent.POST("/sessions/import", handleImportMDSession(database, tagSvc))
				agent.GET("/sessions/:id", handleGetSession(database, sessionRT))
				agent.GET("/tags", handleListTags(tagSvc))
				agent.GET("/tags/overview", handleTagOverview(tagSvc))
				agent.GET("/tags/:slug", handleGetTagMessages(tagSvc))
				agent.POST("/tags/retag", handleRetagMessages(tagSvc))
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

			claudeAPI := protected.Group("/claude/oauth")
			{
				claudeAPI.GET("/status", handleClaudeOAuthStatus(claudeOAuth))
				claudeAPI.GET("/authorize", handleClaudeOAuthAuthorize(claudeOAuth))
				claudeAPI.POST("/exchange", handleClaudeOAuthExchange(claudeOAuth))
				claudeAPI.DELETE("/disconnect", handleClaudeOAuthDisconnect(claudeOAuth))
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
				admin.PUT("/channels/:id/set-default", handleSetDefaultChannel(database))
				admin.POST("/channels/:id/fetch-models", handleFetchChannelModels(database))

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
			admin.PUT("/tokens/:id", handleUpdateToken(database))
			admin.DELETE("/tokens/:id", handleDeleteToken(database))

				admin.GET("/route-profiles", handleListRouteProfiles(database))
				admin.POST("/route-profiles", handleCreateRouteProfile(database))
				admin.PUT("/route-profiles/:id", handleUpdateRouteProfile(database))
				admin.DELETE("/route-profiles/:id", handleDeleteRouteProfile(database))

			admin.GET("/request-logs", handleListRequestLogs(database))
			admin.GET("/request-logs/:id", handleGetRequestLog(database))
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
