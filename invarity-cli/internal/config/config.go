// Package config handles configuration loading from files, environment variables, and flags.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

// Config holds the CLI configuration.
type Config struct {
	Server      string `mapstructure:"server"`
	APIKey      string `mapstructure:"api_key"`
	OrgID       string `mapstructure:"org_id"`
	TenantID    string `mapstructure:"tenant_id"`
	PrincipalID string `mapstructure:"principal_id"`
	Env         string `mapstructure:"env"`
	ProjectID   string `mapstructure:"project_id"`
	ToolsetID   string `mapstructure:"toolset_id"`
	Trace       bool   `mapstructure:"trace"`
	JSON        bool   `mapstructure:"json"`
}

// DefaultConfig returns the default configuration.
func DefaultConfig() *Config {
	return &Config{
		Server:      "http://localhost:8080",
		APIKey:      "",
		OrgID:       "",
		TenantID:    "",
		PrincipalID: "",
		Env:         "sandbox",
		ProjectID:   "",
		ToolsetID:   "",
		Trace:       false,
		JSON:        false,
	}
}

// Load loads configuration with the following precedence (highest to lowest):
// 1. Command-line flags (handled by caller)
// 2. Environment variables (INVARITY_SERVER, INVARITY_API_KEY)
// 3. Config file (~/.invarity/config.yaml)
// 4. Defaults
func Load() (*Config, error) {
	cfg := DefaultConfig()

	// Set up viper
	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yaml")

	// Add config paths
	home, err := os.UserHomeDir()
	if err == nil {
		v.AddConfigPath(filepath.Join(home, ".invarity"))
	}
	v.AddConfigPath(".")

	// Environment variables
	v.SetEnvPrefix("INVARITY")
	v.AutomaticEnv()

	// Bind environment variables
	v.BindEnv("server", "INVARITY_SERVER")
	v.BindEnv("api_key", "INVARITY_API_KEY")
	v.BindEnv("org_id", "INVARITY_ORG_ID")
	v.BindEnv("tenant_id", "INVARITY_TENANT_ID")
	v.BindEnv("principal_id", "INVARITY_PRINCIPAL_ID")
	v.BindEnv("env", "INVARITY_ENV")
	v.BindEnv("project_id", "INVARITY_PROJECT_ID")
	v.BindEnv("toolset_id", "INVARITY_TOOLSET_ID")

	// Read config file (ignore if not found)
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			// Only return error if it's not a "file not found" error
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
	}

	// Unmarshal into config struct
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("error parsing config: %w", err)
	}

	return cfg, nil
}

// GetConfigDir returns the path to the config directory.
func GetConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("unable to find home directory: %w", err)
	}
	return filepath.Join(home, ".invarity"), nil
}

// EnsureConfigDir creates the config directory if it doesn't exist.
func EnsureConfigDir() error {
	dir, err := GetConfigDir()
	if err != nil {
		return err
	}
	return os.MkdirAll(dir, 0700)
}

// Validate checks if the configuration is valid for making API calls.
func (c *Config) Validate() error {
	if c.Server == "" {
		return fmt.Errorf("server URL is required")
	}
	return nil
}

// ValidateWithAuth checks if the configuration has authentication.
func (c *Config) ValidateWithAuth() error {
	if err := c.Validate(); err != nil {
		return err
	}
	if c.APIKey == "" {
		return fmt.Errorf("API key is required (set via --api-key, INVARITY_API_KEY, or config file)")
	}
	return nil
}

// ValidateForPolicy checks if the configuration is valid for policy operations.
func (c *Config) ValidateForPolicy() error {
	if err := c.ValidateWithAuth(); err != nil {
		return err
	}
	if c.OrgID == "" {
		return fmt.Errorf("org_id is required for policy operations (set via --org, INVARITY_ORG_ID, or config file)")
	}
	return nil
}

// ValidateForTools checks if the configuration is valid for tenant-scoped tool operations.
// Tools are registered to tenants (not principals). TenantID defaults to "default" if not set.
func (c *Config) ValidateForTools() error {
	if err := c.ValidateWithAuth(); err != nil {
		return err
	}
	// Note: TenantID is optional - defaults to "default" if not set
	return nil
}

// ValidateForToolsets checks if the configuration is valid for tenant-scoped toolset operations.
// Toolsets are registered to tenants. TenantID defaults to "default" if not set.
func (c *Config) ValidateForToolsets() error {
	if err := c.ValidateWithAuth(); err != nil {
		return err
	}
	// Note: TenantID is optional - defaults to "default" if not set
	return nil
}

// ValidateForPrincipal checks if the configuration is valid for principal-scoped operations.
// Used for applying toolsets to principals.
func (c *Config) ValidateForPrincipal() error {
	if err := c.ValidateWithAuth(); err != nil {
		return err
	}
	if c.PrincipalID == "" {
		return fmt.Errorf("principal_id is required for principal operations (set via --principal, INVARITY_PRINCIPAL_ID, or config file)")
	}
	return nil
}

// ValidEnv checks if the environment value is valid.
func ValidEnv(env string) bool {
	switch env {
	case "sandbox", "staging", "prod", "production":
		return true
	default:
		return false
	}
}

// NormalizeEnv normalizes environment names.
func NormalizeEnv(env string) string {
	if env == "production" {
		return "prod"
	}
	return env
}
