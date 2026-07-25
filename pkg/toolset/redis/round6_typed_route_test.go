package redis_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/futuretea/redis-mcp-server/internal/redisadapter"
	"github.com/futuretea/redis-mcp-server/pkg/core/config"
	redistoolset "github.com/futuretea/redis-mcp-server/pkg/toolset/redis"
)

func TestTypedAdapterPreservesRoundSixBoundsAndDigest(t *testing.T) {
	testCases := []struct {
		name string
		call redistoolset.Call
		want []string
	}{
		{
			name: "numeric score bounds",
			call: redistoolset.Call{Tool: "redis_sorted_set", Operation: "count_by_score", Args: map[string]any{"key": "set", "min": float64(1.5), "max": float64(2)}},
			want: []string{"zcount", "set", "1.5", "2"},
		},
		{
			name: "lex bounds",
			call: redistoolset.Call{Tool: "redis_sorted_set", Operation: "lex_count", Args: map[string]any{"key": "set", "min": "[a", "max": "[z"}},
			want: []string{"zlexcount", "set", "[a", "[z"},
		},
		{
			name: "maximum digest set comparison",
			call: redistoolset.Call{Tool: "redis_string_write", Operation: "set", Write: true, Args: map[string]any{"key": "key", "value": map[string]any{"utf8": "value"}, "condition": "ifdeq", "comparison": "ffffffffffffffff"}},
			want: []string{"set", "key", "value", "ifdeq", "ffffffffffffffff"},
		},
		{
			name: "maximum digest delete comparison",
			call: redistoolset.Call{Tool: "redis_string_write", Operation: "delete_if", Write: true, Args: map[string]any{"key": "key", "condition": "ifdne", "comparison": "ffffffffffffffff"}},
			want: []string{"delex", "key", "ifdne", "ffffffffffffffff"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := newRedisRESPServer(t)
			adapter, err := redisadapter.New(config.RedisConfig{Topology: "standalone", Addresses: []string{server.listener.Addr().String()}})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			t.Cleanup(func() { _ = adapter.Close() })

			if _, err := adapter.Call(context.Background(), testCase.call); err != nil {
				t.Fatalf("Call: %v", err)
			}
			if got := server.command(t, testCase.want[0]); !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("Redis command = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestTypedAdapterRejectsZeroDigestComparison(t *testing.T) {
	testCases := []struct {
		name string
		call redistoolset.Call
	}{
		{
			name: "set comparison",
			call: redistoolset.Call{Tool: "redis_string_write", Operation: "set", Write: true, Args: map[string]any{"key": "key", "value": map[string]any{"utf8": "value"}, "condition": "ifdeq", "comparison": "0000000000000000"}},
		},
		{
			name: "delete comparison",
			call: redistoolset.Call{Tool: "redis_string_write", Operation: "delete_if", Write: true, Args: map[string]any{"key": "key", "condition": "ifdne", "comparison": "0000000000000000"}},
		},
		{
			name: "ifne previous value",
			call: redistoolset.Call{Tool: "redis_string_write", Operation: "set", Write: true, Args: map[string]any{"key": "key", "value": map[string]any{"utf8": "value"}, "condition": "ifne", "comparison": map[string]any{"utf8": "match"}, "return_previous": true}},
		},
		{
			name: "ifdne previous value",
			call: redistoolset.Call{Tool: "redis_string_write", Operation: "set", Write: true, Args: map[string]any{"key": "key", "value": map[string]any{"utf8": "value"}, "condition": "ifdne", "comparison": "0000000000000001", "return_previous": true}},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := newRedisRESPServer(t)
			adapter, err := redisadapter.New(config.RedisConfig{Topology: "standalone", Addresses: []string{server.listener.Addr().String()}})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			t.Cleanup(func() { _ = adapter.Close() })

			_, err = adapter.Call(context.Background(), testCase.call)
			var callErr *redistoolset.CallError
			if !errors.As(err, &callErr) || callErr.Category != "invalid_argument" {
				t.Fatalf("Call error = %#v, want invalid_argument", err)
			}
			select {
			case command := <-server.commands:
				t.Fatalf("Redis command = %q, want no command", command)
			default:
			}
		})
	}
}

func TestTypedAdapterReturnsLosslessDigestRepresentation(t *testing.T) {
	for _, digest := range []string{"0000000000000000", "ffffffffffffffff"} {
		t.Run(digest, func(t *testing.T) {
			server := newRedisRESPServerWithReply(t, func(command []string) string {
				if len(command) > 0 && command[0] == "digest" {
					return "$16\r\n" + digest + "\r\n"
				}
				return redisRESPReply(command)
			})
			adapter, err := redisadapter.New(config.RedisConfig{Topology: "standalone", Addresses: []string{server.listener.Addr().String()}})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			t.Cleanup(func() { _ = adapter.Close() })

			data, err := adapter.Call(context.Background(), redistoolset.Call{Tool: "redis_string", Operation: "digest", Args: map[string]any{"key": "key"}})
			if err != nil {
				t.Fatalf("Call: %v", err)
			}
			want := map[string]any{"metadata": map[string]any{"xxh3": digest}}
			if !reflect.DeepEqual(data, want) {
				t.Fatalf("Call data = %#v, want %#v", data, want)
			}
			if got := server.command(t, "digest"); !reflect.DeepEqual(got, []string{"digest", "key"}) {
				t.Fatalf("Redis command = %q, want DIGEST key", got)
			}
		})
	}
}

func TestTypedAdapterReportsConditionalSetReturnPreviousOutcomes(t *testing.T) {
	noPrevious := map[string]any{"mutation": map[string]any{"applied": true}}
	previous := map[string]any{"mutation": map[string]any{"applied": true, "previous": map[string]any{"base64": "b2xk", "utf8": "old"}}}
	testCases := []struct {
		name         string
		args         map[string]any
		setReply     string
		wantData     map[string]any
		wantCategory string
		wantCommand  []string
	}{
		{
			name:        "nx applies without previous value",
			args:        map[string]any{"key": "key", "value": map[string]any{"utf8": "value"}, "condition": "nx", "return_previous": true},
			setReply:    "+OK\r\n",
			wantData:    noPrevious,
			wantCommand: []string{"set", "key", "value", "nx"},
		},
		{
			name:         "nx condition not met",
			args:         map[string]any{"key": "key", "value": map[string]any{"utf8": "value"}, "condition": "nx", "return_previous": true},
			setReply:     "$-1\r\n",
			wantCategory: "condition_not_met",
			wantCommand:  []string{"set", "key", "value", "nx"},
		},
		{
			name:        "unconditional get without previous value",
			args:        map[string]any{"key": "key", "value": map[string]any{"utf8": "value"}, "return_previous": true},
			setReply:    "$-1\r\n",
			wantData:    noPrevious,
			wantCommand: []string{"set", "key", "value", "get"},
		},
		{
			name:        "xx get returns previous value",
			args:        map[string]any{"key": "key", "value": map[string]any{"utf8": "value"}, "condition": "xx", "return_previous": true},
			setReply:    "$3\r\nold\r\n",
			wantData:    previous,
			wantCommand: []string{"set", "key", "value", "xx", "get"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server := newRedisRESPServerWithReply(t, func(command []string) string {
				if len(command) > 0 && command[0] == "set" {
					return testCase.setReply
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
