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
	Web           WebConfig           `yaml:"web"`
	Logging       LoggingConfig       `yaml:"logging"`
	Observability ObservabilityConfig `yaml:"observability"`
}

// WebConfig controls the embedded alert review UI.
//
// Enabled defaults to true so existing dev configs keep working without a
// schema bump; production deployments that don't want the UI surface set
// it to false. AlertsURLBase is the absolute URL prefix used to build
// deep-links from Discord summary embeds back to /alerts; empty means
// the link is omitted from the embed.
type WebConfig struct {
	Enabled       bool   `yaml:"enabled"`
	AlertsURLBase string `yaml:"alerts_url_base"`
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
	// InterChunkDelay is a defensive sleep between chunks beyond what
	// the response headers require. Zero (the default) means no extra
	// delay; bucket-driven waits still apply.
	InterChunkDelay time.Duration `yaml:"inter_chunk_delay"`
	// SummaryOnly collapses each scheduler tick into a single summary
	// embed regardless of pending alert count. Operators rely on the
	// /alerts page as the work surface when this is on.
	SummaryOnly bool `yaml:"summary_only"`
}

// WebhookConfig defines generic webhook settings.
type WebhookConfig struct {
	Enabled bool              `yaml:"enabled"`
	URL     string            `yaml:"url"`
	Headers map[string]string `yaml:"headers"`
}

// AlertsConfig defines alert behavior.
type AlertsConfig struct {
	// ReAlertsCooldown suppresses re-alerts on the same (watch, listing) within
	// this window. Default: 24h. Set to 0 to disable the cooldown entirely.
	ReAlertsCooldown time.Duration `yaml:"re_alerts_cooldown"`
}

// LoggingConfig defines logging settings.
type LoggingConfig struct {
	Level  string `yaml:"level"`  // debug, info, warn, error
	Format string `yaml:"format"` // text, json
}

// ObservabilityConfig groups the three independently disable-able
// observability subtrees added by DESIGN-0016 / IMPL-0019. Each subtree
// defaults to disabled so existing deployments are unaffected by the
// upgrade until they explicitly opt in.
type ObservabilityConfig struct {
	Otel     OtelConfig     `yaml:"otel"`
	Langfuse LangfuseConfig `yaml:"langfuse"`
	Judge    JudgeConfig    `yaml:"judge"`
}

// OtelConfig controls OpenTelemetry trace and metric emission. App always
// emits 100% of spans (AlwaysSample); sampling decisions live in the
// Collector's tail_sampling processor (platform-side). ServiceName is
// attached as a resource attribute alongside version + commit SHA.
type OtelConfig struct {
	Enabled     bool          `yaml:"enabled"`
	Endpoint    string        `yaml:"endpoint"` // OTLP/gRPC, e.g., "otel-collector:4317"
	ServiceName string        `yaml:"service_name"`
	Insecure    bool          `yaml:"insecure"` // skip TLS for local/dev collectors
	Timeout     time.Duration `yaml:"timeout"`
}

// LangfuseConfig controls the in-house Langfuse HTTP client wired around
// LLMBackend.Generate. The buffered client absorbs transient outages —
// see DESIGN-0016 Open Question 4 / IMPL-0019 Phase 3 for buffer
// semantics and the Prometheus metrics that expose buffer health.
type LangfuseConfig struct {
	Enabled    bool          `yaml:"enabled"`
	Endpoint   string        `yaml:"endpoint"`    // e.g., "https://langfuse.example.com"
	PublicKey  string        `yaml:"public_key"`  // pulled from env via os.ExpandEnv
	SecretKey  string        `yaml:"secret_key"`  // pulled from env via os.ExpandEnv
	BufferSize int           `yaml:"buffer_size"` // capacity of async write channel
	Timeout    time.Duration `yaml:"timeout"`     // per-write HTTP timeout
}

// JudgeConfig controls the async LLM-as-judge worker (IMPL-0019 Phase 5).
// When Enabled is false the worker isn't registered with the scheduler at
// all — startup behaviour matches today's deployment. Backend is an
// optional model override; empty falls through to the LLM extract
// backend's configured model so a Haiku upgrade auto-applies.
type JudgeConfig struct {
	Enabled        bool          `yaml:"enabled"`
	Backend        string        `yaml:"backend"` // "" means inherit from llm.backend
	Model          string        `yaml:"model"`   // "" means inherit from selected backend
	Interval       time.Duration `yaml:"interval"`
	Lookback       time.Duration `yaml:"lookback"`
	BatchSize      int           `yaml:"batch_size"`
	DailyBudgetUSD float64       `yaml:"daily_budget_usd"`
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
	applyObservabilityDefaults(&cfg.Observability)
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

func applyObservabilityDefaults(o *ObservabilityConfig) {
	applyOtelDefaults(&o.Otel)
	applyLangfuseDefaults(&o.Langfuse)
	applyJudgeDefaults(&o.Judge)
}

func applyOtelDefaults(o *OtelConfig) {
	if o.ServiceName == "" {
		o.ServiceName = "server-price-tracker"
	}
	if o.Timeout == 0 {
		o.Timeout = 10 * time.Second
	}
}

func applyLangfuseDefaults(l *LangfuseConfig) {
	if l.BufferSize == 0 {
		l.BufferSize = 1000
	}
	if l.Timeout == 0 {
		l.Timeout = 10 * time.Second
	}
}

func applyJudgeDefaults(j *JudgeConfig) {
	if j.Interval == 0 {
		j.Interval = 15 * time.Minute
	}
	if j.Lookback == 0 {
		j.Lookback = 6 * time.Hour
	}
	if j.BatchSize == 0 {
		j.BatchSize = 50
	}
	if j.DailyBudgetUSD == 0 {
		j.DailyBudgetUSD = 10.0
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
