// Package redis exposes the closed Redis semantic-tool catalog.
package redis

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/futuretea/redis-mcp-server/pkg/toolset"
)

const Domain = "redis"

// Adapter is the narrow internal boundary used by Redis tool handlers. It is
// intentionally operation-based: callers cannot supply a Redis command.
type Adapter interface {
	Call(context.Context, Call) (map[string]any, error)
}

// Call is a validated semantic Redis operation.
type Call struct {
	Tool      string
	Operation string
	Args      map[string]any
	Write     bool
}

// Toolset provides the Redis data-plane tools declared by the proposal.
type Toolset struct {
	adapter      Adapter
	writeEnabled bool
}

// NewToolset constructs a catalog without opening a Redis connection.
func NewToolset(adapter Adapter, writeEnabled bool) *Toolset {
	return &Toolset{adapter: adapter, writeEnabled: writeEnabled}
}

func (t *Toolset) GetName() string { return "redis" }

func (t *Toolset) GetDescription() string {
	return "Closed Redis semantic data-plane tools"
}

func (t *Toolset) GetTools() []toolset.ServerTool {
	specs := readSpecs
	if t.writeEnabled {
		specs = append(append([]toolSpec(nil), specs...), writeSpecs...)
	}

	tools := make([]toolset.ServerTool, 0, len(specs))
	for _, spec := range specs {
		tools = append(tools, toolset.ServerTool{
			Tool:    mcp.NewToolWithRawSchema(spec.name, spec.description, spec.schema()),
			Handler: t.handler(spec),
			Domain:  Domain,
		})
	}
	return tools
}

type toolSpec struct {
	name        string
	description string
	operations  []string
	write       bool
}

func (s toolSpec) schema() json.RawMessage {
	variants := make([]map[string]any, 0, len(s.operations))
	for _, operation := range s.operations {
		contract, ok := contracts[s.name+"/"+operation]
		if !ok {
			panic(fmt.Sprintf("missing Redis contract for %s/%s", s.name, operation))
		}
		properties := make(map[string]any, len(contract.required)+len(contract.optional))
		for _, name := range append(append([]string(nil), contract.required...), contract.optional...) {
			properties[name] = schemaForArgument(s.name, operation, name, contract)
		}
		argsSchema := map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             contract.required,
			"properties":           properties,
		}
		if s.name == "redis_string_write" && operation == "set" {
			argsSchema["allOf"] = conditionalSetSchema()
		}
		if s.name == "redis_string_write" && operation == "delete_if" {
			argsSchema["allOf"] = conditionalDeleteSchema()
		}
		if s.name == "redis_sorted_set_write" && operation == "add" {
			argsSchema["allOf"] = sortedAddSchema()
		}
		if s.name == "redis_stream_write" && operation == "trim" {
			argsSchema["allOf"] = streamTrimSchema()
		}
		if s.name == "redis_stream_write" && operation == "add" {
			argsSchema["allOf"] = streamAddSchema()
		}
		if s.name == "redis_sorted_set" && operation == "range" {
			argsSchema["allOf"] = sortedRangeSchema()
		}
		if s.name == "redis_stream_write" && operation == "group" {
			argsSchema["allOf"] = streamGroupSchema()
		}
		if hasNumberByKindArgument(s.name, operation) {
			argsSchema["allOf"] = numberKindAmountSchema()
		}
		if constraints := variantScalarSchema(s.name, operation, contract); len(constraints) > 0 {
			appendSchemaConstraints(argsSchema, constraints)
		}
		variants = append(variants, map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"operation", "args"},
			"properties": map[string]any{
				"operation": map[string]any{"const": operation},
				"args":      argsSchema,
			},
		})
	}
	schema, err := json.Marshal(map[string]any{"oneOf": variants})
	if err != nil {
		panic(fmt.Sprintf("marshal Redis tool schema: %v", err))
	}
	return schema
}

func schemaForArgument(tool, operation, name string, contract operationContract) map[string]any {
	if name == "comparison" {
		return map[string]any{"oneOf": []map[string]any{binarySchema(), digestComparisonSchema()}}
	}
	if isKeyArgument(name) {
		return nonEmptyStringSchema()
	}
	if values, ok := contract.enums[name]; ok {
		return map[string]any{"type": "string", "enum": values}
	}
	if contains(contract.binary, name) {
		return binarySchema()
	}
	if kind, ok := contract.arrays[name]; ok {
		if name == "keys" {
			return keyArraySchema()
		}
		return arraySchema(kind)
	}
	if contains(contract.binaryLists, name) {
		return arraySchema("binary")
	}
	if name == "expiry" {
		return expirySchema(tool, operation)
	}
	if name == "cursor" {
		return map[string]any{"type": "string", "pattern": "^[A-Za-z0-9_-]{22}$"}
	}
	return schemaForScalarType(scalarTypeFor(tool, operation, name))
}

func schemaForScalarType(argumentType scalarArgumentType) map[string]any {
	switch argumentType {
	case scalarAny:
		return map[string]any{}
	case scalarInteger:
		return map[string]any{"type": "integer"}
	case scalarNumber, scalarNumberByKind:
		return map[string]any{"type": "number"}
	case scalarRedisBound:
		return map[string]any{"oneOf": []map[string]any{{"type": "number"}, {"type": "string"}}}
	case scalarBoolean:
		return map[string]any{"type": "boolean"}
	case scalarByVariant:
		return map[string]any{}
	default:
		return map[string]any{"type": "string"}
	}
}

func variantScalarSchema(tool, operation string, contract operationContract) []map[string]any {
	argumentVariants, ok := variantScalarArgumentTypes[tool+"/"+operation]
	if !ok {
		return nil
	}
	byValues := contract.enums["by"]
	variants := make([]map[string]any, 0, len(byValues))
	for _, by := range byValues {
		properties := map[string]any{"by": map[string]any{"const": by}}
		for name, types := range argumentVariants {
			argumentType, ok := types[by]
			if !ok {
				continue
			}
			properties[name] = schemaForScalarType(argumentType)
		}
		variants = append(variants, map[string]any{"properties": properties})
	}
	return []map[string]any{{"oneOf": variants}}
}

func appendSchemaConstraints(argsSchema map[string]any, constraints []map[string]any) {
	if allOf, ok := argsSchema["allOf"].([]map[string]any); ok {
		argsSchema["allOf"] = append(allOf, constraints...)
		return
	}
	argsSchema["allOf"] = constraints
}

func hasNumberByKindArgument(tool, operation string) bool {
	for _, argumentType := range scalarArgumentTypes[tool+"/"+operation] {
		if argumentType == scalarNumberByKind {
			return true
		}
	}
	return false
}

func numberKindAmountSchema() []map[string]any {
	return []map[string]any{{
		"if": map[string]any{
			"required": []string{"number_kind"},
			"properties": map[string]any{
				"number_kind": map[string]any{"const": "float"},
			},
		},
		"then": map[string]any{"properties": map[string]any{"amount": map[string]any{"type": "number"}}},
		"else": map[string]any{"properties": map[string]any{"amount": map[string]any{"type": "integer"}}},
	}}
}

func streamTrimSchema() []map[string]any {
	return []map[string]any{
		{
			"if":   map[string]any{"properties": map[string]any{"strategy": map[string]any{"const": "maxlen"}}},
			"then": map[string]any{"properties": map[string]any{"threshold": map[string]any{"type": "integer", "minimum": 0}}},
		},
		{
			"if":   map[string]any{"properties": map[string]any{"strategy": map[string]any{"const": "minid"}}},
			"then": map[string]any{"properties": map[string]any{"threshold": map[string]any{"type": "string"}}},
		},
		{
			"if":   map[string]any{"required": []string{"limit"}},
			"then": map[string]any{"required": []string{"approximate"}, "properties": map[string]any{"approximate": map[string]any{"const": true}}},
		},
	}
}

func streamAddSchema() []map[string]any {
	return []map[string]any{
		{
			"oneOf": []map[string]any{
				{"required": []string{"maxlen"}, "not": map[string]any{"required": []string{"minid"}}},
				{"required": []string{"minid"}, "not": map[string]any{"required": []string{"maxlen"}}},
			},
		},
		{
			"if":   map[string]any{"required": []string{"limit"}},
			"then": map[string]any{"required": []string{"approximate"}, "properties": map[string]any{"approximate": map[string]any{"const": true}}},
		},
	}
}

func sortedRangeSchema() []map[string]any {
	without := func(name string) map[string]any {
		return map[string]any{"not": map[string]any{"required": []string{name}}}
	}
	return []map[string]any{
		{
			"oneOf": []map[string]any{
				{"properties": map[string]any{"by": map[string]any{"const": "rank"}}, "required": []string{"start", "count"}, "allOf": []map[string]any{without("min"), without("max"), without("offset")}},
				{"properties": map[string]any{"by": map[string]any{"const": "score"}}, "required": []string{"min", "max", "count"}, "allOf": []map[string]any{without("start")}},
				{"properties": map[string]any{"by": map[string]any{"const": "lex"}}, "required": []string{"min", "max", "count"}, "allOf": []map[string]any{without("start")}},
			},
		},
	}
}

func streamGroupSchema() []map[string]any {
	variant := func(action string, required, forbidden []string) map[string]any {
		checks := make([]map[string]any, 0, len(forbidden))
		for _, name := range forbidden {
			checks = append(checks, map[string]any{"not": map[string]any{"required": []string{name}}})
		}
		return map[string]any{"properties": map[string]any{"action": map[string]any{"const": action}}, "required": required, "allOf": checks}
	}
	return []map[string]any{{"oneOf": []map[string]any{
		variant("create", []string{"id"}, []string{"consumer"}),
		variant("create_consumer", []string{"consumer"}, []string{"id", "mkstream"}),
		variant("delete_consumer", []string{"consumer"}, []string{"id", "mkstream"}),
		variant("destroy", nil, []string{"consumer", "id", "mkstream"}),
		variant("set_id", []string{"id"}, []string{"consumer", "mkstream"}),
	}}}
}

func conditionalSetSchema() []map[string]any {
	return []map[string]any{
		{
			"if":   map[string]any{"required": []string{"condition"}, "properties": map[string]any{"condition": map[string]any{"enum": []string{"ifeq", "ifne"}}}},
			"then": map[string]any{"required": []string{"comparison"}, "properties": map[string]any{"comparison": binarySchema()}},
		},
		{
			"if":   map[string]any{"required": []string{"condition"}, "properties": map[string]any{"condition": map[string]any{"enum": []string{"ifdeq", "ifdne"}}}},
			"then": map[string]any{"required": []string{"comparison"}, "properties": map[string]any{"comparison": digestComparisonSchema()}},
		},
		{
			"if":   map[string]any{"required": []string{"comparison"}},
			"then": map[string]any{"required": []string{"condition"}, "properties": map[string]any{"condition": map[string]any{"enum": []string{"ifeq", "ifne", "ifdeq", "ifdne"}}}},
		},
		{
			"if": map[string]any{
				"required": []string{"condition", "return_previous"},
				"properties": map[string]any{
					"condition":       map[string]any{"enum": []string{"ifne", "ifdne"}},
					"return_previous": map[string]any{"const": true},
				},
			},
			"then": map[string]any{"not": map[string]any{}},
		},
	}
}

func conditionalDeleteSchema() []map[string]any {
	return []map[string]any{
		{
			"if":   map[string]any{"properties": map[string]any{"condition": map[string]any{"enum": []string{"ifeq", "ifne"}}}},
			"then": map[string]any{"properties": map[string]any{"comparison": binarySchema()}},
		},
		{
			"if":   map[string]any{"properties": map[string]any{"condition": map[string]any{"enum": []string{"ifdeq", "ifdne"}}}},
			"then": map[string]any{"properties": map[string]any{"comparison": digestComparisonSchema()}},
		},
	}
}

func digestComparisonSchema() map[string]any {
	return map[string]any{
		"type":    "string",
		"pattern": "^[0-9a-f]{16}$",
		"not":     map[string]any{"const": "0000000000000000"},
	}
}

func sortedAddSchema() []map[string]any {
	return []map[string]any{
		{
			"if": map[string]any{
				"required":   []string{"increment"},
				"properties": map[string]any{"increment": map[string]any{"const": true}},
			},
			"then": map[string]any{
				"properties": map[string]any{"items": map[string]any{"maxItems": 1}},
			},
		},
	}
}

func arraySchema(kind string) map[string]any {
	item := map[string]any{}
	switch kind {
	case "string":
		item["type"] = "string"
	case "number":
		item["type"] = "number"
	case "binary":
		item = binarySchema()
	case "key_value":
		item = objectSchema(map[string]any{"key": nonEmptyStringSchema(), "value": binarySchema()})
	case "field_value":
		item = objectSchema(map[string]any{"field": binarySchema(), "value": binarySchema()})
	case "score_member":
		item = objectSchema(map[string]any{"score": map[string]any{"type": "number"}, "member": binarySchema()})
	case "stream":
		item = objectSchema(map[string]any{"key": nonEmptyStringSchema(), "id": map[string]any{"type": "string"}})
	default:
		item["type"] = "object"
	}
	return map[string]any{"type": "array", "minItems": 1, "maxItems": 1000, "items": item}
}

func nonEmptyStringSchema() map[string]any { return map[string]any{"type": "string", "minLength": 1} }

func keyArraySchema() map[string]any {
	return map[string]any{"type": "array", "minItems": 1, "maxItems": 1000, "items": nonEmptyStringSchema()}
}

func objectSchema(properties map[string]any) map[string]any {
	required := make([]string, 0, len(properties))
	for name := range properties {
		required = append(required, name)
	}
	return map[string]any{"type": "object", "additionalProperties": false, "required": required, "properties": properties}
}

func expirySchema(tool, operation string) map[string]any {
	withValue := func(kinds []string) map[string]any {
		return objectSchema(map[string]any{"kind": map[string]any{"type": "string", "enum": kinds}, "value": map[string]any{"type": "integer", "minimum": 1}})
	}
	withoutValue := func(kinds []string) map[string]any {
		return objectSchema(map[string]any{"kind": map[string]any{"type": "string", "enum": kinds}})
	}
	withValueKinds, withoutValueKinds := make([]string, 0), make([]string, 0)
	for _, kind := range expiryKindsForOperation(tool, operation) {
		if kind == "persist" || kind == "keep_ttl" {
			withoutValueKinds = append(withoutValueKinds, kind)
		} else {
			withValueKinds = append(withValueKinds, kind)
		}
	}
	variants := make([]map[string]any, 0, 2)
	if len(withValueKinds) > 0 {
		variants = append(variants, withValue(withValueKinds))
	}
	if len(withoutValueKinds) > 0 {
		variants = append(variants, withoutValue(withoutValueKinds))
	}
	return map[string]any{"oneOf": variants}
}

func binarySchema() map[string]any {
	return map[string]any{"oneOf": []map[string]any{
		{"type": "object", "additionalProperties": false, "required": []string{"utf8"}, "properties": map[string]any{"utf8": map[string]any{"type": "string"}}},
		{"type": "object", "additionalProperties": false, "required": []string{"base64"}, "properties": map[string]any{"base64": map[string]any{"type": "string"}}},
	}}
}

func (t *Toolset) handler(spec toolSpec) toolset.ToolHandler {
	return func(ctx context.Context, params map[string]any) (string, error) {
		call, err := validateCall(spec, params)
		if err != nil {
			return errorEnvelope(operationFrom(params), "invalid_argument", err.Error()), nil
		}
		if t.adapter == nil {
			return errorEnvelope(call.Operation, "unavailable", "Redis adapter is not configured"), nil
		}
		data, err := t.adapter.Call(ctx, call)
		if err != nil {
			if callErr, ok := err.(*CallError); ok {
				if callErr.Category == "not_found" {
					return notFoundEnvelope(call.Operation)
				}
				if callErr.Category == "condition_not_met" {
					return conditionEnvelope(call.Operation)
				}
				return errorEnvelope(call.Operation, callErr.Category, callErr.Message), nil
			}
			return errorEnvelope(call.Operation, "unavailable", err.Error()), nil
		}
		return okEnvelope(call.Operation, data)
	}
}

var readSpecs = []toolSpec{
	{name: "redis_key", description: "Inspect Redis keys", operations: []string{"exists", "random_key", "type", "expiry", "dump", "object_attribute"}},
	{name: "redis_string", description: "Read Redis strings", operations: []string{"get", "get_range", "get_many", "length", "digest", "longest_common_substring"}},
	{name: "redis_hash", description: "Read Redis hashes", operations: []string{"field_exists", "get", "length", "get_many", "field_expiry", "random_field", "scan", "value_length"}},
	{name: "redis_list", description: "Read Redis lists", operations: []string{"index", "length", "position", "range"}},
	{name: "redis_set", description: "Read Redis sets", operations: []string{"cardinality", "intersection_cardinality", "member_exists", "members_exist", "random_members", "scan"}},
	{name: "redis_sorted_set", description: "Read Redis sorted sets", operations: []string{"cardinality", "count_by_score", "intersection_cardinality", "lex_count", "scores", "random_members", "range", "rank", "scan", "score"}},
	{name: "redis_stream", description: "Read Redis streams without entries", operations: []string{"length", "pending"}},
	{name: "redis_observe", description: "Observe Redis keyspace state", operations: []string{"scan", "db_size", "memory_usage"}},
}

var writeSpecs = []toolSpec{
	{name: "redis_key_write", description: "Modify Redis keys", write: true, operations: []string{"copy", "delete", "set_expiry", "persist_expiry", "rename", "restore", "touch"}},
	{name: "redis_string_write", description: "Modify Redis strings", write: true, operations: []string{"append", "decrement", "delete_if", "get_and_delete", "get_and_set_expiry", "get_and_set", "increment", "set_many", "set", "set_range"}},
	{name: "redis_hash_write", description: "Modify Redis hashes", write: true, operations: []string{"delete_fields", "set_field_expiry", "persist_field_expiry", "get_and_delete", "get_and_set_expiry", "increment", "set_fields", "set_fields_with_expiry", "set_if_absent"}},
	{name: "redis_list_write", description: "Modify Redis lists", write: true, operations: []string{"insert", "move", "pop", "pop_many", "push", "push_if_exists", "remove", "set", "trim"}},
	{name: "redis_set_write", description: "Modify Redis sets", write: true, operations: []string{"add", "store_difference", "store_intersection", "move", "pop", "remove", "store_union"}},
	{name: "redis_sorted_set_write", description: "Modify Redis sorted sets", write: true, operations: []string{"add", "store_difference", "increment", "store_intersection", "pop", "remove", "remove_range", "store_range", "store_union"}},
	{name: "redis_stream_write", description: "Modify Redis streams", write: true, operations: []string{"group", "ack", "add", "claim", "delete", "nack", "trim"}},
}
