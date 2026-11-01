package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/rs/zerolog"
	"github.com/spf13/viper"
)

// StaticConfig contains configuration that is fixed when the server starts.
type StaticConfig struct {
	Port            int         `mapstructure:"port"`
	Listen          string      `mapstructure:"listen"`
	SSEBaseURL      string      `mapstructure:"sse_base_url"`
	LogLevel        string      `mapstructure:"log_level"`
	EnabledTools    []string    `mapstructure:"enabled_tools"`
	DisabledTools   []string    `mapstructure:"disabled_tools"`
	EnabledDomains  []string    `mapstructure:"enabled_domains"`
	DisabledDomains []string    `mapstructure:"disabled_domains"`
	Redis           RedisConfig `mapstructure:"redis"`

	// AllowUnsafeDatabaseRole skips only the startup Redis ACL privilege
	// rejection. Connection and ACL probe failures still abort startup.
	AllowUnsafeDatabaseRole bool `mapstructure:"allow_unsafe_database_role"`
}

// RedisConfig configures a single explicit Redis topology. Password is never
// part of this struct: it is read only from MCP_REDIS_PASSWORD at runtime.
type RedisConfig struct {
	Topology       string   `mapstructure:"topology"`
	Addresses      []string `mapstructure:"addresses"`
	Username       string   `mapstructure:"username"`
	TLS            bool     `mapstructure:"tls"`
	SentinelMaster string   `mapstructure:"sentinel_master"`
	WriteEnabled   bool     `mapstructure:"write_enabled"`
}

// Password returns the deployment-provided password without serializing or
// retaining it in configuration data.
func (c RedisConfig) Password() string {
	return os.Getenv("MCP_REDIS_PASSWORD")
}

// Validate checks Redis topology fields before a connection is constructed.
func (c *RedisConfig) Validate() error {
	c.Topology = strings.ToLower(strings.TrimSpace(c.Topology))
	c.Addresses = normalizeStringSlice(c.Addresses)
	c.Username = strings.TrimSpace(c.Username)
	c.SentinelMaster = strings.TrimSpace(c.SentinelMaster)

	switch c.Topology {
	case "standalone", "cluster", "sentinel":
	case "":
		return fmt.Errorf("redis.topology must be explicitly set to standalone, cluster, or sentinel")
	default:
		return fmt.Errorf("redis.topology must be standalone, cluster, or sentinel, got %q", c.Topology)
	}
	if len(c.Addresses) == 0 {
		return fmt.Errorf("redis.addresses must contain at least one address")
	}
	if c.Topology == "sentinel" && c.SentinelMaster == "" {
		return fmt.Errorf("redis.sentinel_master must be set for sentinel topology")
	}
	return nil
}

// Validate checks whether the configuration can be used to start the server.
func (c *StaticConfig) Validate() error {
	if c.Port < 0 || c.Port > 65535 {
		return fmt.Errorf("port must be between 0 and 65535, got %d", c.Port)
	}
	if c.Port != 0 && strings.TrimSpace(c.Listen) == "" {
		return fmt.Errorf("listen must be set when port is non-zero")
	}
	if _, err := zerolog.ParseLevel(c.LogLevel); err != nil {
		return fmt.Errorf("invalid log_level %q: %w", c.LogLevel, err)
	}
	if err := c.Redis.Validate(); err != nil {
		return err
	}
	return nil
}

// GetListenAddress returns the configured HTTP listen address (host:port).
func (c *StaticConfig) GetListenAddress() string {
	if c.Port == 0 {
		return ""
	}
	return net.JoinHostPort(c.Listen, strconv.Itoa(c.Port))
}

// LoadConfig loads configuration from defaults, optional YAML, environment, and flags.
func LoadConfig(configPath string, v *viper.Viper) (*StaticConfig, error) {
	if v == nil {
		v = viper.New()
	}

	defaults := map[string]any{
		"port":                       0,
		"listen":                     "127.0.0.1",
		"sse_base_url":               "",
		"log_level":                  "info",
		"enabled_tools":              []string{},
		"disabled_tools":             []string{},
		"enabled_domains":            []string{},
		"disabled_domains":           []string{},
		"allow_unsafe_database_role": false,
		"redis.topology":             "",
		"redis.addresses":            []string{},
		"redis.username":             "",
		"redis.tls":                  false,
		"redis.sentinel_master":      "",
		"redis.write_enabled":        false,
	}
	for key, value := range defaults {
		v.SetDefault(key, value)
	}

	if configPath != "" {
		v.SetConfigFile(configPath)
		v.SetConfigType("yaml")
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("read config file: %w", err)
		}
	}

	v.SetEnvPrefix("MCP")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_", "-", "_"))
	v.AutomaticEnv()

	cfg := &StaticConfig{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	cfg.Listen = strings.TrimSpace(cfg.Listen)
	cfg.LogLevel = strings.ToLower(strings.TrimSpace(cfg.LogLevel))
	cfg.EnabledTools = normalizeStringSlice(cfg.EnabledTools)
	cfg.DisabledTools = normalizeStringSlice(cfg.DisabledTools)
	cfg.EnabledDomains = normalizeStringSlice(cfg.EnabledDomains)
	cfg.DisabledDomains = normalizeStringSlice(cfg.DisabledDomains)

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func normalizeStringSlice(values []string) []string {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			normalized = append(normalized, value)
		}
	}
	return normalized
}
