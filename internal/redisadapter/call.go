package redisadapter

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/redis/go-redis/v9"

	redistoolset "github.com/futuretea/redis-mcp-server/pkg/toolset/redis"
)

// Call executes typed Redis client methods for closed semantic operations.
func (a *Adapter) Call(ctx context.Context, call redistoolset.Call) (map[string]any, error) {
	if a == nil || a.client == nil {
		return nil, callError("unavailable", "Redis adapter is not configured")
	}
	data, err := a.execute(ctx, call)
	if err == nil {
		return data, nil
	}
	var toolError *redistoolset.CallError
	if errors.As(err, &toolError) {
		return nil, toolError
	}
	if errors.Is(err, redis.Nil) {
		return nil, callError("not_found", "Redis value was not found")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		if call.Write {
			return nil, callError("deadline_outcome_unknown", "Redis write deadline exceeded")
		}
		return nil, callError("deadline", "Redis operation deadline exceeded")
	}
	message := err.Error()
	switch {
	case strings.HasPrefix(message, "WRONGTYPE"):
		return nil, callError("wrong_type", "Redis value has an incompatible type")
	case strings.HasPrefix(message, "NOAUTH"), strings.HasPrefix(message, "NOPERM"):
		return nil, callError("auth_acl", "Redis denied this operation")
	case strings.HasPrefix(message, "CROSSSLOT"):
		return nil, callError("cross_slot", "Redis keys do not share a hash slot")
	case strings.HasPrefix(message, "ERR "):
		return nil, callError("invalid_argument", "Redis rejected an operation argument")
	}
	return nil, callError("unavailable", "Redis operation failed")
}

func (a *Adapter) execute(ctx context.Context, call redistoolset.Call) (map[string]any, error) {
	if call.Write {
		return a.executeWrite(ctx, call)
	}
	return a.executeRead(ctx, call)
}

func callError(category, message string) error {
	return &redistoolset.CallError{Category: category, Message: message}
}
func stringArg(args map[string]any, name string) string {
	value, _ := args[name].(string)
	return value
}
func intArg(args map[string]any, name string) int {
	value, _ := args[name].(float64)
	return int(value)
}
func int64Arg(args map[string]any, name string) int64 { return int64(intArg(args, name)) }
func int64Value(value any, fallback int64) int64 {
	if value, ok := value.(float64); ok {
		return int64(value)
	}
	return fallback
}
func floatArg(value any, fallback float64) float64 {
	if value, ok := value.(float64); ok {
		return value
	}
	return fallback
}
func boolArg(args map[string]any, name string) bool { value, _ := args[name].(bool); return value }
func stringSlice(value any) []string {
	raw, _ := value.([]any)
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		if text, ok := item.(string); ok {
			result = append(result, text)
		}
	}
	return result
}
func binaryString(value any) string { text, _ := binaryInput(value); return text }
func binarySlice(value any) []string {
	raw, _ := value.([]any)
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		text, err := binaryInput(item)
		if err == nil {
			result = append(result, text)
		}
	}
	return result
}
func binaryInput(value any) (string, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return "", fmt.Errorf("binary must be an object")
	}
	if text, ok := object["utf8"].(string); ok {
		return text, nil
	}
	encoded, ok := object["base64"].(string)
	if !ok {
		return "", fmt.Errorf("binary must contain utf8 or base64")
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	return string(raw), err
}
func binaryOutput(value string) map[string]any {
	result := map[string]any{"base64": base64.StdEncoding.EncodeToString([]byte(value))}
	if utf8.ValidString(value) {
		result["utf8"] = value
	}
	return result
}
func binaryOutputs(values []string) []map[string]any {
	result := make([]map[string]any, 0, len(values))
	for _, value := range values {
		result = append(result, binaryOutput(value))
	}
	return result
}

func expiration(value any) (redis.ExpirationOption, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return redis.ExpirationOption{}, fmt.Errorf("expiry must be an object")
	}
	kind, _ := object["kind"].(string)
	option := redis.ExpirationOption{Value: int64Arg(object, "value")}
	switch kind {
	case "ex":
		option.Mode = redis.EX
	case "px":
		option.Mode = redis.PX
	case "exat":
		option.Mode = redis.EXAT
	case "pxat":
		option.Mode = redis.PXAT
	case "persist":
		option.Mode = redis.PERSIST
	case "keep_ttl":
		option.Mode = redis.KEEPTTL
	default:
		return redis.ExpirationOption{}, fmt.Errorf("unsupported expiry kind")
	}
	return option, nil
}

func keyValues(value any) ([]interface{}, error) {
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("items must be an array")
	}
	result := make([]interface{}, 0, len(items)*2)
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("item must be an object")
		}
		key, ok := item["key"].(string)
		if !ok {
			return nil, fmt.Errorf("item key must be a string")
		}
		value, err := binaryInput(item["value"])
		if err != nil {
			return nil, err
		}
		result = append(result, key, value)
	}
	return result, nil
}

func fieldValues(value any) ([]interface{}, error) {
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("fields must be an array")
	}
	result := make([]interface{}, 0, len(items)*2)
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("field item must be an object")
		}
		field, err := binaryInput(item["field"])
		if err != nil {
			return nil, err
		}
		value, err := binaryInput(item["value"])
		if err != nil {
			return nil, err
		}
		result = append(result, field, value)
	}
	return result, nil
}

func stringsFromInterfaces(values []interface{}) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, fmt.Sprint(value))
	}
	return result
}

func sortedItems(value any) ([]redis.Z, error) {
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("items must be an array")
	}
	result := make([]redis.Z, 0, len(items))
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("sorted set item must be an object")
		}
		member, err := binaryInput(item["member"])
		if err != nil {
			return nil, err
		}
		score, ok := item["score"].(float64)
		if !ok {
			return nil, fmt.Errorf("sorted set score must be a number")
		}
		result = append(result, redis.Z{Score: score, Member: member})
	}
	return result, nil
}
