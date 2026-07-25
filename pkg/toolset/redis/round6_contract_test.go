package redis_test

import (
	"context"
	"encoding/json"
	"testing"

	redistoolset "github.com/futuretea/redis-mcp-server/pkg/toolset/redis"
)

func TestToolsetRejectsRoundSixInvalidInputsBeforeAdapter(t *testing.T) {
	binary := map[string]any{"utf8": "value"}
	testCases := []struct {
		name      string
		tool      string
		operation string
		args      map[string]any
	}{
		{"numeric lex bound", "redis_sorted_set", "lex_count", map[string]any{"key": "set", "min": float64(1), "max": "[z"}},
		{"numeric digest comparison", "redis_string_write", "set", map[string]any{"key": "key", "value": binary, "condition": "ifdeq", "comparison": float64(1)}},
		{"short digest comparison", "redis_string_write", "set", map[string]any{"key": "key", "value": binary, "condition": "ifdeq", "comparison": "000000000000000"}},
		{"noncanonical digest comparison", "redis_string_write", "delete_if", map[string]any{"key": "key", "condition": "ifdeq", "comparison": "FFFFFFFFFFFFFFFF"}},
		{"zero digest set comparison", "redis_string_write", "set", map[string]any{"key": "key", "value": binary, "condition": "ifdeq", "comparison": "0000000000000000"}},
		{"zero digest delete comparison", "redis_string_write", "delete_if", map[string]any{"key": "key", "condition": "ifdne", "comparison": "0000000000000000"}},
		{"ifne previous value", "redis_string_write", "set", map[string]any{"key": "key", "value": binary, "condition": "ifne", "comparison": binary, "return_previous": true}},
		{"ifdne previous value", "redis_string_write", "set", map[string]any{"key": "key", "value": binary, "condition": "ifdne", "comparison": "0000000000000001", "return_previous": true}},
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

func TestToolsetPreservesRoundSixTypedInputs(t *testing.T) {
	binary := map[string]any{"utf8": "value"}
	digest := "ffffffffffffffff"
	testCases := []struct {
		name      string
		tool      string
		operation string
		args      map[string]any
		argument  string
		want      any
	}{
		{"numeric score bounds", "redis_sorted_set", "count_by_score", map[string]any{"key": "set", "min": float64(1.5), "max": float64(2)}, "min", float64(1.5)},
		{"maximum digest set comparison", "redis_string_write", "set", map[string]any{"key": "key", "value": binary, "condition": "ifdeq", "comparison": digest}, "comparison", digest},
		{"maximum digest delete comparison", "redis_string_write", "delete_if", map[string]any{"key": "key", "condition": "ifdne", "comparison": digest}, "comparison", digest},
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

func TestGeneratedSchemasUseRoundSixScalarContracts(t *testing.T) {
	tools := redistoolset.NewToolset(nil, true).GetTools()
	sortedSet := findTool(t, tools, "redis_sorted_set")
	if !hasJSONSchemaTypes(operationArgumentSchema(t, sortedSet.Tool.RawInputSchema, "count_by_score", "min"), "number", "string") {
		t.Fatal("count_by_score min schema must accept number|string")
	}
	if schema := operationArgumentSchema(t, sortedSet.Tool.RawInputSchema, "lex_count", "min"); schema["type"] != "string" {
		t.Fatalf("lex_count min schema type = %#v, want string", schema["type"])
	}

	for _, testCase := range []struct {
		tool      string
		operation string
	}{
		{"redis_string_write", "set"},
		{"redis_string_write", "delete_if"},
	} {
		tool := findTool(t, tools, testCase.tool)
		if !hasFixedWidthDigestSchema(operationArgumentSchema(t, tool.Tool.RawInputSchema, testCase.operation, "comparison")) {
			t.Fatalf("%s/%s comparison schema lacks a fixed-width digest", testCase.tool, testCase.operation)
		}
	}
}

func TestGeneratedSchemaRejectsNonEqualConditionalPrevious(t *testing.T) {
	tool := findTool(t, redistoolset.NewToolset(nil, true).GetTools(), "redis_string_write")
	var schema map[string]any
	if err := json.Unmarshal(tool.Tool.RawInputSchema, &schema); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}
	for _, value := range operationAllOf(t, schema, "set") {
		constraint, _ := value.(map[string]any)
		condition, _ := constraint["if"].(map[string]any)
		properties, _ := condition["properties"].(map[string]any)
		conditionSchema, _ := properties["condition"].(map[string]any)
		returnPreviousSchema, _ := properties["return_previous"].(map[string]any)
		if !hasStringValues(conditionSchema["enum"], "ifne", "ifdne") || returnPreviousSchema["const"] != true {
			continue
		}
		then, _ := constraint["then"].(map[string]any)
		forbidden, present := then["not"].(map[string]any)
		if present && len(forbidden) == 0 {
			return
		}
	}
	t.Fatal("conditional SET schema must reject ifne|ifdne with return_previous=true")
}

func hasStringValues(value any, values ...string) bool {
	actual, _ := value.([]any)
	if len(actual) != len(values) {
		return false
	}
	for _, expected := range values {
		found := false
		for _, candidate := range actual {
			if candidate == expected {
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

func hasFixedWidthDigestSchema(schema map[string]any) bool {
	variants, _ := schema["oneOf"].([]any)
	for _, value := range variants {
		candidate, _ := value.(map[string]any)
		if candidate["type"] != "string" || candidate["pattern"] != "^[0-9a-f]{16}$" {
			continue
		}
		not, _ := candidate["not"].(map[string]any)
		if not["const"] != "0000000000000000" {
			continue
		}
		return true
	}
	return false
}
