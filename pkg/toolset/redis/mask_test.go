package redis

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/futuretea/redis-mcp-server/pkg/core/masking"
	"github.com/futuretea/redis-mcp-server/pkg/toolset"
)

const (
	maskSentinel = "synthetic-token-value"
	maskName     = "synthetic_field"
	maskVisible  = "ordinary-visible"
)

func TestInactiveMaskingPreservesLegacyEnvelope(t *testing.T) {
	data := map[string]any{
		"value":    binaryPair(maskSentinel),
		"count":    int64(4),
		"scalar":   nil,
		"metadata": map[string]any{"next_cursor": "ABCDEFGHIJKLMNOPQRSTUV", "key": maskSentinel},
	}
	adapter := &payloadAdapter{data: cloneMap(data)}
	tool := toolByName(t, NewToolset(adapter, false).GetTools(), "redis_string")
	got, err := tool.Handler(context.Background(), map[string]any{"operation": "get", "args": map[string]any{"key": "sample"}})
	if err != nil {
		t.Fatal(err)
	}
	want, err := okEnvelope("get", data)
	if err != nil {
		t.Fatal(err)
	}
	if got != want || !strings.Contains(got, maskSentinel) {
		t.Fatalf("inactive result = %s", got)
	}
	if adapter.calls != 1 || adapter.last.Args["key"] != "sample" {
		t.Fatalf("call = %#v", adapter.last)
	}
}

func TestActiveMaskingCoversEveryOperationWithoutExtraCalls(t *testing.T) {
	policy := maskPolicy(t, `{"enabled":true,"names":["synthetic_field"],"values":["synthetic-token-value"]}`)
	for name, contract := range contracts {
		toolName, operation, ok := strings.Cut(name, "/")
		if !ok {
			t.Fatalf("contract key %s", name)
		}
		t.Run(name, func(t *testing.T) {
			args := fixtureArgs(toolName, operation, contract)
			adapter := &payloadAdapter{data: richMaskedPayload()}
			toolset := NewToolset(adapter, true)
			toolset.SetPolicy(policy)
			tool := toolByName(t, toolset.GetTools(), toolName)
			result, err := tool.Handler(context.Background(), map[string]any{"operation": operation, "args": args})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(result, maskSentinel) || strings.Contains(result, maskName) {
				t.Fatalf("result leaked configured data: %s", result)
			}
			if adapter.calls != 1 {
				t.Fatalf("calls = %d", adapter.calls)
			}
			if !sameArgs(adapter.last.Args, args) {
				t.Fatalf("command args changed: %#v", adapter.last.Args)
			}
			if adapter.last.Operation != operation || adapter.last.Write != strings.HasSuffix(toolName, "_write") {
				t.Fatalf("call = %#v", adapter.last)
			}
			if name == "redis_key/dump" {
				if !strings.Contains(result, resultMaskedMessage) || strings.Contains(result, "base64") {
					t.Fatalf("dump result = %s", result)
				}
				return
			}
			if strings.Contains(result, resultMaskedMessage) {
				t.Fatalf("unexpected masking failure: %s", result)
			}
		})
	}
}

func TestMaskingShapes(t *testing.T) {
	policy := maskPolicy(t, `{"enabled":true,"names":["synthetic_field","alpha","beta"],"values":["synthetic-token-value","424242.5"],"value_patterns":["sk_live_[0-9]+"]}`)
	toolset := NewToolset(&payloadAdapter{}, true)
	toolset.SetPolicy(policy)

	t.Run("dynamic keys and nil", func(t *testing.T) {
		result := emit(t, toolset, "redis_string", "get_many", map[string]any{
			"values_by_key": map[string]any{
				maskSentinel: binaryPair(maskVisible),
				"plain":      nil,
			},
		})
		if strings.Contains(result, maskSentinel) || !strings.Contains(result, maskVisible) {
			t.Fatalf("dynamic value key = %s", result)
		}
		named := emit(t, toolset, "redis_string", "get_many", map[string]any{
			"values_by_key": map[string]any{maskName: binaryPair("secret-body"), "plain": nil},
		})
		if strings.Contains(named, maskName) || strings.Contains(named, "secret-body") {
			t.Fatalf("dynamic name key = %s", named)
		}
		decoded := decodeData(t, result)
		values := decoded["values_by_key"].(map[string]any)
		if _, ok := values["plain"]; !ok || values["plain"] != nil {
			t.Fatalf("nil shape = %#v", values)
		}
		if _, ok := values["[masked]"]; !ok {
			t.Fatalf("masked key missing: %#v", values)
		}
	})

	t.Run("key collision", func(t *testing.T) {
		result := emit(t, toolset, "redis_string", "get_many", map[string]any{
			"values_by_key": map[string]any{"alpha": nil, "beta": binaryPair(maskVisible)},
		})
		if !strings.Contains(result, resultMaskedMessage) || strings.Contains(result, "alpha") || strings.Contains(result, maskVisible) {
			t.Fatalf("collision result = %s", result)
		}
	})

	t.Run("scan keys and cursor", func(t *testing.T) {
		cursor := "ABCDEFGHIJKLMNOPQRSTUV"
		result := emit(t, toolset, "redis_observe", "scan", map[string]any{
			"items":    map[string]any{"keys": []string{maskSentinel, maskVisible}},
			"metadata": map[string]any{"next_cursor": cursor, "complete": false},
		})
		if strings.Contains(result, maskSentinel) || !strings.Contains(result, cursor) || !strings.Contains(result, maskVisible) {
			t.Fatalf("scan result = %s", result)
		}
		invalidKey := string([]byte{0xff, 0xfe}) + maskSentinel
		invalid := emit(t, toolset, "redis_observe", "scan", map[string]any{
			"items": map[string]any{"keys": []string{invalidKey}},
		})
		if !strings.Contains(invalid, resultMaskedMessage) || strings.Contains(invalid, maskSentinel) || strings.Contains(invalid, "\ufffd") {
			t.Fatalf("invalid scan key = %s", invalid)
		}
		failed := emit(t, toolset, "redis_observe", "scan", map[string]any{
			"items":    map[string]any{"keys": []string{"stored-continuation-key"}},
			"metadata": map[string]any{"next_cursor": "xx-" + maskSentinel, "key": "stored-continuation-key"},
		})
		if !strings.Contains(failed, resultMaskedMessage) || strings.Contains(failed, maskSentinel) || strings.Contains(failed, "stored-continuation-key") {
			t.Fatalf("cursor failure = %s", failed)
		}
	})

	t.Run("hash field and value", func(t *testing.T) {
		result := emit(t, toolset, "redis_hash", "get_many", map[string]any{
			"fields": []any{map[string]any{"field": binaryPair(maskName), "value": binaryPair(maskVisible)}},
		})
		if strings.Contains(result, maskName) || strings.Contains(result, maskVisible) || !strings.Contains(result, "[masked]") {
			t.Fatalf("hash result = %s", result)
		}
	})

	t.Run("typed member pairs stay one byte string", func(t *testing.T) {
		members := []map[string]any{{
			"member": binaryPair("prefix " + maskSentinel + " suffix"),
			"score":  1.5,
		}}
		result := emit(t, toolset, "redis_sorted_set", "range", map[string]any{"members": members})
		if strings.Contains(result, maskSentinel) {
			t.Fatalf("typed pair leaked: %s", result)
		}
		decoded := decodeData(t, result)
		member := decoded["members"].([]any)[0].(map[string]any)["member"].(map[string]any)
		assertPair(t, member, "prefix [masked] suffix")
		if decoded["members"].([]any)[0].(map[string]any)["score"] != 1.5 {
			t.Fatalf("unmatched score changed: %s", result)
		}
		broken := emit(t, toolset, "redis_sorted_set", "range", map[string]any{
			"members": []map[string]any{{
				"member": map[string]any{"utf8": maskSentinel, "base64": base64.StdEncoding.EncodeToString([]byte(maskVisible))},
				"score":  1.5,
			}},
		})
		if !strings.Contains(broken, resultMaskedMessage) || strings.Contains(broken, maskSentinel) || strings.Contains(broken, maskVisible) {
			t.Fatalf("inconsistent typed pair = %s", broken)
		}
	})

	t.Run("native number and bool", func(t *testing.T) {
		native := NewToolset(&payloadAdapter{}, false)
		native.SetPolicy(maskPolicy(t, `{"enabled":true,"values":["411111","true"]}`))
		result := emit(t, native, "redis_sorted_set", "score", map[string]any{
			"count":    int64(7),
			"scalar":   false,
			"metadata": map[string]any{"score": 411111.0, "flag": true, "note": maskVisible},
		})
		decoded := decodeData(t, result)
		if decoded["count"] != float64(7) || decoded["scalar"] != false {
			t.Fatalf("unmatched native values = %#v", decoded)
		}
		metadata := decoded["metadata"].(map[string]any)
		if metadata["score"] != nil || metadata["flag"] != nil || metadata["note"] != maskVisible {
			t.Fatalf("native selection = %#v", metadata)
		}
	})

	t.Run("score and stream id", func(t *testing.T) {
		result := emit(t, toolset, "redis_sorted_set", "range", map[string]any{
			"members": []any{map[string]any{"member": binaryPair(maskVisible), "score": 424242.5}},
			"metadata": map[string]any{
				"key":   maskSentinel,
				"id":    "1-0",
				"score": "sk_live_42",
			},
		})
		if strings.Contains(result, maskSentinel) || strings.Contains(result, "424242.5") || strings.Contains(result, "sk_live_42") {
			t.Fatalf("score result = %s", result)
		}
		if !strings.Contains(result, maskVisible) || !strings.Contains(result, "1-0") || !strings.Contains(result, `"score":null`) {
			t.Fatalf("unselected data changed: %s", result)
		}
	})

	t.Run("pending consumers", func(t *testing.T) {
		result := emit(t, toolset, "redis_stream", "pending", map[string]any{
			"items": []redis.XPendingExt{{
				ID:         "1-" + maskSentinel,
				Consumer:   maskName,
				Idle:       time.Second,
				RetryCount: 2,
			}},
		})
		if strings.Contains(result, maskSentinel) || strings.Contains(result, maskName) {
			t.Fatalf("pending leaked: %s", result)
		}
		if !strings.Contains(result, "1000000000") || !strings.Contains(result, `"RetryCount":2`) {
			t.Fatalf("pending numbers = %s", result)
		}
	})

	t.Run("nested json and binary pair", func(t *testing.T) {
		nested := `{"synthetic_field":"ordinary-visible","n":1}`
		raw := append([]byte{0xff}, []byte(maskSentinel)...)
		raw = append(raw, 0xfe)
		result := emit(t, toolset, "redis_string", "get", map[string]any{
			"value": binaryPair(nested),
			"items": []any{binaryPair(string(raw))},
		})
		if strings.Contains(result, maskName) || strings.Contains(result, maskVisible) || strings.Contains(result, maskSentinel) {
			t.Fatalf("nested or binary leaked: %s", result)
		}
		decoded := decodeData(t, result)
		pair := decoded["value"].(map[string]any)
		if pair["utf8"] == nil || pair["base64"] == nil {
			t.Fatalf("pair = %#v", pair)
		}
		decoded64, err := base64.StdEncoding.DecodeString(pair["base64"].(string))
		if err != nil || string(decoded64) != pair["utf8"].(string) {
			t.Fatalf("stale or mismatched pair %#v", pair)
		}
		binary := decoded["items"].([]any)[0].(map[string]any)
		if _, ok := binary["utf8"]; ok {
			t.Fatalf("invalid utf8 gained a utf8 sibling: %#v", binary)
		}
		decoded64, err = base64.StdEncoding.DecodeString(binary["base64"].(string))
		if err != nil || decoded64[0] != 0xff || decoded64[len(decoded64)-1] != 0xfe || bytesContain(decoded64, []byte(maskSentinel)) {
			t.Fatalf("binary bytes = %x", decoded64)
		}
	})

	t.Run("invalid pair and budget", func(t *testing.T) {
		invalid := emit(t, toolset, "redis_string", "get", map[string]any{
			"value": map[string]any{"utf8": maskSentinel, "base64": base64.StdEncoding.EncodeToString([]byte(maskVisible))},
		})
		if !strings.Contains(invalid, resultMaskedMessage) || strings.Contains(invalid, maskSentinel) || strings.Contains(invalid, maskVisible) {
			t.Fatalf("invalid pair = %s", invalid)
		}
		huge := strings.Repeat("a", 1<<20+1) + maskSentinel
		budget := emit(t, toolset, "redis_string", "get", map[string]any{"value": binaryPair(huge)})
		if !strings.Contains(budget, resultMaskedMessage) || strings.Contains(budget, maskSentinel) {
			t.Fatalf("budget leaked or succeeded")
		}
	})

	t.Run("nan and validation", func(t *testing.T) {
		result := emit(t, toolset, "redis_sorted_set", "score", map[string]any{
			"scalar":   math.NaN(),
			"metadata": map[string]any{"key": maskSentinel},
		})
		if !strings.Contains(result, resultMaskedMessage) || strings.Contains(result, maskSentinel) || strings.Contains(result, "NaN") {
			t.Fatalf("nan result = %s", result)
		}
		inf := emit(t, toolset, "redis_sorted_set", "score", map[string]any{"scalar": math.Inf(1)})
		if !strings.Contains(inf, resultMaskedMessage) || strings.Contains(inf, "Inf") {
			t.Fatalf("inf result = %s", inf)
		}
		validation, err := toolByName(t, toolset.GetTools(), "redis_string").Handler(context.Background(), map[string]any{
			"operation": "get",
			"args":      map[string]any{"key": "sample", maskSentinel: true, "by": maskSentinel},
		})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(validation, maskSentinel) || !strings.Contains(validation, safeArgumentMessage) {
			t.Fatalf("validation = %s", validation)
		}
		badOperation, err := toolByName(t, toolset.GetTools(), "redis_string").Handler(context.Background(), map[string]any{
			"operation": maskSentinel,
			"args":      map[string]any{},
		})
		if err != nil || strings.Contains(badOperation, maskSentinel) {
			t.Fatalf("operation echo = %s, err %v", badOperation, err)
		}
	})

	t.Run("db size and null scalar", func(t *testing.T) {
		result := emit(t, toolset, "redis_observe", "db_size", map[string]any{
			"count":    int64(7),
			"metadata": map[string]any{"aggregate": "single_server"},
			"scalar":   nil,
		})
		decoded := decodeData(t, result)
		if decoded["count"] != float64(7) || decoded["scalar"] != nil {
			t.Fatalf("db size = %#v", decoded)
		}
		metadata := decoded["metadata"].(map[string]any)
		if metadata["aggregate"] != "single_server" || metadata["key"] != nil {
			t.Fatalf("db size metadata = %#v", metadata)
		}
	})
}

func TestDisabledPolicyKeepsDumpAndReflectiveValidation(t *testing.T) {
	adapter := &payloadAdapter{data: map[string]any{"value": binaryPair(maskSentinel)}}
	tool := toolByName(t, NewToolset(adapter, false).GetTools(), "redis_key")
	result, err := tool.Handler(context.Background(), map[string]any{"operation": "dump", "args": map[string]any{"key": "sample"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, maskSentinel) || strings.Contains(result, resultMaskedMessage) {
		t.Fatalf("disabled dump = %s", result)
	}
	validation, err := tool.Handler(context.Background(), map[string]any{
		"operation": "dump",
		"args":      map[string]any{"key": "sample", maskSentinel: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(validation, maskSentinel) {
		t.Fatalf("disabled validation hid the argument name: %s", validation)
	}
}

type payloadAdapter struct {
	calls int
	last  Call
	data  map[string]any
}

func (a *payloadAdapter) Call(_ context.Context, call Call) (map[string]any, error) {
	a.calls++
	a.last = call
	return a.data, nil
}

func emit(t *testing.T, toolset *Toolset, toolName, operation string, data map[string]any) string {
	t.Helper()
	adapter, ok := toolset.adapter.(*payloadAdapter)
	if !ok {
		t.Fatal("payload adapter")
	}
	adapter.data = data
	args := fixtureArgs(toolName, operation, contracts[toolName+"/"+operation])
	tool := toolByName(t, toolset.GetTools(), toolName)
	result, err := tool.Handler(context.Background(), map[string]any{"operation": operation, "args": args})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func decodeData(t *testing.T, result string) map[string]any {
	t.Helper()
	var envelope map[string]any
	if err := json.Unmarshal([]byte(result), &envelope); err != nil {
		t.Fatal(err)
	}
	data, _ := envelope["data"].(map[string]any)
	if data == nil {
		t.Fatalf("envelope = %s", result)
	}
	return data
}

func maskPolicy(t *testing.T, raw string) masking.Policy {
	t.Helper()
	policy, err := masking.ParseJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func richMaskedPayload() map[string]any {
	return map[string]any{
		"value": binaryPair(`{"synthetic_field":"synthetic-token-value"}`),
		"count": int64(2),
		"metadata": map[string]any{
			"key":         maskSentinel,
			"next_cursor": "ABCDEFGHIJKLMNOPQRSTUV",
			"id":          "1-0",
		},
		"values_by_key": map[string]any{maskSentinel: nil, "plain": binaryPair(maskVisible)},
		"fields":        []any{map[string]any{"field": binaryPair("color"), "value": binaryPair(maskSentinel)}},
		"members":       []any{map[string]any{"member": binaryPair(maskVisible), "score": 1.5}},
		"items":         map[string]any{"keys": []string{maskSentinel, maskVisible}},
	}
}

func fixtureArgs(tool, operation string, contract operationContract) map[string]any {
	args := map[string]any{}
	names := append(append([]string(nil), contract.required...), contract.optional...)
	for _, name := range names {
		if values, ok := contract.enums[name]; ok && len(values) > 0 && contains(contract.required, name) {
			args[name] = values[0]
		}
	}
	switch tool + "/" + operation {
	case "redis_stream_write/group":
		args["action"] = "destroy"
	case "redis_stream_write/trim":
		args["strategy"] = "maxlen"
	case "redis_sorted_set/range":
		args["by"] = "rank"
	}
	for _, name := range contract.required {
		if _, ok := args[name]; ok {
			continue
		}
		args[name] = fixtureValue(tool, operation, name, contract, args)
	}
	switch tool + "/" + operation {
	case "redis_sorted_set/range":
		args["start"] = float64(0)
		args["count"] = float64(1)
	case "redis_stream_write/add":
		args["maxlen"] = float64(1)
	case "redis_stream_write/trim":
		args["threshold"] = float64(0)
	case "redis_stream_write/claim":
		args["justid"] = true
	}
	return args
}

func fixtureValue(tool, operation, name string, contract operationContract, args map[string]any) any {
	if kind, ok := contract.arrays[name]; ok {
		return fixtureArray(kind)
	}
	if contains(contract.binaryLists, name) {
		return []any{map[string]any{"utf8": "ordinary"}}
	}
	if contains(contract.binary, name) {
		return map[string]any{"utf8": "ordinary"}
	}
	if name == "expiry" {
		return map[string]any{"kind": "ex", "value": float64(1)}
	}
	if isKeyArgument(name) {
		return "sample"
	}
	switch scalarTypeForArgs(tool, operation, name, args) {
	case scalarInteger, scalarNumber, scalarNumberByKind:
		return float64(1)
	case scalarRedisBound:
		return float64(1)
	case scalarBoolean:
		return true
	default:
		return "sample"
	}
}

func fixtureArray(kind string) any {
	switch kind {
	case "number":
		return []any{float64(1)}
	case "binary":
		return []any{map[string]any{"utf8": "ordinary"}}
	case "key_value":
		return []any{map[string]any{"key": "sample", "value": map[string]any{"utf8": "ordinary"}}}
	case "field_value":
		return []any{map[string]any{"field": map[string]any{"utf8": "ordinary"}, "value": map[string]any{"utf8": "ordinary"}}}
	case "score_member":
		return []any{map[string]any{"score": float64(1), "member": map[string]any{"utf8": "ordinary"}}}
	case "stream":
		return []any{map[string]any{"key": "sample", "id": "1-0"}}
	default:
		return []any{"sample"}
	}
}

func toolByName(t *testing.T, tools []toolset.ServerTool, name string) toolset.ServerTool {
	t.Helper()
	for _, tool := range tools {
		if tool.Tool.Name == name {
			return tool
		}
	}
	t.Fatalf("missing tool %s", name)
	return toolset.ServerTool{}
}

func sameArgs(got, want map[string]any) bool {
	gotRaw, gotErr := json.Marshal(got)
	wantRaw, wantErr := json.Marshal(want)
	return gotErr == nil && wantErr == nil && string(gotRaw) == string(wantRaw)
}

func cloneMap(value map[string]any) map[string]any {
	raw, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return value
	}
	return out
}

func bytesContain(haystack, needle []byte) bool {
	return strings.Contains(string(haystack), string(needle))
}

func assertPair(t *testing.T, pair map[string]any, utf8Text string) {
	t.Helper()
	if pair["utf8"] != utf8Text {
		t.Fatalf("utf8 = %#v", pair["utf8"])
	}
	decoded, err := base64.StdEncoding.DecodeString(pair["base64"].(string))
	if err != nil || string(decoded) != utf8Text {
		t.Fatalf("pair siblings disagree: %#v", pair)
	}
}
