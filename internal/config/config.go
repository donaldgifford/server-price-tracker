// Package config handles loading and validating the application configuration
// from YAML files with environment variable substitution.
package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the top-level application configuration.
type Config struct {
	Server        ServerConfig        `yaml:"server"`
	Database      DatabaseConfig      `yaml:"database"`
	Ebay          EbayConfig          `yaml:"ebay"`
	LLM           LLMConfig           `yaml:"llm"`
	Scoring       ScoringConfig       `yaml:"scoring"`
	Schedule      ScheduleConfig      `yaml:"schedule"`
	Alerts        AlertsConfig        `yaml:"alerts"`
	Notifications NotificationsConfig `yaml:"notifications"`
	Logging       LoggingConfig       `yaml:"logging"`
}

// ServerConfig defines the Echo HTTP server settings.
type ServerConfig struct {
	Host         string        `yaml:"host"`
	Port         int           `yaml:"port"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
}

// DatabaseConfig defines PostgreSQL connection settings.
type DatabaseConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Name     string `yaml:"name"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	SSLMode  string `yaml:"sslmode"`
	PoolSize int    `yaml:"pool_size"`
}

// DSN returns a PostgreSQL connection string.
func (d *DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%d dbname=%s user=%s password=%s sslmode=%s",
		d.Host, d.Port, d.Name, d.User, d.Password, d.SSLMode,
	)
}

// EbayConfig defines eBay API settings.
type EbayConfig struct {
	AppID            string          `yaml:"app_id"`
	CertID           string          `yaml:"cert_id"`
	TokenURL         string          `yaml:"token_url"`
	BrowseURL        string          `yaml:"browse_url"`
	AnalyticsURL     string          `yaml:"analytics_url"`
	Marketplace      string          `yaml:"marketplace"`
	MaxCallsPerCycle int             `yaml:"max_calls_per_cycle"`
	RateLimit        RateLimitConfig `yaml:"rate_limit"`
}

// RateLimitConfig defines eBay API rate limiting settings.
type RateLimitConfig struct {
	PerSecond  float64 `yaml:"per_second"`
	Burst      int     `yaml:"burst"`
	DailyLimit int64   `yaml:"daily_limit"`
}

// LLMConfig defines LLM backend settings.
type LLMConfig struct {
	Backend      string             `yaml:"backend"` // ollama, anthropic, openai_compat
	Ollama       OllamaConfig       `yaml:"ollama"`
	Anthropic    AnthropicConfig    `yaml:"anthropic"`
	OpenAICompat OpenAICompatConfig `yaml:"openai_compat"`
	UseGrammar   bool               `yaml:"use_grammar"`
	Concurrency  int                `yaml:"concurrency"`
	Timeout      time.Duration      `yaml:"timeout"`
}

// OllamaConfig defines Ollama-specific settings.
type OllamaConfig struct {
	Endpoint string `yaml:"endpoint"`
	Model    string `yaml:"model"`
}

// AnthropicConfig defines Anthropic API settings.
type AnthropicConfig struct {
	Model string `yaml:"model"`
}

// OpenAICompatConfig defines OpenAI-compatible endpoint settings.
type OpenAICompatConfig struct {
	Endpoint string `yaml:"endpoint"`
	Model    string `yaml:"model"`
}

// ScoringConfig defines scoring weights and baseline parameters.
type ScoringConfig struct {
	Weights            ScoringWeights `yaml:"weights"`
	MinBaselineSamples int            `yaml:"min_baseline_samples"`
	BaselineWindowDays int            `yaml:"baseline_window_days"`
}

// ScoringWeights defines the relative weight of each scoring factor.
type ScoringWeights struct {
	Price     float64 `yaml:"price"`
	Seller    float64 `yaml:"seller"`
	Condition float64 `yaml:"condition"`
	Quantity  float64 `yaml:"quantity"`
	Quality   float64 `yaml:"quality"`
	Time      float64 `yaml:"time"`
}

// ScheduleConfig defines cron intervals.
type ScheduleConfig struct {
	IngestionInterval    time.Duration `yaml:"ingestion_interval"`
	BaselineInterval     time.Duration `yaml:"baseline_interval"`
	StaggerOffset        time.Duration `yaml:"stagger_offset"`
	ReExtractionInterval time.Duration `yaml:"re_extraction_interval"`
}

// NotificationsConfig defines notification targets.
type NotificationsConfig struct {
	Discord DiscordConfig `yaml:"discord"`
	Webhook WebhookConfig `yaml:"webhook"`
}

// DiscordConfig defines Discord webhook settings.
type DiscordConfig struct {
	Enabled    bool   `yaml:"enabled"`
	WebhookURL string `yaml:"webhook_url"`
}

// WebhookConfig defines generic webhook settings.
type WebhookConfig struct {
	Enabled bool              `yaml:"enabled"`
	URL     string            `yaml:"url"`
	Headers map[string]string `yaml:"headers"`
}

// AlertsConfig defines alert behavior.
type AlertsConfig struct {
	ReAlertsEnabled  bool          `yaml:"re_alerts_enabled"`  // default: false
	ReAlertsCooldown time.Duration `yaml:"re_alerts_cooldown"` // default: 24h
}

// LoggingConfig defines logging settings.
type LoggingConfig struct {
	Level  string `yaml:"level"`  // debug, info, warn, error
	Format string `yaml:"format"` // text, json
}

// Load reads and parses a YAML config file, performing environment variable
// substitution and validation.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path) //nolint:gosec // config path from trusted CLI flag
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	// Expand environment variables in the YAML content.
	expanded := os.ExpandEnv(string(data))

	cfg := &Config{}
	if err := yaml.Unmarshal([]byte(expanded), cfg); err != nil {
		return nil, fmt.Errorf("parsing config YAML: %w", err)
	}

	applyDefaults(cfg)

	if err := validate(cfg); err != nil {
		return nil, fmt.Errorf("validating config: %w", err)
	}

	return cfg, nil
}

func applyDefaults(cfg *Config) {
	applyServerDefaults(&cfg.Server)
	applyDatabaseDefaults(&cfg.Database)
	applyEbayDefaults(&cfg.Ebay)
	applyLLMDefaults(&cfg.LLM)
	applyScoringDefaults(&cfg.Scoring)
	applyScheduleDefaults(&cfg.Schedule)
	applyAlertsDefaults(&cfg.Alerts)
	applyLoggingDefaults(&cfg.Logging)
}

func applyEbayDefaults(e *EbayConfig) {
	if e.TokenURL == "" {
		e.TokenURL = "https://api.ebay.com/identity/v1/oauth2/token"
	}
	if e.BrowseURL == "" {
		e.BrowseURL = "https://api.ebay.com/buy/browse/v1/item_summary/search"
	}
	if e.AnalyticsURL == "" {
		e.AnalyticsURL = "https://api.ebay.com/developer/analytics/v1_beta/rate_limit/"
	}
	applyRateLimitDefaults(&e.RateLimit)
}

func applyRateLimitDefaults(r *RateLimitConfig) {
	if r.PerSecond == 0 {
		r.PerSecond = 5.0
	}
	if r.Burst == 0 {
		r.Burst = 10
	}
	if r.DailyLimit == 0 {
		r.DailyLimit = 5000
	}
}

func applyServerDefaults(s *ServerConfig) {
	if s.Host == "" {
		s.Host = "0.0.0.0"
	}
	if s.Port == 0 {
		s.Port = 8080
	}
	if s.ReadTimeout == 0 {
		s.ReadTimeout = 30 * time.Second
	}
	if s.WriteTimeout == 0 {
		s.WriteTimeout = 30 * time.Second
	}
}

func applyDatabaseDefaults(d *DatabaseConfig) {
	if d.Port == 0 {
		d.Port = 5432
	}
	if d.SSLMode == "" {
		d.SSLMode = "disable"
	}
	if d.PoolSize == 0 {
		d.PoolSize = 10
	}
}

func applyLLMDefaults(l *LLMConfig) {
	if l.Backend == "" {
		l.Backend = "ollama"
	}
	if l.Concurrency == 0 {
		l.Concurrency = 4
	}
	if l.Timeout == 0 {
		l.Timeout = 30 * time.Second
	}
}

func applyScoringDefaults(s *ScoringConfig) {
	if s.MinBaselineSamples == 0 {
		s.MinBaselineSamples = 10
	}
	if s.BaselineWindowDays == 0 {
		s.BaselineWindowDays = 90
	}
}

func applyScheduleDefaults(s *ScheduleConfig) {
	if s.IngestionInterval == 0 {
		s.IngestionInterval = 15 * time.Minute
	}
	if s.BaselineInterval == 0 {
		s.BaselineInterval = 6 * time.Hour
	}
	if s.StaggerOffset == 0 {
		s.StaggerOffset = 30 * time.Second
	}
}

func applyAlertsDefaults(a *AlertsConfig) {
	if a.ReAlertsCooldown == 0 {
		a.ReAlertsCooldown = 24 * time.Hour
	}
}

func applyLoggingDefaults(l *LoggingConfig) {
	if l.Level == "" {
		l.Level = "info"
	}
	if l.Format == "" {
		l.Format = "text"
	}
}

func validate(cfg *Config) error {
	var errs []error

	if cfg.Database.Host == "" {
		errs = append(errs, fmt.Errorf("database.host is required"))
	}
	if cfg.Database.Name == "" {
		errs = append(errs, fmt.Errorf("database.name is required"))
	}
	if cfg.Database.User == "" {
		errs = append(errs, fmt.Errorf("database.user is required"))
	}

	switch cfg.LLM.Backend {
	case "ollama":
		if cfg.LLM.Ollama.Endpoint == "" {
			errs = append(
				errs,
				fmt.Errorf("llm.ollama.endpoint is required when backend is ollama"),
			)
		}
	case "anthropic":
		// API key comes from env, model must be set.
		if cfg.LLM.Anthropic.Model == "" {
			errs = append(
				errs,
				fmt.Errorf("llm.anthropic.model is required when backend is anthropic"),
			)
		}
	case "openai_compat":
		if cfg.LLM.OpenAICompat.Endpoint == "" {
			errs = append(
				errs,
				fmt.Errorf("llm.openai_compat.endpoint is required when backend is openai_compat"),
			)
		}
	default:
		errs = append(
			errs,
			fmt.Errorf(
				"llm.backend must be one of: ollama, anthropic, openai_compat (got %q)",
				cfg.LLM.Backend,
			),
		)
	}

	return errors.Join(errs...)
}
