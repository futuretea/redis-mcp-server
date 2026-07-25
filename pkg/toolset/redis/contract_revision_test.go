package redis_test

import (
	"context"
	"encoding/json"
	"testing"

	redistoolset "github.com/futuretea/redis-mcp-server/pkg/toolset/redis"
)

func TestToolsetRejectsRemovedUnboundedAndStreamEntryOperations(t *testing.T) {
	testCases := []struct {
		tool      string
		operation string
		args      map[string]any
	}{
		{"redis_hash", "get_all", map[string]any{"key": "hash"}},
		{"redis_hash", "fields", map[string]any{"key": "hash"}},
		{"redis_hash", "values", map[string]any{"key": "hash"}},
		{"redis_set", "difference", map[string]any{"keys": []any{"{slot}a"}}},
		{"redis_set", "intersection", map[string]any{"keys": []any{"{slot}a"}}},
		{"redis_set", "members", map[string]any{"key": "set"}},
		{"redis_set", "union", map[string]any{"keys": []any{"{slot}a"}}},
		{"redis_sorted_set", "difference", map[string]any{"keys": []any{"{slot}a"}}},
		{"redis_sorted_set", "intersection", map[string]any{"keys": []any{"{slot}a"}}},
		{"redis_sorted_set", "union", map[string]any{"keys": []any{"{slot}a"}}},
		{"redis_stream", "inspect", map[string]any{"key": "stream", "kind": "stream"}},
		{"redis_stream", "range", map[string]any{"key": "stream", "start": "-", "end": "+"}},
		{"redis_stream", "read", map[string]any{"streams": []any{map[string]any{"key": "stream", "id": "0-0"}}}},
		{"redis_stream_write", "read_group", map[string]any{"group": "group", "consumer": "consumer", "streams": []any{map[string]any{"key": "stream", "id": ">"}}}},
		{"redis_stream_write", "auto_claim", map[string]any{"key": "stream", "group": "group", "consumer": "consumer", "min_idle_ms": float64(1), "start_id": "0-0"}},
		{"redis_stream_write", "claim", map[string]any{"key": "stream", "group": "group", "consumer": "consumer", "min_idle_ms": float64(1), "ids": []any{"0-0"}}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.tool+"/"+testCase.operation, func(t *testing.T) {
			tool := findTool(t, redistoolset.NewToolset(nil, true).GetTools(), testCase.tool)
			result, err := tool.Handler(context.Background(), map[string]any{"operation": testCase.operation, "args": testCase.args})
			if err != nil {
				t.Fatalf("Handler: %v", err)
			}
			if !containsText(result, `"category":"invalid_argument"`) {
				t.Fatalf("result = %s, want invalid_argument before Adapter.Call", result)
			}
		})
	}
}

func TestToolsetRequiresBoundForEveryPublicScan(t *testing.T) {
	testCases := []struct {
		tool string
		args map[string]any
	}{
		{"redis_observe", map[string]any{}},
		{"redis_hash", map[string]any{"key": "hash"}},
		{"redis_set", map[string]any{"key": "set"}},
		{"redis_sorted_set", map[string]any{"key": "sorted"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.tool, func(t *testing.T) {
			adapter := &recordingAdapter{}
			tool := findTool(t, redistoolset.NewToolset(adapter, false).GetTools(), testCase.tool)
			result, err := tool.Handler(context.Background(), map[string]any{"operation": "scan", "args": testCase.args})
			if err != nil {
				t.Fatalf("Handler: %v", err)
			}
			if !containsText(result, `"category":"invalid_argument"`) {
				t.Fatalf("result = %s, want invalid_argument", result)
			}
			if adapter.calls != 0 {
				t.Fatalf("adapter calls = %d, want 0", adapter.calls)
			}
		})
	}
}

func TestToolsetRejectsOpaqueCursorAndTypedOptionViolationsBeforeAdapter(t *testing.T) {
	testCases := []struct {
		tool      string
		operation string
		args      map[string]any
	}{
		{"redis_set", "scan", map[string]any{"key": "set", "count": float64(1), "cursor": "0"}},
		{"redis_key_write", "set_expiry", map[string]any{"key": "key", "condition": "nx", "expiry": map[string]any{"kind": "px", "value": float64(1)}}},
		{"redis_string_write", "set", map[string]any{"key": "key", "value": map[string]any{"utf8": "value"}, "expiry": map[string]any{"kind": "pxat", "value": float64(1)}}},
		{"redis_stream_write", "add", map[string]any{"key": "stream", "fields": []any{map[string]any{"field": map[string]any{"utf8": "field"}, "value": map[string]any{"utf8": "value"}}}}},
		{"redis_stream_write", "trim", map[string]any{"key": "stream", "strategy": "maxlen", "threshold": float64(1), "limit": float64(1)}},
		{"redis_sorted_set", "range", map[string]any{"key": "set", "by": "rank", "start": float64(-1), "count": float64(1)}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.tool+"/"+testCase.operation, func(t *testing.T) {
			adapter := &recordingAdapter{}
			tool := findTool(t, redistoolset.NewToolset(adapter, true).GetTools(), testCase.tool)
			result, err := tool.Handler(context.Background(), map[string]any{"operation": testCase.operation, "args": testCase.args})
			if err != nil {
				t.Fatalf("Handler: %v", err)
			}
			if !containsText(result, `"category":"invalid_argument"`) || adapter.calls != 0 {
				t.Fatalf("result=%s adapter calls=%d, want invalid_argument before Adapter.Call", result, adapter.calls)
			}
		})
	}
}

func TestToolsetRejectsEmptyPublicKeyShapesBeforeAdapter(t *testing.T) {
	validBinary := map[string]any{"utf8": "value"}
	testCases := []struct {
		name      string
		tool      string
		operation string
		args      map[string]any
	}{
		{"key", "redis_string", "get", map[string]any{"key": ""}},
		{"source_key", "redis_key_write", "copy", map[string]any{"source_key": "", "destination_key": ""}},
		{"destination_key", "redis_key_write", "copy", map[string]any{"source_key": "", "destination_key": ""}},
		{"first_key", "redis_string", "longest_common_substring", map[string]any{"first_key": "", "second_key": ""}},
		{"second_key", "redis_string", "longest_common_substring", map[string]any{"first_key": "", "second_key": ""}},
		{"keys", "redis_key", "exists", map[string]any{"keys": []any{""}}},
		{"items.key", "redis_string_write", "set_many", map[string]any{"items": []any{map[string]any{"key": "", "value": validBinary}}}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			adapter := &recordingAdapter{}
			tool := findTool(t, redistoolset.NewToolset(adapter, true).GetTools(), testCase.tool)
			result, err := tool.Handler(context.Background(), map[string]any{"operation": testCase.operation, "args": testCase.args})
			if err != nil {
				t.Fatalf("Handler: %v", err)
			}
			if !containsText(result, `"category":"invalid_argument"`) || adapter.calls != 0 {
				t.Fatalf("result=%s adapter calls=%d, want invalid_argument before Adapter.Call", result, adapter.calls)
			}
		})
	}
}

func TestGeneratedSchemasRequireNonEmptyPublicKeyShapes(t *testing.T) {
	testCases := []struct {
		tool      string
		operation string
		path      []string
	}{
		{"redis_string", "get", []string{"key"}},
		{"redis_key_write", "copy", []string{"source_key"}},
		{"redis_key_write", "copy", []string{"destination_key"}},
		{"redis_string", "longest_common_substring", []string{"first_key"}},
		{"redis_string", "longest_common_substring", []string{"second_key"}},
		{"redis_key", "exists", []string{"keys", "items"}},
		{"redis_string_write", "set_many", []string{"items", "items", "properties", "key"}},
	}

	tools := redistoolset.NewToolset(nil, true).GetTools()
	for _, testCase := range testCases {
		t.Run(testCase.tool+"/"+testCase.operation, func(t *testing.T) {
			tool := findTool(t, tools, testCase.tool)
			schema := operationArgumentSchema(t, tool.Tool.RawInputSchema, testCase.operation, testCase.path...)
			if got, ok := schema["minLength"].(float64); !ok || got != 1 {
				t.Fatalf("schema %v minLength = %#v, want 1", testCase.path, schema["minLength"])
			}
		})
	}
}

func operationArgumentSchema(t *testing.T, raw json.RawMessage, operation string, path ...string) map[string]any {
	t.Helper()
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	variants, ok := schema["oneOf"].([]any)
	if !ok {
		t.Fatal("tool schema has no operation variants")
	}
	for _, value := range variants {
		variant, _ := value.(map[string]any)
		properties, _ := variant["properties"].(map[string]any)
		operationSchema, _ := properties["operation"].(map[string]any)
		if operationSchema["const"] != operation {
			continue
		}
		args, _ := properties["args"].(map[string]any)
		properties, _ = args["properties"].(map[string]any)
		var current any = properties
		for _, segment := range path {
			object, ok := current.(map[string]any)
			if !ok {
				t.Fatalf("schema path %v is not an object at %q", path, segment)
			}
			current = object[segment]
		}
		result, ok := current.(map[string]any)
		if !ok {
			t.Fatalf("schema path %v is not a schema object", path)
		}
		return result
	}
	t.Fatalf("operation %q not found", operation)
	return nil
}
