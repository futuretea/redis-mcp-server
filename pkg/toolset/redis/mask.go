package redis

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/futuretea/redis-mcp-server/pkg/core/masking"
)

const resultMaskedMessage = "[redis_masking_failed] result was not returned"

const safeArgumentMessage = "Redis rejected an operation argument"

func (t *Toolset) SetPolicy(policy masking.Policy) {
	if t == nil {
		return
	}
	t.policy = policy
}

func (t *Toolset) failure(spec toolSpec, operation, category, message string) string {
	return errorEnvelope(t.publicOperation(spec, operation), category, t.safeMessage(category, message))
}

func (t *Toolset) publicOperation(spec toolSpec, operation string) string {
	if !t.policy.Enabled() || contains(spec.operations, operation) {
		return operation
	}
	return ""
}

func (t *Toolset) safeMessage(category, message string) string {
	if !t.policy.Enabled() || message == resultMaskedMessage || fixedPublicMessage(message) || !strings.Contains(message, `"`) {
		return message
	}
	if category == "invalid_argument" {
		return safeArgumentMessage
	}
	return resultMaskedMessage
}

func fixedPublicMessage(message string) bool {
	switch message {
	case "Redis adapter is not configured",
		"Redis value was not found",
		"Redis write deadline exceeded",
		"Redis operation deadline exceeded",
		"Redis value has an incompatible type",
		"Redis denied this operation",
		"Redis keys do not share a hash slot",
		safeArgumentMessage,
		"Redis operation failed",
		"Redis operation mapping is unavailable",
		"Redis write condition was not met",
		"cursor does not match this scan",
		"scan cursor expired or was evicted",
		"scan continuation state cannot be admitted",
		"scan continuation state is unavailable",
		"cluster primary set changed during scan continuation",
		"cluster scan state used with a non-cluster client",
		"cluster COPY only supports destination_db 0",
		"GETEX supports relative expiry or persist",
		"expiry kind is not supported for hash fields",
		"unsupported hash GETEX expiry",
		"unsupported hash SETEX expiry",
		"expiry kind is not supported for key expiry",
		"return_previous is unavailable for non-equal conditional SET",
		"SET expiry kind is unavailable",
		"digest comparison must be a 16-character lowercase hexadecimal digest",
		"digest comparison must be non-zero",
		"score bound must be a number or Redis bound string",
		"lex bound must be a string",
		resultMaskedMessage:
		return true
	default:
		return false
	}
}

func maskResult(policy masking.Policy, data map[string]any) (map[string]any, error) {
	if data == nil {
		return nil, nil
	}
	masked, err := maskObject(policy, data, false)
	if err != nil {
		return nil, err
	}
	return masked, nil
}

func maskAny(policy masking.Policy, value any, selected bool) (any, error) {
	if selected {
		return redactWhole(policy, value)
	}
	if value == nil {
		return nil, nil
	}
	switch typed := value.(type) {
	case string:
		return maskDataString(policy, typed)
	case bool:
		return maskScalarText(policy, strconv.FormatBool(typed), typed)
	case float64:
		return maskFloat(policy, typed)
	case float32:
		return maskFloat(policy, float64(typed))
	case int:
		return maskScalarText(policy, strconv.Itoa(typed), typed)
	case int8:
		return maskScalarText(policy, strconv.FormatInt(int64(typed), 10), typed)
	case int16:
		return maskScalarText(policy, strconv.FormatInt(int64(typed), 10), typed)
	case int32:
		return maskScalarText(policy, strconv.FormatInt(int64(typed), 10), typed)
	case int64:
		return maskScalarText(policy, strconv.FormatInt(typed, 10), typed)
	case uint:
		return maskScalarText(policy, strconv.FormatUint(uint64(typed), 10), typed)
	case uint8:
		return maskScalarText(policy, strconv.FormatUint(uint64(typed), 10), typed)
	case uint16:
		return maskScalarText(policy, strconv.FormatUint(uint64(typed), 10), typed)
	case uint32:
		return maskScalarText(policy, strconv.FormatUint(uint64(typed), 10), typed)
	case uint64:
		return maskScalarText(policy, strconv.FormatUint(typed, 10), typed)
	case map[string]any:
		return maskObject(policy, typed, false)
	case []any:
		return maskSlice(policy, typed)
	case []string:
		return maskStrings(policy, typed)
	default:
		return maskReflected(policy, value)
	}
}

func maskSlice(policy masking.Policy, values []any) ([]any, error) {
	out := make([]any, len(values))
	for index, value := range values {
		masked, err := maskAny(policy, value, false)
		if err != nil {
			return nil, err
		}
		out[index] = masked
	}
	return out, nil
}

func maskStrings(policy masking.Policy, values []string) ([]string, error) {
	out := make([]string, len(values))
	for index, value := range values {
		masked, err := maskDataString(policy, value)
		if err != nil {
			return nil, err
		}
		out[index] = masked
	}
	return out, nil
}

func maskObject(policy masking.Policy, obj map[string]any, dynamic bool) (map[string]any, error) {
	if obj == nil {
		return nil, nil
	}
	if !dynamic {
		raw, binary, err := asBinary(obj)
		if err != nil || binary {
			if err != nil {
				return nil, err
			}
			return maskBinary(policy, raw)
		}
	}
	fieldSelected := false
	if !dynamic {
		selected, err := fieldNameSelected(policy, obj)
		if err != nil {
			return nil, err
		}
		fieldSelected = selected
	}
	out := make(map[string]any, len(obj))
	seen := make(map[string]struct{}, len(obj))
	for key, value := range obj {
		outKey := key
		selected := false
		if dynamic {
			if policy.MatchName(key) {
				outKey = policy.Placeholder()
				selected = true
			} else {
				maskedKey, err := maskDataString(policy, key)
				if err != nil {
					return nil, err
				}
				outKey = maskedKey
			}
			if _, exists := seen[outKey]; exists {
				return nil, masking.ErrFailed
			}
			seen[outKey] = struct{}{}
		}
		var masked any
		var err error
		switch {
		case !dynamic && key == "next_cursor":
			masked, err = maskCursor(policy, value)
		case !dynamic && key == "values_by_key":
			masked, err = maskDynamicValue(policy, value)
		case fieldSelected && key == "value":
			masked, err = redactWhole(policy, value)
		default:
			masked, err = maskAny(policy, value, selected)
		}
		if err != nil {
			return nil, err
		}
		out[outKey] = masked
	}
	return out, nil
}

func maskDynamicValue(policy masking.Policy, value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return nil, masking.ErrFailed
	}
	return maskObject(policy, obj, true)
}

func fieldNameSelected(policy masking.Policy, obj map[string]any) (bool, error) {
	field, ok := obj["field"]
	if !ok {
		return false, nil
	}
	raw, binary, err := asBinary(field)
	if err != nil || !binary || !utf8.Valid(raw) {
		return false, err
	}
	return policy.MatchName(string(raw)), nil
}

func asBinary(value any) ([]byte, bool, error) {
	obj, ok := value.(map[string]any)
	if !ok || len(obj) == 0 || len(obj) > 2 {
		return nil, false, nil
	}
	for key := range obj {
		if key != "utf8" && key != "base64" {
			return nil, false, nil
		}
	}
	var raw []byte
	sawBase64 := false
	if encoded, ok := obj["base64"]; ok {
		text, ok := encoded.(string)
		if !ok {
			return nil, false, nil
		}
		decoded, err := base64.StdEncoding.DecodeString(text)
		if err != nil {
			return nil, true, masking.ErrFailed
		}
		raw = decoded
		sawBase64 = true
	}
	if textValue, ok := obj["utf8"]; ok {
		text, ok := textValue.(string)
		if !ok {
			return nil, false, nil
		}
		if sawBase64 && string(raw) != text {
			return nil, true, masking.ErrFailed
		}
		if !sawBase64 {
			raw = []byte(text)
		}
	}
	return raw, true, nil
}

func maskBinary(policy masking.Policy, raw []byte) (map[string]any, error) {
	if utf8.Valid(raw) && policy.MatchName(string(raw)) {
		return binaryPair(policy.Placeholder()), nil
	}
	masked, err := policy.MaskBytes(raw)
	if err != nil {
		return nil, err
	}
	return binaryPair(string(masked)), nil
}

func binaryPair(value string) map[string]any {
	result := map[string]any{"base64": base64.StdEncoding.EncodeToString([]byte(value))}
	if utf8.ValidString(value) {
		result["utf8"] = value
	}
	return result
}

func redactWhole(policy masking.Policy, value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	if _, ok := value.(string); ok {
		return policy.Placeholder(), nil
	}
	if _, binary, err := asBinary(value); binary || err != nil {
		if err != nil {
			return nil, err
		}
		return binaryPair(policy.Placeholder()), nil
	}
	return nil, nil
}

func maskCursor(policy masking.Policy, value any) (any, error) {
	text, ok := value.(string)
	if !ok {
		return nil, masking.ErrFailed
	}
	if policy.MatchName(text) {
		return nil, masking.ErrFailed
	}
	masked, err := policy.MaskText(text)
	if err != nil || masked != text {
		return nil, masking.ErrFailed
	}
	return text, nil
}

func maskDataString(policy masking.Policy, value string) (string, error) {
	var out string
	if policy.MatchName(value) {
		out = policy.Placeholder()
	} else {
		masked, err := policy.MaskBytes([]byte(value))
		if err != nil {
			return "", err
		}
		out = string(masked)
	}
	if !utf8.ValidString(out) {
		return "", masking.ErrFailed
	}
	return out, nil
}

func maskFloat(policy masking.Policy, value float64) (any, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return nil, masking.ErrFailed
	}
	encoded, err := jsonMarshalNumber(value)
	if err != nil {
		return nil, masking.ErrFailed
	}
	return maskScalarText(policy, encoded, value)
}

func jsonMarshalNumber(value float64) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func maskScalarText(policy masking.Policy, text string, original any) (any, error) {
	masked, err := policy.MaskText(text)
	if err != nil {
		return nil, err
	}
	if masked != text {
		return nil, nil
	}
	return original, nil
}

func maskReflected(policy masking.Policy, value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	return maskReflectValue(policy, reflect.ValueOf(value))
}

func maskReflectValue(policy masking.Policy, value reflect.Value) (any, error) {
	if !value.IsValid() {
		return nil, nil
	}
	for value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil, nil
		}
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.String:
		return maskDataString(policy, value.String())
	case reflect.Bool:
		return maskScalarText(policy, strconv.FormatBool(value.Bool()), value.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		number := value.Int()
		return maskScalarText(policy, strconv.FormatInt(number, 10), number)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		number := value.Uint()
		return maskScalarText(policy, strconv.FormatUint(number, 10), number)
	case reflect.Float32, reflect.Float64:
		return maskFloat(policy, value.Float())
	case reflect.Slice, reflect.Array:
		if value.Type().Elem().Kind() == reflect.Uint8 {
			return nil, masking.ErrFailed
		}
		items := make([]any, value.Len())
		for index := 0; index < value.Len(); index++ {
			items[index] = value.Index(index).Interface()
		}
		return maskSlice(policy, items)
	case reflect.Struct:
		out := make(map[string]any, value.NumField())
		for index := 0; index < value.NumField(); index++ {
			field := value.Type().Field(index)
			name, ok := exportedJSONName(field)
			if !ok {
				continue
			}
			masked, err := maskReflectValue(policy, value.Field(index))
			if err != nil {
				return nil, err
			}
			if _, exists := out[name]; exists {
				return nil, masking.ErrFailed
			}
			out[name] = masked
		}
		return out, nil
	case reflect.Map:
		if value.Type().Key().Kind() != reflect.String {
			return nil, masking.ErrFailed
		}
		// Keep original values for one maskObject pass. Masking utf8 and base64
		// separately makes the siblings disagree.
		obj := make(map[string]any, value.Len())
		for _, key := range value.MapKeys() {
			obj[key.String()] = value.MapIndex(key).Interface()
		}
		return maskObject(policy, obj, false)
	default:
		return nil, masking.ErrFailed
	}
}

func exportedJSONName(field reflect.StructField) (string, bool) {
	if field.PkgPath != "" {
		return "", false
	}
	tag := field.Tag.Get("json")
	if tag == "-" {
		return "", false
	}
	if tag == "" {
		return field.Name, true
	}
	name, _, _ := strings.Cut(tag, ",")
	if name == "" {
		return field.Name, true
	}
	return name, true
}
