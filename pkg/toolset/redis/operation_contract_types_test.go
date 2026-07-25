package redis_test

import (
	"context"
	"encoding/json"
	"testing"

	redistoolset "github.com/futuretea/redis-mcp-server/pkg/toolset/redis"
)

func TestToolsetRejectsOperationSpecificWrongScalarTypesBeforeAdapter(t *testing.T) {
	binary := map[string]any{"utf8": "member"}
	testCases := []struct {
		name      string
		tool      string
		operation string
		args      map[string]any
	}{
		{"rank_with_score", "redis_sorted_set", "rank", map[string]any{"key": "set", "member": binary, "with_score": "true"}},
		{"zadd_increment", "redis_sorted_set_write", "add", map[string]any{"key": "set", "items": []any{map[string]any{"score": float64(1), "member": binary}}, "increment": "true"}},
		{"zincrby_increment", "redis_sorted_set_write", "increment", map[string]any{"key": "set", "increment": true, "member": binary}},
		{"get_range_start", "redis_string", "get_range", map[string]any{"key": "string", "start": "0", "stop": float64(1)}},
		{"get_range_stop", "redis_string", "get_range", map[string]any{"key": "string", "start": float64(0), "stop": "1"}},
		{"string_integer_amount", "redis_string_write", "increment", map[string]any{"key": "string", "number_kind": "integer", "amount": float64(0.5)}},
		{"hash_integer_amount", "redis_hash_write", "increment", map[string]any{"key": "hash", "field": binary, "number_kind": "integer", "amount": float64(0.5)}},
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

func TestToolsetPreservesOperationSpecificScalarValues(t *testing.T) {
	binary := map[string]any{"utf8": "member"}
	testCases := []struct {
		name      string
		tool      string
		operation string
		args      map[string]any
		argument  string
		want      any
	}{
		{"rank_with_score", "redis_sorted_set", "rank", map[string]any{"key": "set", "member": binary, "with_score": true}, "with_score", true},
		{"zadd_increment", "redis_sorted_set_write", "add", map[string]any{"key": "set", "items": []any{map[string]any{"score": float64(1), "member": binary}}, "increment": true}, "increment", true},
		{"zincrby_increment", "redis_sorted_set_write", "increment", map[string]any{"key": "set", "increment": float64(0.5), "member": binary}, "increment", float64(0.5)},
		{"string_float_amount", "redis_string_write", "increment", map[string]any{"key": "string", "number_kind": "float", "amount": float64(0.5)}, "amount", float64(0.5)},
		{"hash_float_amount", "redis_hash_write", "increment", map[string]any{"key": "hash", "field": binary, "number_kind": "float", "amount": float64(0.5)}, "amount", float64(0.5)},
		{"get_range_indexes", "redis_string", "get_range", map[string]any{"key": "string", "start": float64(0), "stop": float64(1)}, "stop", float64(1)},
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
			if got := adapter.last.Args[testCase.argument]; got != testCase.want {
				t.Fatalf("Adapter.Call argument %q = %#v, want %#v", testCase.argument, got, testCase.want)
			}
		})
	}
}

func TestGeneratedSchemasUseOperationSpecificScalarTypes(t *testing.T) {
	testCases := []struct {
		tool      string
		operation string
		argument  string
		wantType  string
	}{
		{"redis_sorted_set", "rank", "with_score", "boolean"},
		{"redis_sorted_set_write", "add", "increment", "boolean"},
		{"redis_sorted_set_write", "increment", "increment", "number"},
		{"redis_string_write", "increment", "amount", "number"},
		{"redis_hash_write", "increment", "amount", "number"},
		{"redis_string", "get_range", "start", "integer"},
		{"redis_string", "get_range", "stop", "integer"},
	}

	tools := redistoolset.NewToolset(nil, true).GetTools()
	for _, testCase := range testCases {
		t.Run(testCase.tool+"/"+testCase.operation+"/"+testCase.argument, func(t *testing.T) {
			tool := findTool(t, tools, testCase.tool)
			schema := operationArgumentSchema(t, tool.Tool.RawInputSchema, testCase.operation, testCase.argument)
			if got := schema["type"]; got != testCase.wantType {
				t.Fatalf("schema type = %#v, want %q", got, testCase.wantType)
			}
		})
	}
}

func TestToolsetRejectsRemoveRangeVariantWrongBoundsBeforeAdapter(t *testing.T) {
	testCases := []struct {
		name string
		args map[string]any
	}{
		{"rank_min", map[string]any{"key": "set", "by": "rank", "min": "zero", "max": float64(1)}},
		{"rank_max", map[string]any{"key": "set", "by": "rank", "min": float64(0), "max": "one"}},
		{"lex_min", map[string]any{"key": "set", "by": "lex", "min": float64(0), "max": "[z"}},
		{"lex_max", map[string]any{"key": "set", "by": "lex", "min": "[a", "max": float64(1)}},
		{"score_min", map[string]any{"key": "set", "by": "score", "min": true, "max": "+inf"}},
		{"score_max", map[string]any{"key": "set", "by": "score", "min": "-inf", "max": true}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			adapter := &recordingAdapter{}
			tool := findTool(t, redistoolset.NewToolset(adapter, true).GetTools(), "redis_sorted_set_write")
			result, err := tool.Handler(context.Background(), map[string]any{"operation": "remove_range", "args": testCase.args})
			if err != nil {
				t.Fatalf("Handler: %v", err)
			}
			if !containsText(result, `"category":"invalid_argument"`) || adapter.calls != 0 {
				t.Fatalf("result=%s adapter calls=%d, want invalid_argument before Adapter.Call", result, adapter.calls)
			}
		})
	}
}

func TestToolsetPreservesRemoveRangeVariantBounds(t *testing.T) {
	testCases := []struct {
		name string
		args map[string]any
	}{
		{"rank", map[string]any{"key": "set", "by": "rank", "min": float64(0), "max": float64(10)}},
		{"lex", map[string]any{"key": "set", "by": "lex", "min": "[alpha", "max": "[omega"}},
		{"score", map[string]any{"key": "set", "by": "score", "min": "-inf", "max": "+inf"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			adapter := &recordingAdapter{}
			tool := findTool(t, redistoolset.NewToolset(adapter, true).GetTools(), "redis_sorted_set_write")
			result, err := tool.Handler(context.Background(), map[string]any{"operation": "remove_range", "args": testCase.args})
			if err != nil {
				t.Fatalf("Handler: %v", err)
			}
			if containsText(result, `"category":"invalid_argument"`) || adapter.calls != 1 {
				t.Fatalf("result=%s adapter calls=%d, want one Adapter.Call", result, adapter.calls)
			}
			if adapter.last.Operation != "remove_range" || adapter.last.Args["by"] != testCase.args["by"] || adapter.last.Args["min"] != testCase.args["min"] || adapter.last.Args["max"] != testCase.args["max"] {
				t.Fatalf("Adapter.Call = %#v, want remove_range %s bounds unchanged", adapter.last, testCase.name)
			}
		})
	}
}

func TestGeneratedSchemasUseRemoveRangeVariantBoundTypes(t *testing.T) {
	testCases := []struct {
		by       string
		wantType string
	}{
		{"rank", "integer"},
		{"lex", "string"},
		{"score", "redis_bound"},
	}

	tool := findTool(t, redistoolset.NewToolset(nil, true).GetTools(), "redis_sorted_set_write")
	for _, testCase := range testCases {
		t.Run(testCase.by, func(t *testing.T) {
			for name, schema := range variantBoundSchemas(t, tool.Tool.RawInputSchema, "remove_range", testCase.by) {
				if testCase.wantType == "redis_bound" {
					if !hasJSONSchemaTypes(schema, "number", "string") {
						t.Fatalf("%s schema = %#v, want Redis bound number|string", name, schema)
					}
					continue
				}
				if schema["type"] != testCase.wantType {
					t.Fatalf("%s schema type = %#v, want %q", name, schema["type"], testCase.wantType)
				}
			}
		})
	}
}

func TestToolsetRejectsRangeAndStoreRangeVariantWrongBoundsBeforeAdapter(t *testing.T) {
	testCases := []struct {
		name      string
		tool      string
		operation string
		args      map[string]any
	}{
		{"range_rank_start", "redis_sorted_set", "range", map[string]any{"key": "set", "by": "rank", "start": "zero", "count": float64(1)}},
		{"range_lex_min", "redis_sorted_set", "range", map[string]any{"key": "set", "by": "lex", "min": float64(0), "max": "[z", "count": float64(1)}},
		{"range_lex_max", "redis_sorted_set", "range", map[string]any{"key": "set", "by": "lex", "min": "[a", "max": float64(1), "count": float64(1)}},
		{"range_score_min", "redis_sorted_set", "range", map[string]any{"key": "set", "by": "score", "min": true, "max": "+inf", "count": float64(1)}},
		{"range_score_max", "redis_sorted_set", "range", map[string]any{"key": "set", "by": "score", "min": "-inf", "max": true, "count": float64(1)}},
		{"store_rank_min", "redis_sorted_set_write", "store_range", map[string]any{"destination_key": "destination{tag}", "source_key": "source{tag}", "by": "rank", "min": "zero", "max": float64(1)}},
		{"store_rank_max", "redis_sorted_set_write", "store_range", map[string]any{"destination_key": "destination{tag}", "source_key": "source{tag}", "by": "rank", "min": float64(0), "max": "one"}},
		{"store_lex_min", "redis_sorted_set_write", "store_range", map[string]any{"destination_key": "destination{tag}", "source_key": "source{tag}", "by": "lex", "min": float64(0), "max": "[z"}},
		{"store_lex_max", "redis_sorted_set_write", "store_range", map[string]any{"destination_key": "destination{tag}", "source_key": "source{tag}", "by": "lex", "min": "[a", "max": float64(1)}},
		{"store_score_min", "redis_sorted_set_write", "store_range", map[string]any{"destination_key": "destination{tag}", "source_key": "source{tag}", "by": "score", "min": true, "max": "+inf"}},
		{"store_score_max", "redis_sorted_set_write", "store_range", map[string]any{"destination_key": "destination{tag}", "source_key": "source{tag}", "by": "score", "min": "-inf", "max": true}},
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

func TestToolsetPreservesRangeAndStoreRangeVariantBounds(t *testing.T) {
	testCases := []struct {
		name      string
		tool      string
		operation string
		args      map[string]any
	}{
		{"range_rank", "redis_sorted_set", "range", map[string]any{"key": "set", "by": "rank", "start": float64(0), "count": float64(1)}},
		{"range_lex", "redis_sorted_set", "range", map[string]any{"key": "set", "by": "lex", "min": "[alpha", "max": "[omega", "count": float64(1)}},
		{"range_score", "redis_sorted_set", "range", map[string]any{"key": "set", "by": "score", "min": "-inf", "max": "+inf", "count": float64(1)}},
		{"store_rank", "redis_sorted_set_write", "store_range", map[string]any{"destination_key": "destination{tag}", "source_key": "source{tag}", "by": "rank", "min": float64(0), "max": float64(10)}},
		{"store_lex", "redis_sorted_set_write", "store_range", map[string]any{"destination_key": "destination{tag}", "source_key": "source{tag}", "by": "lex", "min": "[alpha", "max": "[omega"}},
		{"store_score", "redis_sorted_set_write", "store_range", map[string]any{"destination_key": "destination{tag}", "source_key": "source{tag}", "by": "score", "min": "-inf", "max": "+inf"}},
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
			if adapter.last.Tool != testCase.tool || adapter.last.Operation != testCase.operation {
				t.Fatalf("Adapter.Call = %#v, want %s/%s", adapter.last, testCase.tool, testCase.operation)
			}
			for _, name := range []string{"by", "start", "min", "max"} {
				if want, present := testCase.args[name]; present && adapter.last.Args[name] != want {
					t.Fatalf("Adapter.Call argument %q = %#v, want %#v", name, adapter.last.Args[name], want)
				}
			}
		})
	}
}

func TestGeneratedSchemasUseRangeAndStoreRangeVariantBoundTypes(t *testing.T) {
	testCases := []struct {
		tool      string
		operation string
		by        string
		arguments []string
		wantType  string
	}{
		{"redis_sorted_set", "range", "rank", []string{"start"}, "integer"},
		{"redis_sorted_set", "range", "lex", []string{"min", "max"}, "string"},
		{"redis_sorted_set", "range", "score", []string{"min", "max"}, "redis_bound"},
		{"redis_sorted_set_write", "store_range", "rank", []string{"min", "max"}, "integer"},
		{"redis_sorted_set_write", "store_range", "lex", []string{"min", "max"}, "string"},
		{"redis_sorted_set_write", "store_range", "score", []string{"min", "max"}, "redis_bound"},
	}

	tools := redistoolset.NewToolset(nil, true).GetTools()
	for _, testCase := range testCases {
		t.Run(testCase.tool+"/"+testCase.operation+"/"+testCase.by, func(t *testing.T) {
			tool := findTool(t, tools, testCase.tool)
			for name, schema := range variantBoundSchemas(t, tool.Tool.RawInputSchema, testCase.operation, testCase.by, testCase.arguments...) {
				if testCase.wantType == "redis_bound" {
					if !hasJSONSchemaTypes(schema, "number", "string") {
						t.Fatalf("%s schema = %#v, want Redis bound number|string", name, schema)
					}
					continue
				}
				if schema["type"] != testCase.wantType {
					t.Fatalf("%s schema type = %#v, want %q", name, schema["type"], testCase.wantType)
				}
			}
		})
	}
}

func variantBoundSchemas(t *testing.T, raw json.RawMessage, operation, by string, names ...string) map[string]map[string]any {
	t.Helper()
	if len(names) == 0 {
		names = []string{"min", "max"}
	}
	var toolSchema map[string]any
	if err := json.Unmarshal(raw, &toolSchema); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	for _, variantValue := range toolSchema["oneOf"].([]any) {
		variant := variantValue.(map[string]any)
		properties := variant["properties"].(map[string]any)
		if properties["operation"].(map[string]any)["const"] != operation {
			continue
		}
		args := properties["args"].(map[string]any)
		for _, conditionValue := range args["allOf"].([]any) {
			condition := conditionValue.(map[string]any)
			variants, ok := condition["oneOf"].([]any)
			if !ok {
				continue
			}
			for _, boundValue := range variants {
				bound := boundValue.(map[string]any)
				boundProperties := bound["properties"].(map[string]any)
				if boundProperties["by"].(map[string]any)["const"] != by {
					continue
				}
				result := make(map[string]map[string]any, len(names))
				for _, name := range names {
					schema, ok := boundProperties[name].(map[string]any)
					if !ok {
						break
					}
					result[name] = schema
				}
				if len(result) == len(names) {
					return result
				}
			}
		}
	}
	t.Fatalf("%s schema has no %q variant types", operation, by)
	return nil
}

func hasJSONSchemaTypes(schema map[string]any, types ...string) bool {
	variants, ok := schema["oneOf"].([]any)
	if !ok || len(variants) != len(types) {
		return false
	}
	for _, wantType := range types {
		found := false
		for _, variant := range variants {
			if variant.(map[string]any)["type"] == wantType {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
