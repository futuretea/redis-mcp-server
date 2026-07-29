package redis_test

import (
	"context"
	"encoding/json"
	"sort"
	"testing"

	"github.com/futuretea/redis-mcp-server/pkg/toolset"
	redistoolset "github.com/futuretea/redis-mcp-server/pkg/toolset/redis"
)

func TestToolset_DefaultCatalogContainsOnlyReadTools(t *testing.T) {
	tools := redistoolset.NewToolset(nil, false).GetTools()

	got := toolNames(tools)
	want := []string{
		"redis_hash",
		"redis_key",
		"redis_list",
		"redis_observe",
		"redis_set",
		"redis_sorted_set",
		"redis_stream",
		"redis_string",
	}
	if !sameStrings(got, want) {
		t.Fatalf("default tool names = %v, want %v", got, want)
	}
}

func TestToolset_WriteCatalogAddsOnlyDeclaredWriteTools(t *testing.T) {
	tools := redistoolset.NewToolset(nil, true).GetTools()

	got := toolNames(tools)
	want := []string{
		"redis_hash",
		"redis_hash_write",
		"redis_key",
		"redis_key_write",
		"redis_list",
		"redis_list_write",
		"redis_observe",
		"redis_set",
		"redis_set_write",
		"redis_sorted_set",
		"redis_sorted_set_write",
		"redis_stream",
		"redis_stream_write",
		"redis_string",
		"redis_string_write",
	}
	if !sameStrings(got, want) {
		t.Fatalf("write tool names = %v, want %v", got, want)
	}
}

func TestToolsetSchemasDeclareObjectRoot(t *testing.T) {
	for _, writeEnabled := range []bool{false, true} {
		for _, tool := range redistoolset.NewToolset(nil, writeEnabled).GetTools() {
			var schema map[string]any
			if err := json.Unmarshal(tool.Tool.RawInputSchema, &schema); err != nil {
				t.Fatalf("unmarshal %s schema: %v", tool.Tool.Name, err)
			}
			if schema["type"] != "object" {
				t.Fatalf("%s input schema type = %#v, want object", tool.Tool.Name, schema["type"])
			}
		}
	}
}

func TestToolsetRejectsUnknownTopLevelAndArguments(t *testing.T) {
	tool := findTool(t, redistoolset.NewToolset(nil, false).GetTools(), "redis_string")
	result, err := tool.Handler(context.Background(), map[string]any{
		"operation": "get",
		"args":      map[string]any{"key": "sample", "unexpected": true},
	})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(result), &envelope); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	errorValue, _ := envelope["error"].(map[string]any)
	if errorValue["category"] != "invalid_argument" {
		t.Fatalf("result = %s, want invalid_argument", result)
	}
}

func TestToolsetRejectsMissingBinaryAndCrossSlotBeforeAdapter(t *testing.T) {
	adapter := &recordingAdapter{}
	read := findTool(t, redistoolset.NewToolset(adapter, false).GetTools(), "redis_string")
	for _, params := range []map[string]any{
		{"operation": "get", "args": map[string]any{}},
		{"operation": "get_many", "args": map[string]any{"keys": []any{"first", "second"}}},
	} {
		result, err := read.Handler(context.Background(), params)
		if err != nil {
			t.Fatalf("Handler: %v", err)
		}
		if !containsText(result, `"category":"invalid_argument"`) {
			t.Fatalf("result = %s, want invalid_argument", result)
		}
	}
	write := findTool(t, redistoolset.NewToolset(adapter, true).GetTools(), "redis_string_write")
	result, err := write.Handler(context.Background(), map[string]any{
		"operation": "append",
		"args":      map[string]any{"key": "sample", "value": map[string]any{"utf8": "a", "base64": "YQ=="}},
	})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if !containsText(result, `"category":"invalid_argument"`) {
		t.Fatalf("result = %s, want invalid_argument", result)
	}
	if adapter.calls != 0 {
		t.Fatalf("adapter calls = %d, want 0", adapter.calls)
	}
}

func TestToolsetUsesStableNonSuccessEnvelopes(t *testing.T) {
	tool := findTool(t, redistoolset.NewToolset(errorAdapter{category: "not_found"}, false).GetTools(), "redis_string")
	result, err := tool.Handler(context.Background(), map[string]any{"operation": "get", "args": map[string]any{"key": "sample"}})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if result != `{"operation":"get","status":"not_found"}` {
		t.Fatalf("result = %s", result)
	}
	tool = findTool(t, redistoolset.NewToolset(errorAdapter{category: "condition_not_met"}, true).GetTools(), "redis_string_write")
	result, err = tool.Handler(context.Background(), map[string]any{"operation": "set", "args": map[string]any{"key": "sample", "value": map[string]any{"utf8": "value"}}})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if result != `{"operation":"set","status":"condition_not_met"}` {
		t.Fatalf("result = %s", result)
	}
}

func TestToolsetAcceptsDigestComparisonAsFixedWidthHex(t *testing.T) {
	tool := findTool(t, redistoolset.NewToolset(nil, true).GetTools(), "redis_string_write")
	result, err := tool.Handler(context.Background(), map[string]any{
		"operation": "delete_if",
		"args": map[string]any{
			"key":        "sample",
			"condition":  "ifdeq",
			"comparison": "000000000000002a",
		},
	})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if !containsText(result, `"category":"unavailable"`) {
		t.Fatalf("result = %s, want unavailable after valid contract", result)
	}
}

func TestToolsetRequiresComparisonForConditionalSet(t *testing.T) {
	tool := findTool(t, redistoolset.NewToolset(nil, true).GetTools(), "redis_string_write")
	result, err := tool.Handler(context.Background(), map[string]any{
		"operation": "set",
		"args": map[string]any{
			"key":       "sample",
			"value":     map[string]any{"utf8": "value"},
			"condition": "ifeq",
		},
	})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if !containsText(result, `"category":"invalid_argument"`) {
		t.Fatalf("result = %s, want invalid_argument", result)
	}
}

func TestToolsetRejectsInvalidStreamTrimThresholdBeforeAdapter(t *testing.T) {
	adapter := &recordingAdapter{}
	tool := findTool(t, redistoolset.NewToolset(adapter, true).GetTools(), "redis_stream_write")
	result, err := tool.Handler(context.Background(), map[string]any{
		"operation": "trim",
		"args": map[string]any{
			"key": "sample", "strategy": "maxlen", "threshold": "not-an-integer",
		},
	})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if !containsText(result, `"category":"invalid_argument"`) {
		t.Fatalf("result = %s, want invalid_argument", result)
	}
	if adapter.calls != 0 {
		t.Fatalf("adapter calls = %d, want 0", adapter.calls)
	}
}

type recordingAdapter struct {
	calls int
	last  redistoolset.Call
}

func (a *recordingAdapter) Call(_ context.Context, call redistoolset.Call) (map[string]any, error) {
	a.calls++
	a.last = call
	return map[string]any{}, nil
}

type errorAdapter struct{ category string }

func (a errorAdapter) Call(context.Context, redistoolset.Call) (map[string]any, error) {
	return nil, &redistoolset.CallError{Category: a.category, Message: "fixture"}
}

func containsText(value, part string) bool {
	return len(value) >= len(part) && (value == part || containsSubstring(value, part))
}

func containsSubstring(value, part string) bool {
	for index := 0; index+len(part) <= len(value); index++ {
		if value[index:index+len(part)] == part {
			return true
		}
	}
	return false
}

func findTool(t *testing.T, tools []toolset.ServerTool, name string) toolset.ServerTool {
	t.Helper()
	for _, tool := range tools {
		if tool.Tool.Name == name {
			return tool
		}
	}
	t.Fatalf("tool %q not found", name)
	return toolset.ServerTool{}
}

func toolNames(tools []toolset.ServerTool) []string {
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Tool.Name)
	}
	return names
}

func sameStrings(got, want []string) bool {
	sort.Strings(got)
	sort.Strings(want)
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
