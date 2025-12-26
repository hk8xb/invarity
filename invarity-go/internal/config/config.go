// Package config handles configuration parsing and validation.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all configuration for the Invarity Firewall.
type Config struct {
	// Server settings
	Port     int
	LogLevel string

	// AWS settings
	S3Bucket  string
	AWSRegion string

	// Cognito settings
	CognitoIssuer   string // e.g., https://cognito-idp.us-east-1.amazonaws.com/us-east-1_xxxxx
	CognitoAudience string // Cognito app client ID
	CognitoEnabled  bool   // Whether to enable Cognito JWT verification

	// DynamoDB table names
	DDBTableTenants     string
	DDBTableUsers       string
	DDBTableMemberships string
	DDBTablePrincipals  string
	DDBTableTokens      string

	// LLM endpoints
	FunctionGemmaBaseURL string
	FunctionGemmaAPIKey  string
	LlamaGuardBaseURL    string
	LlamaGuardAPIKey     string
	QwenBaseURL          string
	QwenAPIKey           string

	// Intent alignment model (RunPod)
	IntentModelEndpoint string
	IntentModelAPIKey   string
	IntentModelTimeout  time.Duration

	// Request limits
	RequestMaxBytes int
	MaxContextChars int
	MaxIntentChars  int

	// Cache settings
	CacheTTL time.Duration

	// Feature flags
	EnableThreatSentinel bool
	EnableControlPlane   bool // Whether to enable control plane endpoints (onboarding, etc.)
}

// DefaultConfig returns a configuration with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Port:                 8080,
		LogLevel:             "info",
		S3Bucket:             "",
		AWSRegion:            "us-east-1",
		CognitoIssuer:        "",
		CognitoAudience:      "",
		CognitoEnabled:       false,
		DDBTableTenants:      "invarity-tenants",
		DDBTableUsers:        "invarity-users",
		DDBTableMemberships:  "invarity-memberships",
		DDBTablePrincipals:   "invarity-principals",
		DDBTableTokens:       "invarity-tokens",
		FunctionGemmaBaseURL: "http://localhost:8001/v1",
		FunctionGemmaAPIKey:  "",
		LlamaGuardBaseURL:    "http://localhost:8002/v1",
		LlamaGuardAPIKey:     "",
		QwenBaseURL:          "http://localhost:8003/v1",
		QwenAPIKey:           "",
		IntentModelEndpoint:  "", // Set via INTENT_MODEL_ENDPOINT env var
		IntentModelAPIKey:    "",
		IntentModelTimeout:   1500 * time.Millisecond,
		RequestMaxBytes:      1 << 20, // 1MB
		MaxContextChars:      32000,
		MaxIntentChars:       4000,
		CacheTTL:             5 * time.Minute,
		EnableThreatSentinel: true,
		EnableControlPlane:   false,
	}
}

// LoadFromEnv loads configuration from environment variables.
func LoadFromEnv() (*Config, error) {
	cfg := DefaultConfig()

	if v := os.Getenv("PORT"); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid PORT: %w", err)
		}
		cfg.Port = port
	}

	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}

	if v := os.Getenv("S3_BUCKET"); v != "" {
		cfg.S3Bucket = v
	}

	if v := os.Getenv("AWS_REGION"); v != "" {
		cfg.AWSRegion = v
	}

	if v := os.Getenv("FUNCTIONGEMMA_BASE_URL"); v != "" {
		cfg.FunctionGemmaBaseURL = v
	}

	if v := os.Getenv("FUNCTIONGEMMA_API_KEY"); v != "" {
		cfg.FunctionGemmaAPIKey = v
	}

	if v := os.Getenv("LLAMAGUARD_BASE_URL"); v != "" {
		cfg.LlamaGuardBaseURL = v
	}

	if v := os.Getenv("LLAMAGUARD_API_KEY"); v != "" {
		cfg.LlamaGuardAPIKey = v
	}

	if v := os.Getenv("QWEN_BASE_URL"); v != "" {
		cfg.QwenBaseURL = v
	}

	if v := os.Getenv("QWEN_API_KEY"); v != "" {
		cfg.QwenAPIKey = v
	}

	if v := os.Getenv("INTENT_MODEL_ENDPOINT"); v != "" {
		cfg.IntentModelEndpoint = v
	}

	if v := os.Getenv("INTENT_MODEL_API_KEY"); v != "" {
		cfg.IntentModelAPIKey = v
	}

	if v := os.Getenv("INTENT_MODEL_TIMEOUT_MS"); v != "" {
		timeout, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid INTENT_MODEL_TIMEOUT_MS: %w", err)
		}
		cfg.IntentModelTimeout = time.Duration(timeout) * time.Millisecond
	}

	if v := os.Getenv("REQUEST_MAX_BYTES"); v != "" {
		maxBytes, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid REQUEST_MAX_BYTES: %w", err)
		}
		cfg.RequestMaxBytes = maxBytes
	}

	if v := os.Getenv("MAX_CONTEXT_CHARS"); v != "" {
		maxChars, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid MAX_CONTEXT_CHARS: %w", err)
		}
		cfg.MaxContextChars = maxChars
	}

	if v := os.Getenv("MAX_INTENT_CHARS"); v != "" {
		maxChars, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid MAX_INTENT_CHARS: %w", err)
		}
		cfg.MaxIntentChars = maxChars
	}

	if v := os.Getenv("CACHE_TTL_SECONDS"); v != "" {
		ttl, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid CACHE_TTL_SECONDS: %w", err)
		}
		cfg.CacheTTL = time.Duration(ttl) * time.Second
	}

	if v := os.Getenv("ENABLE_THREAT_SENTINEL"); v != "" {
		cfg.EnableThreatSentinel = v == "true" || v == "1"
	}

	// Cognito settings
	if v := os.Getenv("INVARITY_COGNITO_ISSUER"); v != "" {
		cfg.CognitoIssuer = v
		cfg.CognitoEnabled = true // Auto-enable when issuer is set
	}

	if v := os.Getenv("INVARITY_COGNITO_AUDIENCE"); v != "" {
		cfg.CognitoAudience = v
	}

	if v := os.Getenv("INVARITY_COGNITO_ENABLED"); v != "" {
		cfg.CognitoEnabled = v == "true" || v == "1"
	}

	// Override AWS region if INVARITY-specific var is set
	if v := os.Getenv("INVARITY_AWS_REGION"); v != "" {
		cfg.AWSRegion = v
	}

	// DynamoDB table names
	if v := os.Getenv("INVARITY_DDB_TABLE_TENANTS"); v != "" {
		cfg.DDBTableTenants = v
	}

	if v := os.Getenv("INVARITY_DDB_TABLE_USERS"); v != "" {
		cfg.DDBTableUsers = v
	}

	if v := os.Getenv("INVARITY_DDB_TABLE_MEMBERSHIPS"); v != "" {
		cfg.DDBTableMemberships = v
	}

	if v := os.Getenv("INVARITY_DDB_TABLE_PRINCIPALS"); v != "" {
		cfg.DDBTablePrincipals = v
	}

	if v := os.Getenv("INVARITY_DDB_TABLE_TOKENS"); v != "" {
		cfg.DDBTableTokens = v
	}

	// Control plane feature flag
	if v := os.Getenv("INVARITY_ENABLE_CONTROL_PLANE"); v != "" {
		cfg.EnableControlPlane = v == "true" || v == "1"
	}

	return cfg, cfg.Validate()
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}

	if c.RequestMaxBytes < 1024 {
		return fmt.Errorf("REQUEST_MAX_BYTES must be at least 1024")
	}

	if c.MaxContextChars < 100 {
		return fmt.Errorf("MAX_CONTEXT_CHARS must be at least 100")
	}

	if c.MaxIntentChars < 10 {
		return fmt.Errorf("MAX_INTENT_CHARS must be at least 10")
	}

	validLogLevels := map[string]bool{
		"debug": true, "info": true, "warn": true, "error": true,
	}
	if !validLogLevels[c.LogLevel] {
		return fmt.Errorf("LOG_LEVEL must be one of: debug, info, warn, error")
	}

	return nil
}
