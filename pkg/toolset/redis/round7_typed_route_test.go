package redis_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/futuretea/redis-mcp-server/internal/redisadapter"
	"github.com/futuretea/redis-mcp-server/pkg/core/config"
	redistoolset "github.com/futuretea/redis-mcp-server/pkg/toolset/redis"
)

func TestTypedAdapterPreservesHashSetEXConditionWithoutExpiry(t *testing.T) {
	field := map[string]any{"field": map[string]any{"utf8": "field"}, "value": map[string]any{"utf8": "value"}}
	testCases := []struct {
		name    string
		args    map[string]any
		reply   string
		want    map[string]any
		command []string
	}{
		{
			name:    "fnx applies",
			args:    map[string]any{"key": "hash", "fields": []any{field}, "condition": "fnx"},
			reply:   ":1\r\n",
			want:    map[string]any{"mutation": map[string]any{"applied": true}},
			command: []string{"HSETEX", "hash", "FNX", "FIELDS", "1", "field", "value"},
		},
		{
			name:    "fxx condition not met",
			args:    map[string]any{"key": "hash", "fields": []any{field}, "condition": "fxx"},
			reply:   ":0\r\n",
			want:    map[string]any{"mutation": map[string]any{"applied": false}},
			command: []string{"HSETEX", "hash", "FXX", "FIELDS", "1", "field", "value"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := newRedisRESPServerWithReply(t, func(command []string) string {
				if len(command) > 0 && strings.EqualFold(command[0], "hsetex") {
					return testCase.reply
				}
				return redisRESPReply(command)
			})
			adapter, err := redisadapter.New(config.RedisConfig{Topology: "standalone", Addresses: []string{server.listener.Addr().String()}})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			t.Cleanup(func() { _ = adapter.Close() })

			data, err := adapter.Call(context.Background(), redistoolset.Call{Tool: "redis_hash_write", Operation: "set_fields_with_expiry", Write: true, Args: testCase.args})
			if err != nil {
				t.Fatalf("Call: %v", err)
			}
			if !reflect.DeepEqual(data, testCase.want) {
				t.Fatalf("Call data = %#v, want %#v", data, testCase.want)
			}
			if got := server.command(t, "hsetex"); !reflect.DeepEqual(got, testCase.command) {
				t.Fatalf("Redis command = %q, want %q", got, testCase.command)
			}
		})
	}
}

func TestTypedAdapterReportsNonEqualConditionalSetOutcomes(t *testing.T) {
	noPrevious := map[string]any{"mutation": map[string]any{"applied": true}}
	testCases := []struct {
		name         string
		args         map[string]any
		reply        string
		wantData     map[string]any
		wantCategory string
		wantCommand  []string
	}{
		{
			name:        "ifne applies",
			args:        map[string]any{"key": "key", "value": map[string]any{"utf8": "value"}, "condition": "ifne", "comparison": map[string]any{"utf8": "match"}},
			reply:       "+OK\r\n",
			wantData:    noPrevious,
			wantCommand: []string{"set", "key", "value", "ifne", "match"},
		},
		{
			name:         "ifne condition not met",
			args:         map[string]any{"key": "key", "value": map[string]any{"utf8": "value"}, "condition": "ifne", "comparison": map[string]any{"utf8": "match"}},
			reply:        "$-1\r\n",
			wantCategory: "condition_not_met",
			wantCommand:  []string{"set", "key", "value", "ifne", "match"},
		},
		{
			name:        "ifdne applies",
			args:        map[string]any{"key": "key", "value": map[string]any{"utf8": "value"}, "condition": "ifdne", "comparison": "0000000000000001"},
			reply:       "+OK\r\n",
			wantData:    noPrevious,
			wantCommand: []string{"set", "key", "value", "ifdne", "0000000000000001"},
		},
		{
			name:         "ifdne condition not met",
			args:         map[string]any{"key": "key", "value": map[string]any{"utf8": "value"}, "condition": "ifdne", "comparison": "0000000000000001"},
			reply:        "$-1\r\n",
			wantCategory: "condition_not_met",
			wantCommand:  []string{"set", "key", "value", "ifdne", "0000000000000001"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := newRedisRESPServerWithReply(t, func(command []string) string {
				if len(command) > 0 && command[0] == "set" {
					return testCase.reply
				}
				return redisRESPReply(command)
			})
			adapter, err := redisadapter.New(config.RedisConfig{Topology: "standalone", Addresses: []string{server.listener.Addr().String()}})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			t.Cleanup(func() { _ = adapter.Close() })

			data, err := adapter.Call(context.Background(), redistoolset.Call{Tool: "redis_string_write", Operation: "set", Write: true, Args: testCase.args})
			if testCase.wantCategory != "" {
				var callErr *redistoolset.CallError
				if !errors.As(err, &callErr) || callErr.Category != testCase.wantCategory {
					t.Fatalf("Call error = %#v, want category %q", err, testCase.wantCategory)
				}
			} else if err != nil {
				t.Fatalf("Call: %v", err)
			} else if !reflect.DeepEqual(data, testCase.wantData) {
				t.Fatalf("Call data = %#v, want %#v", data, testCase.wantData)
			}
			if got := server.command(t, "set"); !reflect.DeepEqual(got, testCase.wantCommand) {
				t.Fatalf("Redis command = %q, want %q", got, testCase.wantCommand)
			}
		})
	}
}

func TestTypedAdapterClassifiesRedisArgumentErrors(t *testing.T) {
	testCases := []struct {
		name     string
		reply    string
		category string
	}{
		{name: "setrange offset is out of range", reply: "-ERR offset is out of range\r\n", category: "invalid_argument"},
		{name: "generic redis argument error", reply: "-ERR syntax error\r\n", category: "invalid_argument"},
		{name: "redis loading remains unavailable", reply: "-LOADING Redis is loading the dataset in memory\r\n", category: "unavailable"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := newRedisRESPServerWithReply(t, func(command []string) string {
				if len(command) > 0 && command[0] == "setrange" {
					return testCase.reply
				}
				return redisRESPReply(command)
			})
			adapter, err := redisadapter.New(config.RedisConfig{Topology: "standalone", Addresses: []string{server.listener.Addr().String()}})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			t.Cleanup(func() { _ = adapter.Close() })

			_, err = adapter.Call(context.Background(), redistoolset.Call{Tool: "redis_string_write", Operation: "set_range", Write: true, Args: map[string]any{"key": "key", "offset": float64(-1), "value": map[string]any{"utf8": "value"}}})
			var callErr *redistoolset.CallError
			if !errors.As(err, &callErr) || callErr.Category != testCase.category {
				t.Fatalf("Call error = %#v, want category %q", err, testCase.category)
			}
			if got := server.command(t, "setrange"); !reflect.DeepEqual(got, []string{"setrange", "key", "-1", "value"}) {
				t.Fatalf("Redis command = %q, want SETRANGE key -1 value", got)
			}
		})
	}
}
