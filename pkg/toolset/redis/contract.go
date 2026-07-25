package redis

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

const continuationReferenceLength = 22

type CallError struct{ Category, Message string }

func (e *CallError) Error() string { return e.Message }

func validateCall(spec toolSpec, params map[string]any) (Call, error) {
	if len(params) != 2 {
		return Call{}, fmt.Errorf("expected only operation and args")
	}
	operation, ok := params["operation"].(string)
	if !ok || operation == "" {
		return Call{}, fmt.Errorf("operation must be a non-empty string")
	}
	if !contains(spec.operations, operation) {
		return Call{}, fmt.Errorf("operation %q is not supported by %s", operation, spec.name)
	}
	args, ok := params["args"].(map[string]any)
	if !ok || args == nil {
		return Call{}, fmt.Errorf("args must be an object")
	}
	contract, ok := contracts[spec.name+"/"+operation]
	if !ok {
		return Call{}, fmt.Errorf("missing contract for %s/%s", spec.name, operation)
	}
	if err := validateArgs(spec.name, operation, contract, args); err != nil {
		return Call{}, err
	}
	if err := validateVariants(spec.name, operation, args); err != nil {
		return Call{}, err
	}
	if contract.sameSlot && !sameSlot(keysForCall(args)) {
		return Call{}, fmt.Errorf("cross_slot: all keys must share a Redis hash slot")
	}
	return Call{Tool: spec.name, Operation: operation, Args: args, Write: spec.write}, nil
}

func validateArgs(tool, operation string, contract operationContract, args map[string]any) error {
	allowed := append(append([]string(nil), contract.required...), contract.optional...)
	for name := range args {
		if !contains(allowed, name) {
			return fmt.Errorf("argument %q is not supported", name)
		}
	}
	for _, name := range contract.required {
		if _, ok := args[name]; !ok {
			return fmt.Errorf("argument %q is required", name)
		}
	}
	for name, values := range contract.enums {
		if value, present := args[name]; present {
			text, ok := value.(string)
			if !ok || !contains(values, text) {
				return fmt.Errorf("argument %q must be one of %v", name, values)
			}
		}
	}
	for _, name := range contract.binary {
		if value, present := args[name]; present {
			if name == "comparison" && digestCondition(args) {
				if err := validateDigestComparison(value); err != nil {
					return fmt.Errorf("argument comparison %w", err)
				}
				continue
			}
			if err := validateBinary(value); err != nil {
				return fmt.Errorf("argument %q: %w", name, err)
			}
		}
	}
	for name, kind := range contract.arrays {
		if value, present := args[name]; present {
			if err := validateArray(name, kind, value); err != nil {
				return err
			}
		}
	}
	for _, name := range contract.binaryLists {
		if value, present := args[name]; present {
			if err := validateBinaryList(name, value); err != nil {
				return err
			}
		}
	}
	for name, value := range args {
		if contractValueIsStructured(contract, name) {
			continue
		}
		if err := validateScalar(tool, operation, name, value, args); err != nil {
			return err
		}
	}
	if cursor, present := args["cursor"]; present {
		if err := validateContinuationReference(cursor); err != nil {
			return err
		}
	}
	if value, present := args["expiry"]; present {
		if err := validateExpiry(value); err != nil {
			return err
		}
		if err := validateExpiryForOperation(tool, operation, value); err != nil {
			return err
		}
	}
	return validateCollectionBounds(tool, operation, args)
}

func digestCondition(args map[string]any) bool {
	condition, _ := args["condition"].(string)
	return condition == "ifdeq" || condition == "ifdne"
}

func validateDigestComparison(value any) error {
	text, ok := value.(string)
	if !ok || len(text) != 16 {
		return fmt.Errorf("must be a 16-character lowercase hexadecimal digest")
	}
	digest, err := strconv.ParseUint(text, 16, 64)
	if err != nil || fmt.Sprintf("%016x", digest) != text {
		return fmt.Errorf("must be a 16-character lowercase hexadecimal digest")
	}
	if digest == 0 {
		return fmt.Errorf("must be a non-zero 16-character lowercase hexadecimal digest")
	}
	return nil
}

func contractValueIsStructured(contract operationContract, name string) bool {
	if _, ok := contract.enums[name]; ok {
		return true
	}
	if contains(contract.binary, name) || contains(contract.binaryLists, name) || name == "expiry" || name == "cursor" {
		return true
	}
	_, isArray := contract.arrays[name]
	return isArray
}

func validateScalar(tool, operation, name string, value any, args map[string]any) error {
	if isKeyArgument(name) {
		if err := nonEmptyStringValue(value); err != nil {
			return fmt.Errorf("argument %q: %w", name, err)
		}
		return nil
	}
	switch scalarTypeForArgs(tool, operation, name, args) {
	case scalarAny:
		return nil
	case scalarString:
		if _, ok := value.(string); !ok {
			return fmt.Errorf("argument %q must be a string", name)
		}
	case scalarInteger:
		if _, ok := integer(value); !ok {
			return fmt.Errorf("argument %q must be an integer", name)
		}
	case scalarNumber:
		if _, ok := number(value); !ok {
			return fmt.Errorf("argument %q must be a number", name)
		}
	case scalarRedisBound:
		if _, stringOK := value.(string); !stringOK {
			if _, numberOK := number(value); !numberOK {
				return fmt.Errorf("argument %q must be a number or Redis bound string", name)
			}
		}
	case scalarBoolean:
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("argument %q must be a boolean", name)
		}
	case scalarNumberByKind:
		if _, ok := number(value); !ok {
			return fmt.Errorf("argument %q must be a number", name)
		}
		if args["number_kind"] != "float" {
			if _, ok := integer(value); !ok {
				return fmt.Errorf("argument %q must be an integer unless number_kind is float", name)
			}
		}
	case scalarByVariant:
		return fmt.Errorf("argument %q has no type for variant %q", name, args["by"])
	}
	return nil
}

func validateArray(name, kind string, value any) error {
	values, ok := value.([]any)
	if !ok || len(values) == 0 || len(values) > 1000 {
		return fmt.Errorf("argument %q must be a non-empty array", name)
	}
	for _, value := range values {
		switch kind {
		case "string":
			if name == "keys" {
				if err := nonEmptyStringValue(value); err != nil {
					return fmt.Errorf("argument %q: %w", name, err)
				}
				continue
			}
			if _, ok := value.(string); !ok {
				return fmt.Errorf("argument %q must contain strings", name)
			}
		case "number":
			if _, ok := number(value); !ok {
				return fmt.Errorf("argument %q must contain numbers", name)
			}
		case "binary":
			if err := validateBinary(value); err != nil {
				return fmt.Errorf("argument %q: %w", name, err)
			}
		case "key_value":
			if err := validateObject(value, map[string]func(any) error{"key": nonEmptyStringValue, "value": validateBinary}); err != nil {
				return fmt.Errorf("argument %q: %w", name, err)
			}
		case "field_value":
			if err := validateObject(value, map[string]func(any) error{"field": validateBinary, "value": validateBinary}); err != nil {
				return fmt.Errorf("argument %q: %w", name, err)
			}
		case "score_member":
			if err := validateObject(value, map[string]func(any) error{"score": numberValue, "member": validateBinary}); err != nil {
				return fmt.Errorf("argument %q: %w", name, err)
			}
		case "stream":
			if err := validateObject(value, map[string]func(any) error{"key": nonEmptyStringValue, "id": stringValue}); err != nil {
				return fmt.Errorf("argument %q: %w", name, err)
			}
		default:
			return fmt.Errorf("argument %q has unsupported array contract", name)
		}
	}
	return nil
}

func validateBinaryList(name string, value any) error { return validateArray(name, "binary", value) }

func validateObject(value any, fields map[string]func(any) error) error {
	object, ok := value.(map[string]any)
	if !ok || len(object) != len(fields) {
		return fmt.Errorf("must be an object with only %v", keys(fields))
	}
	for name, validate := range fields {
		field, present := object[name]
		if !present {
			return fmt.Errorf("field %q is required", name)
		}
		if err := validate(field); err != nil {
			return fmt.Errorf("field %q: %w", name, err)
		}
	}
	return nil
}

func validateBinary(value any) error {
	object, ok := value.(map[string]any)
	if !ok || len(object) != 1 {
		return fmt.Errorf("must be exactly one of utf8 or base64")
	}
	if text, present := object["utf8"]; present {
		if _, ok := text.(string); !ok {
			return fmt.Errorf("utf8 must be a string")
		}
		return nil
	}
	if encoded, present := object["base64"]; present {
		text, ok := encoded.(string)
		if !ok {
			return fmt.Errorf("base64 must be a string")
		}
		if _, err := base64.StdEncoding.DecodeString(text); err != nil {
			return fmt.Errorf("base64 is invalid")
		}
		return nil
	}
	return fmt.Errorf("must be exactly one of utf8 or base64")
}

func validateExpiry(value any) error {
	object, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("argument expiry must be an object")
	}
	kind, ok := object["kind"].(string)
	if !ok || !contains([]string{"ex", "px", "exat", "pxat", "persist", "keep_ttl"}, kind) {
		return fmt.Errorf("argument expiry.kind is invalid")
	}
	_, hasValue := object["value"]
	if (kind == "persist" || kind == "keep_ttl") && hasValue {
		return fmt.Errorf("argument expiry.%s must not include value", kind)
	}
	if kind != "persist" && kind != "keep_ttl" {
		if !hasValue {
			return fmt.Errorf("argument expiry.%s requires value", kind)
		}
		if value, ok := integer(object["value"]); !ok || value <= 0 {
			return fmt.Errorf("argument expiry.value must be a positive integer")
		}
	}
	for name := range object {
		if name != "kind" && name != "value" {
			return fmt.Errorf("argument expiry has unsupported field %q", name)
		}
	}
	return nil
}

func validateContinuationReference(value any) error {
	text, ok := value.(string)
	if !ok || len(text) != continuationReferenceLength {
		return fmt.Errorf("argument cursor must be a fixed-length opaque continuation reference")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(text)
	if err != nil || len(decoded) != 16 {
		return fmt.Errorf("argument cursor must be a fixed-length opaque continuation reference")
	}
	return nil
}

func validateExpiryForOperation(tool, operation string, value any) error {
	expiry := value.(map[string]any)
	kind := expiry["kind"].(string)
	if !contains(expiryKindsForOperation(tool, operation), kind) {
		key := tool + "/" + operation
		return fmt.Errorf("expiry kind %q is not supported for %s", kind, key)
	}
	return nil
}

func expiryKindsForOperation(tool, operation string) []string {
	if kinds, ok := map[string][]string{
		"redis_key_write/set_expiry":              {"ex", "px", "exat", "pxat"},
		"redis_string_write/get_and_set_expiry":   {"ex", "px", "persist"},
		"redis_string_write/increment":            {"ex", "px", "exat", "pxat", "persist"},
		"redis_string_write/set_many":             {"ex", "px", "exat", "pxat", "keep_ttl"},
		"redis_string_write/set":                  {"ex", "px", "exat", "keep_ttl"},
		"redis_hash_write/set_field_expiry":       {"ex", "px", "exat", "pxat"},
		"redis_hash_write/get_and_set_expiry":     {"ex", "px", "exat", "pxat", "persist"},
		"redis_hash_write/set_fields_with_expiry": {"ex", "px", "exat", "pxat", "keep_ttl"},
	}[tool+"/"+operation]; ok {
		return kinds
	}
	return []string{"ex", "px", "exat", "pxat", "persist", "keep_ttl"}
}

func validateVariants(tool, operation string, args map[string]any) error {
	for _, validate := range variantValidators {
		if err := validate(tool, operation, args); err != nil {
			return err
		}
	}
	return nil
}

func validateCollectionBounds(tool, operation string, args map[string]any) error {
	key := tool + "/" + operation
	if requiresBoundedCount(key) {
		value, present := args["count"]
		if !present {
			return fmt.Errorf("argument count is required")
		}
		if err := validateBoundedCount(value); err != nil {
			return err
		}
	} else if value, present := args["count"]; present && countBoundsWhenPresent(key) {
		if err := validateBoundedCount(value); err != nil {
			return err
		}
	}

	for _, name := range boundedVectors(key) {
		values, ok := args[name].([]any)
		if !ok || len(values) == 0 || len(values) > 1000 {
			return fmt.Errorf("argument %q must contain between 1 and 1000 items", name)
		}
	}
	return nil
}

func validateBoundedCount(value any) error {
	count, ok := integer(value)
	if !ok || count < 1 || count > 1000 {
		return fmt.Errorf("argument count must be an integer from 1 through 1000")
	}
	return nil
}

func requiresBoundedCount(key string) bool {
	return contains([]string{
		"redis_observe/scan", "redis_hash/scan", "redis_set/scan", "redis_sorted_set/scan",
		"redis_list/range", "redis_sorted_set/range", "redis_stream/pending",
	}, key)
}

func countBoundsWhenPresent(key string) bool {
	return contains([]string{
		"redis_hash/random_field", "redis_list/position", "redis_list_write/pop",
		"redis_list_write/pop_many", "redis_set/random_members", "redis_set_write/pop",
		"redis_sorted_set/random_members", "redis_sorted_set_write/pop",
	}, key)
}

func boundedVectors(key string) []string {
	switch key {
	case "redis_key/exists", "redis_string/get_many":
		return []string{"keys"}
	case "redis_hash/get_many", "redis_hash/field_expiry", "redis_hash_write/delete_fields", "redis_hash_write/set_field_expiry", "redis_hash_write/persist_field_expiry", "redis_hash_write/get_and_delete", "redis_hash_write/get_and_set_expiry":
		return []string{"fields"}
	case "redis_set/members_exist", "redis_sorted_set/scores", "redis_set_write/add", "redis_set_write/remove", "redis_sorted_set_write/remove":
		return []string{"members"}
	case "redis_stream_write/ack", "redis_stream_write/claim", "redis_stream_write/delete", "redis_stream_write/nack":
		return []string{"ids"}
	default:
		return nil
	}
}

func keysForCall(args map[string]any) []string {
	keys := make([]string, 0)
	for _, name := range []string{"key", "source_key", "destination_key", "first_key", "second_key"} {
		if key, ok := args[name].(string); ok {
			keys = append(keys, key)
		}
	}
	for _, name := range []string{"keys"} {
		if values, ok := args[name].([]any); ok {
			for _, value := range values {
				if key, ok := value.(string); ok {
					keys = append(keys, key)
				}
			}
		}
	}
	for _, name := range []string{"items", "streams"} {
		if values, ok := args[name].([]any); ok {
			for _, value := range values {
				if item, ok := value.(map[string]any); ok {
					if key, ok := item["key"].(string); ok {
						keys = append(keys, key)
					}
				}
			}
		}
	}
	return keys
}

func sameSlot(keys []string) bool {
	if len(keys) < 2 {
		return true
	}
	want := hashSlot(keys[0])
	for _, key := range keys[1:] {
		if hashSlot(key) != want {
			return false
		}
	}
	return true
}

func hashSlot(key string) uint16 {
	for start := 0; start < len(key); start++ {
		if key[start] != '{' {
			continue
		}
		for end := start + 1; end < len(key); end++ {
			if key[end] == '}' {
				if end > start+1 {
					key = key[start+1 : end]
				}
				break
			}
		}
		break
	}
	var crc uint16
	for i := 0; i < len(key); i++ {
		crc ^= uint16(key[i]) << 8
		for bit := 0; bit < 8; bit++ {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc % 16384
}

func integer(value any) (int64, bool) {
	number, ok := value.(float64)
	if !ok || math.Trunc(number) != number || number < math.MinInt64 || number > math.MaxInt64 {
		return 0, false
	}
	return int64(number), true
}

func number(value any) (float64, bool) {
	number, ok := value.(float64)
	return number, ok && !math.IsNaN(number) && !math.IsInf(number, 0)
}
func stringValue(value any) error {
	if _, ok := value.(string); !ok {
		return fmt.Errorf("must be a string")
	}
	return nil
}
func nonEmptyStringValue(value any) error {
	text, ok := value.(string)
	if !ok || text == "" {
		return fmt.Errorf("must be a non-empty string")
	}
	return nil
}

func isKeyArgument(name string) bool {
	return contains([]string{"key", "source_key", "destination_key", "first_key", "second_key"}, name)
}
func numberValue(value any) error {
	if _, ok := number(value); !ok {
		return fmt.Errorf("must be a number")
	}
	return nil
}
func keys(fields map[string]func(any) error) []string {
	result := make([]string, 0, len(fields))
	for key := range fields {
		result = append(result, key)
	}
	return result
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func operationFrom(params map[string]any) string {
	operation, _ := params["operation"].(string)
	return operation
}
func okEnvelope(operation string, data map[string]any) (string, error) {
	return marshalEnvelope(map[string]any{"status": "ok", "operation": operation, "data": data})
}
func notFoundEnvelope(operation string) (string, error) {
	return marshalEnvelope(map[string]any{"status": "not_found", "operation": operation})
}
func conditionEnvelope(operation string) (string, error) {
	return marshalEnvelope(map[string]any{"status": "condition_not_met", "operation": operation})
}
func errorEnvelope(operation, category, message string) string {
	result, err := marshalEnvelope(map[string]any{"status": "error", "operation": operation, "error": map[string]string{"category": category, "message": message}})
	if err != nil {
		return `{"status":"error","operation":"","error":{"category":"unavailable","message":"encode result"}}`
	}
	return result
}
func marshalEnvelope(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal Redis result: %w", err)
	}
	return string(raw), nil
}
