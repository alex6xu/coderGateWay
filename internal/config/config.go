package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Redis    RedisConfig    `yaml:"redis"`
	Agent    AgentConfig    `yaml:"agent"`
	Gateway  GatewayConfig  `yaml:"gateway"`
	Platform PlatformConfig `yaml:"platforms"`
	Billing  BillingConfig  `yaml:"billing"`
}

type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
	Mode string `yaml:"mode"`
}

type DatabaseConfig struct {
	Driver string `yaml:"driver"`
	DSN    string `yaml:"dsn"`
}

type RedisConfig struct {
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

type AgentConfig struct {
	DefaultModel           string       `yaml:"default_model"`
	MaxIterations          int          `yaml:"max_iterations"`
	MaxTokens              int          `yaml:"max_tokens"` // completion / generation cap
	ContextBudgetTokens    int          `yaml:"context_budget_tokens"`
	HistoryMaxTurns        int          `yaml:"history_max_turns"`
	ToolResultMaxChars     int          `yaml:"tool_result_max_chars"`
	ToolResultKeepRecent   int          `yaml:"tool_result_keep_recent"`
	SummarizeEveryTurns    int          `yaml:"summarize_every_turns"`
	Temperature            float64      `yaml:"temperature"`
	PromptCacheEnabled     bool         `yaml:"prompt_cache_enabled"`
	ParallelReadonlyTools  bool         `yaml:"parallel_readonly_tools"`
	TreeHintLimit          int          `yaml:"tree_hint_limit"`
	MemoryConfig           MemoryConfig `yaml:"memory"`
	SkillsConfig           SkillsConfig `yaml:"skills"`
	CronConfig             CronConfig   `yaml:"cron"`
}

type MemoryConfig struct {
	Enabled           bool    `yaml:"enabled"`
	ReconcileOnSearch bool    `yaml:"reconcile_on_search"`
	ScoreFloor        float64 `yaml:"score_floor"`
	MaxSnippets       int     `yaml:"max_snippets"`
}

type SkillsConfig struct {
	BuiltinPath string `yaml:"builtin_path"`
	CustomPath  string `yaml:"custom_path"`
}

type CronConfig struct {
	Enabled      bool   `yaml:"enabled"`
	TickInterval string `yaml:"tick_interval"`
}

type GatewayConfig struct {
	Enabled         bool              `yaml:"enabled"`
	Routing         RoutingConfig     `yaml:"routing"`
	FreeProxies     []ProxyConfig     `yaml:"free_proxies"`
	DefaultChannels []DefaultChannel  `yaml:"default_channels"`
}

type DefaultChannel struct {
	Name     string `yaml:"name"`
	Type     int    `yaml:"type"`
	BaseURL  string `yaml:"base_url"`
	Key      string `yaml:"key"`
	Models   string `yaml:"models"`
	Weight   int    `yaml:"weight"`
	Priority int    `yaml:"priority"`
}

type RoutingConfig struct {
	Strategy        string `yaml:"strategy"`
	FallbackEnabled bool   `yaml:"fallback_enabled"`
	RetryCount      int    `yaml:"retry_count"`
}

type ProxyConfig struct {
	Name      string   `yaml:"name"`
	BaseURL   string   `yaml:"base_url"`
	Models    []string `yaml:"models"`
	RateLimit int      `yaml:"rate_limit"`
}

type PlatformConfig struct {
	Telegram TelegramConfig `yaml:"telegram"`
	Web      WebConfig      `yaml:"web"`
	Terminal TerminalConfig `yaml:"terminal"`
	WeChat   WeChatConfig   `yaml:"wechat"`
}

type TelegramConfig struct {
	Enabled  bool   `yaml:"enabled"`
	BotToken string `yaml:"bot_token"`
}

type WebConfig struct {
	Enabled     bool     `yaml:"enabled"`
	CORSOrigins []string `yaml:"cors_origins"`
}

type TerminalConfig struct {
	Enabled bool `yaml:"enabled"`
}

type WeChatConfig struct {
	Enabled   bool   `yaml:"enabled"`
	AppID     string `yaml:"app_id"`
	AppSecret string `yaml:"app_secret"`
	Token     string `yaml:"token"`
}

type BillingConfig struct {
	Enabled  bool                       `yaml:"enabled"`
	Currency string                     `yaml:"currency"`
	Pricing  map[string]ModelPricing    `yaml:"pricing"`
}

type ModelPricing struct {
	Input  float64 `yaml:"input"`
	Output float64 `yaml:"output"`
}

func Load() (*Config, error) {
	// Default config path
	configPath := "codegateway.yaml"
	if envPath := os.Getenv("CODEGATEWAY_CONFIG"); envPath != "" {
		configPath = envPath
	}

	// Check if config file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Return default config
		return defaultConfig(), nil
	}

	// Read config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Parse config
	cfg := defaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Ensure data directory exists
	dataDir := filepath.Dir(cfg.Database.DSN)
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	return cfg, nil
}

func defaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host: "0.0.0.0",
			Port: 8080,
			Mode: "debug",
		},
		Database: DatabaseConfig{
			Driver: "sqlite",
			DSN:    "./data/codegateway.db",
		},
		Redis: RedisConfig{
			Addr:     "localhost:6379",
			Password: "",
			DB:       0,
		},
		Agent: AgentConfig{
			DefaultModel:          "gpt-4o",
			MaxIterations:         50,
			MaxTokens:             4096,
			ContextBudgetTokens:   8000,
			HistoryMaxTurns:       8,
			ToolResultMaxChars:    4000,
			ToolResultKeepRecent:  2,
			SummarizeEveryTurns:   10,
			Temperature:           0.7,
			PromptCacheEnabled:    true,
			ParallelReadonlyTools: true,
			TreeHintLimit:         40,
			MemoryConfig: MemoryConfig{
				Enabled:           true,
				ReconcileOnSearch: true,
				ScoreFloor:        0.15,
				MaxSnippets:       5,
			},
			SkillsConfig: SkillsConfig{
				BuiltinPath: "./skills",
				CustomPath:  "~/.codegateway/skills",
			},
			CronConfig: CronConfig{
				Enabled:      true,
				TickInterval: "1m",
			},
		},
		Gateway: GatewayConfig{
			Enabled: true,
			Routing: RoutingConfig{
				Strategy:        "auto",
				FallbackEnabled: true,
				RetryCount:      3,
			},
		},
		Platform: PlatformConfig{
			Web: WebConfig{
				Enabled:     true,
				CORSOrigins: []string{"*"},
			},
			Terminal: TerminalConfig{
				Enabled: true,
			},
		},
		Billing: BillingConfig{
			Enabled:  true,
			Currency: "USD",
			Pricing: map[string]ModelPricing{
				"gpt-4o": {
					Input:  2.5,
					Output: 10,
				},
				"claude-3-5-sonnet": {
					Input:  3,
					Output: 15,
				},
				"deepseek-v3": {
					Input:  0.14,
					Output: 0.28,
				},
			},
		},
	}
}
