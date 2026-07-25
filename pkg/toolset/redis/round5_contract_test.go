package redis_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	redistoolset "github.com/futuretea/redis-mcp-server/pkg/toolset/redis"
)

func TestToolsetRejectsRoundFiveInvalidInputsBeforeAdapter(t *testing.T) {
	binary := map[string]any{"utf8": "value"}
	items := []any{
		map[string]any{"score": float64(1), "member": binary},
		map[string]any{"score": float64(2), "member": binary},
	}
	testCases := []struct {
		name      string
		tool      string
		operation string
		args      map[string]any
	}{
		{"set nx comparison", "redis_string_write", "set", map[string]any{"key": "key", "value": binary, "condition": "nx", "comparison": binary}},
		{"set xx comparison", "redis_string_write", "set", map[string]any{"key": "key", "value": binary, "condition": "xx", "comparison": binary}},
		{"decrement wrong amount type", "redis_string_write", "decrement", map[string]any{"key": "key", "amount": "0"}},
		{"list pop many zero count", "redis_list_write", "pop_many", map[string]any{"keys": []any{"{slot}list"}, "side": "left", "count": float64(0)}},
		{"list pop many wrong count type", "redis_list_write", "pop_many", map[string]any{"keys": []any{"{slot}list"}, "side": "left", "count": "0"}},
		{"sorted set pop zero count", "redis_sorted_set_write", "pop", map[string]any{"keys": []any{"{slot}set"}, "side": "min", "count": float64(0)}},
		{"sorted set pop wrong count type", "redis_sorted_set_write", "pop", map[string]any{"keys": []any{"{slot}set"}, "side": "min", "count": "0"}},
		{"sorted set add increment multiple items", "redis_sorted_set_write", "add", map[string]any{"key": "set", "items": items, "increment": true}},
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

func TestToolsetPreservesRoundFiveSupportedInputs(t *testing.T) {
	binary := map[string]any{"utf8": "value"}
	testCases := []struct {
		name      string
		tool      string
		operation string
		args      map[string]any
	}{
		{"set byte comparison", "redis_string_write", "set", map[string]any{"key": "key", "value": binary, "condition": "ifeq", "comparison": binary}},
		{"set digest comparison", "redis_string_write", "set", map[string]any{"key": "key", "value": binary, "condition": "ifdeq", "comparison": "0000000000000001"}},
		{"decrement explicit zero", "redis_string_write", "decrement", map[string]any{"key": "key", "amount": float64(0)}},
		{"list pop many omitted count", "redis_list_write", "pop_many", map[string]any{"keys": []any{"{slot}list"}, "side": "left"}},
		{"sorted set pop omitted count", "redis_sorted_set_write", "pop", map[string]any{"keys": []any{"{slot}set"}, "side": "min"}},
		{"sorted set add increment single item", "redis_sorted_set_write", "add", map[string]any{"key": "set", "items": []any{map[string]any{"score": float64(1), "member": binary}}, "increment": true}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			adapter := &recordingAdapter{}
			tool := findTool(t, redistoolset.NewToolset(adapter, true).GetTools(), testCase.tool)
			result, err := tool.Handler(context.Background(), map[string]any{"operation": testCase.operation, "args": testCase.args})
			if err != nil {
				t.Fatalf("Handler: %v", err)
			}
			if containsText(result, `"category":"invalid_argument"`) || adapter.calls != 1 {
				t.Fatalf("result=%s adapter calls=%d, want one Adapter.Call", result, adapter.calls)
			}
			if comparison, present := testCase.args["comparison"]; present && !reflect.DeepEqual(adapter.last.Args["comparison"], comparison) {
				t.Fatalf("comparison = %#v, want %#v", adapter.last.Args["comparison"], comparison)
			}
		})
	}
}

func TestGeneratedSchemaLimitsIncrementingSortedAddToOneItem(t *testing.T) {
	tool := findTool(t, redistoolset.NewToolset(nil, true).GetTools(), "redis_sorted_set_write")
	var schema map[string]any
	if err := json.Unmarshal(tool.Tool.RawInputSchema, &schema); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	allOf := operationAllOf(t, schema, "add")
	for _, value := range allOf {
		constraint, _ := value.(map[string]any)
		condition, _ := constraint["if"].(map[string]any)
		properties, _ := condition["properties"].(map[string]any)
		increment, _ := properties["increment"].(map[string]any)
		if increment["const"] != true {
			continue
		}
		then, _ := constraint["then"].(map[string]any)
		thenProperties, _ := then["properties"].(map[string]any)
		items, _ := thenProperties["items"].(map[string]any)
		if maxItems, ok := items["maxItems"].(float64); !ok || maxItems != 1 {
			t.Fatalf("incrementing sorted-set add maxItems = %#v, want 1", items["maxItems"])
		}
		return
	}
	t.Fatal("sorted-set add schema has no increment cardinality constraint")
}

func operationAllOf(t *testing.T, schema map[string]any, operation string) []any {
	t.Helper()
	variants, _ := schema["oneOf"].([]any)
	for _, value := range variants {
		variant, _ := value.(map[string]any)
		properties, _ := variant["properties"].(map[string]any)
		operationSchema, _ := properties["operation"].(map[string]any)
		if operationSchema["const"] != operation {
			continue
		}
		args, _ := properties["args"].(map[string]any)
		allOf, _ := args["allOf"].([]any)
		return allOf
	}
	t.Fatalf("operation %q not found", operation)
	return nil
}
