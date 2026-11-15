//go:build integration

package redisadapter

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/futuretea/redis-mcp-server/pkg/core/config"
	"github.com/futuretea/redis-mcp-server/pkg/core/masking"
	"github.com/futuretea/redis-mcp-server/pkg/toolset"
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

func TestStandaloneMaskingHidesStoredBytes(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	adapter, err := New(integrationRedisConfig("standalone"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = adapter.Close() }()

	const secret = "synthetic-token-value"
	policy, err := masking.ParseJSON(`{"enabled":true,"values":["` + secret + `"],"names":["synthetic_field"]}`)
	if err != nil {
		t.Fatal(err)
	}
	active := redistoolset.NewToolset(adapter, true)
	active.SetPolicy(policy)
	disabled := redistoolset.NewToolset(adapter, true)
	key := "mask-{int}"
	set := findIntegrationTool(t, active.GetTools(), "redis_string_write")
	get := findIntegrationTool(t, active.GetTools(), "redis_string")
	if _, err := set.Handler(ctx, map[string]any{
		"operation": "set",
		"args":      map[string]any{"key": key, "value": map[string]any{"utf8": "prefix " + secret + " suffix"}},
	}); err != nil {
		t.Fatal(err)
	}
	masked, err := get.Handler(ctx, map[string]any{"operation": "get", "args": map[string]any{"key": key}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(masked, secret) || !strings.Contains(masked, "[masked]") {
		t.Fatalf("masked GET = %s", masked)
	}
	plain, err := findIntegrationTool(t, disabled.GetTools(), "redis_string").Handler(ctx, map[string]any{
		"operation": "get", "args": map[string]any{"key": key},
	})
	if err != nil || !strings.Contains(plain, secret) {
		t.Fatalf("disabled GET = %s, err %v", plain, err)
	}

	hashKey := "mask-hash"
	if _, err := findIntegrationTool(t, active.GetTools(), "redis_hash_write").Handler(ctx, map[string]any{
		"operation": "set_fields",
		"args": map[string]any{"key": hashKey, "fields": []any{map[string]any{
			"field": map[string]any{"utf8": "synthetic_field"},
			"value": map[string]any{"utf8": secret},
		}}},
	}); err != nil {
		t.Fatal(err)
	}
	hashResult, err := findIntegrationTool(t, active.GetTools(), "redis_hash").Handler(ctx, map[string]any{
		"operation": "get",
		"args":      map[string]any{"key": hashKey, "field": map[string]any{"utf8": "synthetic_field"}},
	})
	if err != nil || strings.Contains(hashResult, secret) || strings.Contains(hashResult, "synthetic_field") {
		t.Fatalf("hash GET = %s, err %v", hashResult, err)
	}

	if _, err := findIntegrationTool(t, active.GetTools(), "redis_sorted_set_write").Handler(ctx, map[string]any{
		"operation": "add",
		"args": map[string]any{"key": "mask-zset", "items": []any{map[string]any{
			"score":  float64(1),
			"member": map[string]any{"utf8": secret},
		}}},
	}); err != nil {
		t.Fatal(err)
	}
	zsetResult, err := findIntegrationTool(t, active.GetTools(), "redis_sorted_set").Handler(ctx, map[string]any{
		"operation": "range",
		"args":      map[string]any{"key": "mask-zset", "by": "rank", "start": float64(0), "count": float64(1), "with_scores": true},
	})
	if err != nil || strings.Contains(zsetResult, secret) || !strings.Contains(zsetResult, `"score":1`) {
		t.Fatalf("zset range = %s, err %v", zsetResult, err)
	}

	many, err := get.Handler(ctx, map[string]any{
		"operation": "get_many",
		"args":      map[string]any{"keys": []any{key, "missing-{int}"}},
	})
	if err != nil || strings.Contains(many, secret) || !strings.Contains(many, "[masked]") {
		t.Fatalf("get_many = %s, err %v", many, err)
	}

	size, err := findIntegrationTool(t, active.GetTools(), "redis_observe").Handler(ctx, map[string]any{
		"operation": "db_size", "args": map[string]any{},
	})
	if err != nil || strings.Contains(size, secret) || strings.Contains(size, resultMaskedFailure) {
		t.Fatalf("db_size = %s, err %v", size, err)
	}
	dumped, err := findIntegrationTool(t, active.GetTools(), "redis_key").Handler(ctx, map[string]any{
		"operation": "dump", "args": map[string]any{"key": key},
	})
	if err != nil || !strings.Contains(dumped, resultMaskedFailure) || strings.Contains(dumped, secret) {
		t.Fatalf("dump = %s, err %v", dumped, err)
	}
}

const resultMaskedFailure = "[redis_masking_failed] result was not returned"

func findIntegrationTool(t *testing.T, tools []toolset.ServerTool, name string) toolset.ServerTool {
	t.Helper()
	for _, tool := range tools {
		if tool.Tool.Name == name {
			return tool
		}
	}
	t.Fatalf("missing %s", name)
	return toolset.ServerTool{}
}
