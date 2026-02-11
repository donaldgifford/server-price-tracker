package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name      string
		yaml      string
		envVars   map[string]string
		wantErr   string
		checkFunc func(t *testing.T, cfg *Config)
	}{
		{
			name: "valid minimal config",
			yaml: `
database:
  host: localhost
  name: testdb
  user: testuser
llm:
  backend: ollama
  ollama:
    endpoint: http://localhost:11434
    model: mistral
`,
			checkFunc: func(t *testing.T, cfg *Config) {
				t.Helper()
				assert.Equal(t, "localhost", cfg.Database.Host)
				assert.Equal(t, "testdb", cfg.Database.Name)
				assert.Equal(t, "testuser", cfg.Database.User)
				assert.Equal(t, "ollama", cfg.LLM.Backend)
				assert.Equal(t, "http://localhost:11434", cfg.LLM.Ollama.Endpoint)
			},
		},
		{
			name: "defaults applied for optional fields",
			yaml: `
database:
  host: localhost
  name: testdb
  user: testuser
llm:
  backend: ollama
  ollama:
    endpoint: http://localhost:11434
`,
			checkFunc: func(t *testing.T, cfg *Config) {
				t.Helper()
				assert.Equal(t, "0.0.0.0", cfg.Server.Host)
				assert.Equal(t, 8080, cfg.Server.Port)
				assert.Equal(t, 30*time.Second, cfg.Server.ReadTimeout)
				assert.Equal(t, 30*time.Second, cfg.Server.WriteTimeout)
				assert.Equal(t, 5432, cfg.Database.Port)
				assert.Equal(t, "disable", cfg.Database.SSLMode)
				assert.Equal(t, 10, cfg.Database.PoolSize)
				assert.Equal(t, 4, cfg.LLM.Concurrency)
				assert.Equal(t, 30*time.Second, cfg.LLM.Timeout)
				assert.Equal(t, 10, cfg.Scoring.MinBaselineSamples)
				assert.Equal(t, 90, cfg.Scoring.BaselineWindowDays)
				assert.Equal(t, 15*time.Minute, cfg.Schedule.IngestionInterval)
				assert.Equal(t, 6*time.Hour, cfg.Schedule.BaselineInterval)
				assert.Equal(t, 30*time.Second, cfg.Schedule.StaggerOffset)
				assert.Equal(t, "info", cfg.Logging.Level)
				assert.Equal(t, "text", cfg.Logging.Format)
			},
		},
		{
			name: "env var substitution",
			yaml: `
database:
  host: localhost
  name: testdb
  user: testuser
  password: "${TEST_DB_PASSWORD}"
llm:
  backend: ollama
  ollama:
    endpoint: http://localhost:11434
`,
			envVars: map[string]string{
				"TEST_DB_PASSWORD": "secret123",
			},
			checkFunc: func(t *testing.T, cfg *Config) {
				t.Helper()
				assert.Equal(t, "secret123", cfg.Database.Password)
			},
		},
		{
			name: "missing required database.host",
			yaml: `
database:
  name: testdb
  user: testuser
llm:
  backend: ollama
  ollama:
    endpoint: http://localhost:11434
`,
			wantErr: "database.host is required",
		},
		{
			name: "missing required database.name",
			yaml: `
database:
  host: localhost
  user: testuser
llm:
  backend: ollama
  ollama:
    endpoint: http://localhost:11434
`,
			wantErr: "database.name is required",
		},
		{
			name: "missing required database.user",
			yaml: `
database:
  host: localhost
  name: testdb
llm:
  backend: ollama
  ollama:
    endpoint: http://localhost:11434
`,
			wantErr: "database.user is required",
		},
		{
			name: "invalid llm backend",
			yaml: `
database:
  host: localhost
  name: testdb
  user: testuser
llm:
  backend: invalid_backend
`,
			wantErr: `llm.backend must be one of: ollama, anthropic, openai_compat (got "invalid_backend")`,
		},
		{
			name: "ollama backend missing endpoint",
			yaml: `
database:
  host: localhost
  name: testdb
  user: testuser
llm:
  backend: ollama
`,
			wantErr: "llm.ollama.endpoint is required when backend is ollama",
		},
		{
			name: "anthropic backend missing model",
			yaml: `
database:
  host: localhost
  name: testdb
  user: testuser
llm:
  backend: anthropic
`,
			wantErr: "llm.anthropic.model is required when backend is anthropic",
		},
		{
			name: "openai_compat backend missing endpoint",
			yaml: `
database:
  host: localhost
  name: testdb
  user: testuser
llm:
  backend: openai_compat
`,
			wantErr: "llm.openai_compat.endpoint is required when backend is openai_compat",
		},
		{
			name:    "invalid YAML",
			yaml:    `{{{not valid yaml`,
			wantErr: "parsing config YAML",
		},
		{
			name: "anthropic backend valid config",
			yaml: `
database:
  host: localhost
  name: testdb
  user: testuser
llm:
  backend: anthropic
  anthropic:
    model: claude-haiku-4-20250514
`,
			checkFunc: func(t *testing.T, cfg *Config) {
				t.Helper()
				assert.Equal(t, "anthropic", cfg.LLM.Backend)
				assert.Equal(t, "claude-haiku-4-20250514", cfg.LLM.Anthropic.Model)
			},
		},
		{
			name: "full config with overrides",
			yaml: `
server:
  host: "127.0.0.1"
  port: 9090
  read_timeout: 60s
  write_timeout: 60s
database:
  host: db.example.com
  port: 5433
  name: tracker_prod
  user: admin
  password: pass
  sslmode: require
  pool_size: 20
ebay:
  app_id: my-app-id
  cert_id: my-cert-id
  marketplace: EBAY_US
  max_calls_per_cycle: 100
llm:
  backend: ollama
  ollama:
    endpoint: http://ollama:11434
    model: mistral:7b
  concurrency: 8
  timeout: 60s
  use_grammar: true
scoring:
  weights:
    price: 0.40
    seller: 0.20
    condition: 0.15
    quantity: 0.10
    quality: 0.10
    time: 0.05
  min_baseline_samples: 20
  baseline_window_days: 60
schedule:
  ingestion_interval: 30m
  baseline_interval: 12h
  stagger_offset: 1m
notifications:
  discord:
    enabled: true
    webhook_url: https://discord.com/api/webhooks/123
  webhook:
    enabled: false
logging:
  level: debug
  format: json
`,
			checkFunc: func(t *testing.T, cfg *Config) {
				t.Helper()
				assert.Equal(t, "127.0.0.1", cfg.Server.Host)
				assert.Equal(t, 9090, cfg.Server.Port)
				assert.Equal(t, 60*time.Second, cfg.Server.ReadTimeout)
				assert.Equal(t, "db.example.com", cfg.Database.Host)
				assert.Equal(t, 5433, cfg.Database.Port)
				assert.Equal(t, "require", cfg.Database.SSLMode)
				assert.Equal(t, 20, cfg.Database.PoolSize)
				assert.Equal(t, "my-app-id", cfg.Ebay.AppID)
				assert.Equal(t, "EBAY_US", cfg.Ebay.Marketplace)
				assert.Equal(t, 100, cfg.Ebay.MaxCallsPerCycle)
				assert.Equal(t, 8, cfg.LLM.Concurrency)
				assert.True(t, cfg.LLM.UseGrammar)
				assert.Equal(t, 0.40, cfg.Scoring.Weights.Price)
				assert.Equal(t, 20, cfg.Scoring.MinBaselineSamples)
				assert.Equal(t, 60, cfg.Scoring.BaselineWindowDays)
				assert.Equal(t, 30*time.Minute, cfg.Schedule.IngestionInterval)
				assert.True(t, cfg.Notifications.Discord.Enabled)
				assert.Equal(t, "https://discord.com/api/webhooks/123", cfg.Notifications.Discord.WebhookURL)
				assert.Equal(t, "debug", cfg.Logging.Level)
				assert.Equal(t, "json", cfg.Logging.Format)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Only parallelize tests that don't modify env vars.
			if len(tt.envVars) == 0 {
				t.Parallel()
			}

			// Set env vars for this test.
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			// Write YAML to a temp file.
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tt.yaml), 0o644))

			cfg, err := Load(path)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, cfg)

			if tt.checkFunc != nil {
				tt.checkFunc(t, cfg)
			}
		})
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	t.Parallel()

	_, err := Load("/nonexistent/path/config.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading config file")
}

func TestDatabaseConfig_DSN(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  DatabaseConfig
		want string
	}{
		{
			name: "basic DSN",
			cfg: DatabaseConfig{
				Host:     "localhost",
				Port:     5432,
				Name:     "testdb",
				User:     "testuser",
				Password: "testpass",
				SSLMode:  "disable",
			},
			want: "host=localhost port=5432 dbname=testdb user=testuser password=testpass sslmode=disable",
		},
		{
			name: "production DSN",
			cfg: DatabaseConfig{
				Host:     "db.example.com",
				Port:     5433,
				Name:     "tracker",
				User:     "admin",
				Password: "s3cret",
				SSLMode:  "require",
			},
			want: "host=db.example.com port=5433 dbname=tracker user=admin password=s3cret sslmode=require",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.cfg.DSN())
		})
	}
}
