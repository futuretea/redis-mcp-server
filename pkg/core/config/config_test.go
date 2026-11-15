package config_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"

	"github.com/futuretea/redis-mcp-server/pkg/core/config"
)

func TestLoadConfigDefaults(t *testing.T) {
	v := viper.New()
	v.Set("redis.topology", "standalone")
	v.Set("redis.addresses", []string{"127.0.0.1:6379"})
	cfg, err := config.LoadConfig("", v)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Port != 0 {
		t.Fatalf("expected port 0, got %d", cfg.Port)
	}
	if cfg.Listen != "127.0.0.1" {
		t.Fatalf("expected listen 127.0.0.1, got %q", cfg.Listen)
	}
	if cfg.LogLevel != "info" {
		t.Fatalf("expected log_level info, got %q", cfg.LogLevel)
	}
	if cfg.Redis.Topology != "standalone" || len(cfg.Redis.Addresses) != 1 {
		t.Fatalf("unexpected Redis defaults: %+v", cfg.Redis)
	}
	if cfg.AllowUnsafeDatabaseRole {
		t.Fatal("allow_unsafe_database_role default = true, want false")
	}
}

func TestLoadConfigAllowUnsafeDatabaseRoleFromEnvironment(t *testing.T) {
	t.Setenv("MCP_ALLOW_UNSAFE_DATABASE_ROLE", "true")
	v := viper.New()
	v.Set("redis.topology", "standalone")
	v.Set("redis.addresses", []string{"127.0.0.1:6379"})
	cfg, err := config.LoadConfig("", v)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.AllowUnsafeDatabaseRole {
		t.Fatal("AllowUnsafeDatabaseRole = false, want true from MCP_ALLOW_UNSAFE_DATABASE_ROLE")
	}
}

func TestLoadConfigRequiresExplicitRedisTopology(t *testing.T) {
	_, err := config.LoadConfig("", viper.New())
	if err == nil || !strings.Contains(err.Error(), "redis.topology") {
		t.Fatalf("LoadConfig error = %v, want explicit topology error", err)
	}
}

func TestRedisConfigValidatesSentinelAndReadsPasswordOnlyFromEnvironment(t *testing.T) {
	t.Setenv("MCP_REDIS_PASSWORD", "test-secret")
	v := viper.New()
	v.Set("redis.topology", "sentinel")
	v.Set("redis.addresses", []string{"127.0.0.1:26379"})

	_, err := config.LoadConfig("", v)
	if err == nil || !strings.Contains(err.Error(), "redis.sentinel_master") {
		t.Fatalf("LoadConfig sentinel error = %v, want missing master", err)
	}

	v.Set("redis.sentinel_master", "mymaster")
	cfg, err := config.LoadConfig("", v)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got := cfg.Redis.Password(); got != "test-secret" {
		t.Fatalf("Redis.Password() = %q, want environment value", got)
	}
	if strings.Contains(fmt.Sprintf("%+v", cfg), "test-secret") {
		t.Fatal("Redis password leaked through configuration formatting")
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config.StaticConfig
		wantSub string
	}{
		{
			name:    "bad port",
			cfg:     config.StaticConfig{Port: 70000, LogLevel: "info", Listen: "127.0.0.1"},
			wantSub: "port must be between",
		},
		{
			name:    "empty listen",
			cfg:     config.StaticConfig{Port: 8080, LogLevel: "info", Listen: " "},
			wantSub: "listen must be set",
		},
		{
			name:    "bad log level",
			cfg:     config.StaticConfig{Port: 0, LogLevel: "nope", Listen: "127.0.0.1"},
			wantSub: "invalid log_level",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tt.wantSub) {
				t.Fatalf("Validate() error = %v, want substring %q", err, tt.wantSub)
			}
		})
	}
}

func TestGetListenAddress(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.StaticConfig
		want string
	}{
		{name: "stdio", cfg: config.StaticConfig{Port: 0, Listen: "127.0.0.1"}, want: ""},
		{name: "IPv4", cfg: config.StaticConfig{Port: 8080, Listen: "127.0.0.1"}, want: "127.0.0.1:8080"},
		{name: "hostname", cfg: config.StaticConfig{Port: 8080, Listen: "localhost"}, want: "localhost:8080"},
		{name: "IPv6", cfg: config.StaticConfig{Port: 8080, Listen: "::1"}, want: "[::1]:8080"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.GetListenAddress(); got != tt.want {
				t.Fatalf("GetListenAddress() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoadConfigMaskingPrecedence(t *testing.T) {
	secret := "synthetic-token-value"
	t.Setenv("MCP_MASKING_JSON", "")
	os.Unsetenv("MCP_MASKING_JSON")
	dir := t.TempDir()
	path := filepath.Join(dir, "redis.yaml")
	body := []byte("log_level: info\nredis:\n  topology: standalone\n  addresses: [\"127.0.0.1:6379\"]\nmasking:\n  enabled: true\n  names: [\"synthetic_field\"]\n")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadConfig(path, viper.New())
	if err != nil || !cfg.Masking.Enabled() || !cfg.Masking.MatchName("synthetic_field") {
		t.Fatalf("file policy enabled=%v err=%v", cfg != nil && cfg.Masking.Enabled(), err)
	}

	t.Setenv("MCP_MASKING_JSON", `{"enabled":false,"values":["`+secret+`"]}`)
	cfg, err = config.LoadConfig(path, viper.New())
	if err != nil || cfg.Masking.Enabled() || strings.Contains(errString(err), secret) {
		t.Fatalf("env replacement enabled=%v err=%v", cfg != nil && cfg.Masking.Enabled(), err)
	}

	t.Setenv("MCP_MASKING_JSON", `{"enabled":true,"extra":"`+secret+`"}`)
	_, err = config.LoadConfig(path, viper.New())
	if err == nil || err.Error() != "invalid masking config" || strings.Contains(err.Error(), secret) {
		t.Fatalf("invalid masking error = %v", err)
	}

	t.Setenv("MCP_MASKING_JSON", " ")
	_, err = config.LoadConfig(path, viper.New())
	if err == nil || err.Error() != "invalid masking config" {
		t.Fatalf("empty masking env error = %v", err)
	}

	os.Unsetenv("MCP_MASKING_JSON")
	broken := filepath.Join(dir, "broken.yaml")
	if err := os.WriteFile(broken, []byte("masking: ["+secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = config.LoadConfig(broken, viper.New())
	if err == nil || strings.Contains(err.Error(), secret) || err.Error() != "invalid config" {
		t.Fatalf("yaml error = %v", err)
	}

	t.Setenv("MCP_REDIS_TOPOLOGY", secret)
	_, err = config.LoadConfig("", viper.New())
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("topology error = %v", err)
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
