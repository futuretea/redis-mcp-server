//go:build integration

package redisadapter

import (
	"context"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/futuretea/redis-mcp-server/pkg/core/config"
	redistoolset "github.com/futuretea/redis-mcp-server/pkg/toolset/redis"
)

const integrationDockerNetworkEnv = "REDIS_MCP_INTEGRATION_DOCKER_NETWORK"

func integrationRedisConfig(topology string) config.RedisConfig {
	if os.Getenv(integrationDockerNetworkEnv) == "1" {
		switch topology {
		case "standalone":
			return config.RedisConfig{Topology: "standalone", Addresses: []string{"standalone:6379"}}
		case "sentinel":
			return config.RedisConfig{Topology: "sentinel", Addresses: []string{"sentinel:26379"}, SentinelMaster: "mymaster"}
		case "cluster":
			return config.RedisConfig{Topology: "cluster", Addresses: []string{"cluster-1:6379", "cluster-2:6379", "cluster-3:6379"}}
		}
	}

	switch topology {
	case "standalone":
		return config.RedisConfig{Topology: "standalone", Addresses: []string{"127.0.0.1:16379"}}
	case "sentinel":
		return config.RedisConfig{Topology: "sentinel", Addresses: []string{"127.0.0.1:26379"}, SentinelMaster: "mymaster"}
	case "cluster":
		return config.RedisConfig{Topology: "cluster", Addresses: []string{"127.0.0.1:17001", "127.0.0.1:17002", "127.0.0.1:17003"}}
	default:
		panic("unknown integration topology: " + topology)
	}
}

func TestDockerNetworkConfigUsesReachableServiceEndpoints(t *testing.T) {
	t.Setenv(integrationDockerNetworkEnv, "1")

	testCases := []struct {
		topology string
		want     config.RedisConfig
	}{
		{"standalone", config.RedisConfig{Topology: "standalone", Addresses: []string{"standalone:6379"}}},
		{"sentinel", config.RedisConfig{Topology: "sentinel", Addresses: []string{"sentinel:26379"}, SentinelMaster: "mymaster"}},
		{"cluster", config.RedisConfig{Topology: "cluster", Addresses: []string{"cluster-1:6379", "cluster-2:6379", "cluster-3:6379"}}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.topology, func(t *testing.T) {
			if got := integrationRedisConfig(testCase.topology); !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("integrationRedisConfig(%q) = %#v, want %#v", testCase.topology, got, testCase.want)
			}
		})
	}
}

func TestRedis881TopologiesAcceptReadOperation(t *testing.T) {
	testCases := []struct {
		name string
	}{
		{"standalone"},
		{"sentinel"},
		{"cluster"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			adapter, err := New(integrationRedisConfig(testCase.name))
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			defer func() { _ = adapter.Close() }()

			key := "integration-" + testCase.name
			if _, err := adapter.Call(ctx, redistoolset.Call{
				Tool: "redis_string_write", Operation: "set", Write: true,
				Args: map[string]any{"key": key, "value": map[string]any{"utf8": "value"}},
			}); err != nil {
				t.Fatalf("SET through Adapter.Call: %v", err)
			}
			result, err := adapter.Call(ctx, redistoolset.Call{
				Tool: "redis_string", Operation: "get", Args: map[string]any{"key": key},
			})
			if err != nil {
				t.Fatalf("GET through Adapter.Call: %v", err)
			}
			value, _ := result["value"].(map[string]any)
			if value["base64"] != "dmFsdWU=" {
				t.Fatalf("GET result = %#v, want base64 value", result)
			}
		})
	}
}

func TestAdapterConditionalSetUsesRedisCondition(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	adapter, err := New(integrationRedisConfig("standalone"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = adapter.Close() }()

	_, err = adapter.Call(ctx, redistoolset.Call{
		Tool: "redis_string_write", Operation: "set", Write: true,
		Args: map[string]any{"key": "conditional-set", "value": map[string]any{"utf8": "before"}},
	})
	if err != nil {
		t.Fatalf("initial SET: %v", err)
	}
	result, err := adapter.Call(ctx, redistoolset.Call{
		Tool: "redis_string_write", Operation: "set", Write: true,
		Args: map[string]any{
			"key": "conditional-set", "value": map[string]any{"utf8": "after"}, "condition": "ifeq",
			"comparison": map[string]any{"utf8": "before"}, "return_previous": true,
		},
	})
	if err != nil {
		t.Fatalf("conditional SET: %v", err)
	}
	mutation := result["mutation"].(map[string]any)
	previous := mutation["previous"].(map[string]any)
	if mutation["applied"] != true || previous["utf8"] != "before" {
		t.Fatalf("conditional SET result = %#v", result)
	}
	_, err = adapter.Call(ctx, redistoolset.Call{
		Tool: "redis_string_write", Operation: "set", Write: true,
		Args: map[string]any{
			"key": "conditional-set", "value": map[string]any{"utf8": "ignored"}, "condition": "ifeq",
			"comparison": map[string]any{"utf8": "before"},
		},
	})
	callError, ok := err.(*redistoolset.CallError)
	if !ok || callError.Category != "condition_not_met" {
		t.Fatalf("conditional SET mismatch error = %#v, want condition_not_met", err)
	}
}
