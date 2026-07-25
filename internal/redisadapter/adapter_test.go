package redisadapter

import (
	"testing"

	"github.com/redis/go-redis/v9"

	"github.com/futuretea/redis-mcp-server/pkg/core/config"
)

func TestNewSelectsOnlyExplicitTopology(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.RedisConfig
		want string
	}{
		{name: "standalone", cfg: config.RedisConfig{Topology: "standalone", Addresses: []string{"127.0.0.1:6379"}}, want: "standalone"},
		{name: "cluster", cfg: config.RedisConfig{Topology: "cluster", Addresses: []string{"127.0.0.1:6379"}}, want: "cluster"},
		{name: "sentinel", cfg: config.RedisConfig{Topology: "sentinel", Addresses: []string{"127.0.0.1:26379"}, SentinelMaster: "mymaster"}, want: "sentinel"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter, err := New(tt.cfg)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			defer func() { _ = adapter.Close() }()
			if got := adapter.Topology(); got != tt.want {
				t.Fatalf("Topology() = %q, want %q", got, tt.want)
			}
			switch client := adapter.client.(type) {
			case *redis.Client:
				if client.Options().MaxRetries != 0 {
					t.Fatalf("single-node MaxRetries = %d, want 0", client.Options().MaxRetries)
				}
			case *redis.ClusterClient:
				if client.Options().MaxRetries != -1 {
					t.Fatalf("cluster MaxRetries = %d, want -1", client.Options().MaxRetries)
				}
			}
		})
	}
}
