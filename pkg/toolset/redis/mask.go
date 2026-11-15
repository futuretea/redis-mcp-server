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
	return maskObject(policy, data, false, &masking.Budget{})
}

func maskAny(policy masking.Policy, value any, selected bool, budget *masking.Budget) (any, error) {
	if selected {
		return redactWhole(policy, value)
	}
	if value == nil {
		return nil, nil
	}
	switch typed := value.(type) {
	case string:
		return maskDataString(policy, typed, budget)
	case bool:
		return maskScalarText(policy, strconv.FormatBool(typed), typed, budget)
	case float64:
		return maskFloat(policy, typed, budget)
	case float32:
		return maskFloat(policy, float64(typed), budget)
	case int:
		return maskScalarText(policy, strconv.Itoa(typed), typed, budget)
	case int8:
		return maskScalarText(policy, strconv.FormatInt(int64(typed), 10), typed, budget)
	case int16:
		return maskScalarText(policy, strconv.FormatInt(int64(typed), 10), typed, budget)
	case int32:
		return maskScalarText(policy, strconv.FormatInt(int64(typed), 10), typed, budget)
	case int64:
		return maskScalarText(policy, strconv.FormatInt(typed, 10), typed, budget)
	case uint:
		return maskScalarText(policy, strconv.FormatUint(uint64(typed), 10), typed, budget)
	case uint8:
		return maskScalarText(policy, strconv.FormatUint(uint64(typed), 10), typed, budget)
	case uint16:
		return maskScalarText(policy, strconv.FormatUint(uint64(typed), 10), typed, budget)
	case uint32:
		return maskScalarText(policy, strconv.FormatUint(uint64(typed), 10), typed, budget)
	case uint64:
		return maskScalarText(policy, strconv.FormatUint(typed, 10), typed, budget)
	case map[string]any:
		return maskObject(policy, typed, false, budget)
	case []any:
		return maskSlice(policy, typed, budget)
	case []string:
		return maskStrings(policy, typed, budget)
	default:
		return maskReflected(policy, value, budget)
	}
}

func maskSlice(policy masking.Policy, values []any, budget *masking.Budget) ([]any, error) {
	if values == nil {
		return nil, nil
	}
	out := make([]any, len(values))
	for index, value := range values {
		masked, err := maskAny(policy, value, false, budget)
		if err != nil {
			return nil, err
		}
		out[index] = masked
	}
	return out, nil
}

func maskStrings(policy masking.Policy, values []string, budget *masking.Budget) ([]string, error) {
	if values == nil {
		return nil, nil
	}
	out := make([]string, len(values))
	for index, value := range values {
		masked, err := maskDataString(policy, value, budget)
		if err != nil {
			return nil, err
		}
		out[index] = masked
	}
	return out, nil
}

func maskObject(policy masking.Policy, obj map[string]any, dynamic bool, budget *masking.Budget) (map[string]any, error) {
	if obj == nil {
		return nil, nil
	}
	if !dynamic {
		raw, binary, err := asBinary(obj)
		if err != nil || binary {
			if err != nil {
				return nil, err
			}
			return maskBinary(policy, raw, budget)
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
				maskedKey, err := maskDataString(policy, key, budget)
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
			masked, err = maskCursor(policy, value, budget)
		case !dynamic && key == "values_by_key":
			masked, err = maskDynamicValue(policy, value, budget)
		case fieldSelected && key == "value":
			masked, err = redactWhole(policy, value)
		default:
			masked, err = maskAny(policy, value, selected, budget)
		}
		if err != nil {
			return nil, err
		}
		out[outKey] = masked
	}
	return out, nil
}

func maskDynamicValue(policy masking.Policy, value any, budget *masking.Budget) (any, error) {
	if value == nil {
		return nil, nil
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return nil, masking.ErrFailed
	}
	return maskObject(policy, obj, true, budget)
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
		if !masking.WithinInput(len(text)) {
			return nil, true, masking.ErrFailed
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

func maskBinary(policy masking.Policy, raw []byte, budget *masking.Budget) (map[string]any, error) {
	if utf8.Valid(raw) && policy.MatchName(string(raw)) {
		return binaryPair(policy.Placeholder()), nil
	}
	masked, err := policy.MaskBytes(raw, budget)
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

func maskCursor(policy masking.Policy, value any, budget *masking.Budget) (any, error) {
	text, ok := value.(string)
	if !ok {
		return nil, masking.ErrFailed
	}
	if policy.MatchName(text) {
		return nil, masking.ErrFailed
	}
	masked, err := policy.MaskText(text, budget)
	if err != nil || masked != text {
		return nil, masking.ErrFailed
	}
	return text, nil
}

func maskDataString(policy masking.Policy, value string, budget *masking.Budget) (string, error) {
	var out string
	if policy.MatchName(value) {
		out = policy.Placeholder()
	} else {
		masked, err := policy.MaskBytes([]byte(value), budget)
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

func maskFloat(policy masking.Policy, value float64, budget *masking.Budget) (any, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return nil, masking.ErrFailed
	}
	encoded, err := jsonMarshalNumber(value)
	if err != nil {
		return nil, masking.ErrFailed
	}
	return maskScalarText(policy, encoded, value, budget)
}

func jsonMarshalNumber(value float64) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func maskScalarText(policy masking.Policy, text string, original any, budget *masking.Budget) (any, error) {
	hit, err := policy.MatchScalar(text, budget)
	if err != nil {
		return nil, err
	}
	if hit {
		return nil, nil
	}
	return original, nil
}

func maskReflected(policy masking.Policy, value any, budget *masking.Budget) (any, error) {
	if value == nil {
		return nil, nil
	}
	return maskReflectValue(policy, reflect.ValueOf(value), budget)
}

func maskReflectValue(policy masking.Policy, value reflect.Value, budget *masking.Budget) (any, error) {
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
		return maskDataString(policy, value.String(), budget)
	case reflect.Bool:
		return maskScalarText(policy, strconv.FormatBool(value.Bool()), value.Bool(), budget)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		number := value.Int()
		return maskScalarText(policy, strconv.FormatInt(number, 10), number, budget)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		number := value.Uint()
		return maskScalarText(policy, strconv.FormatUint(number, 10), number, budget)
	case reflect.Float32, reflect.Float64:
		return maskFloat(policy, value.Float(), budget)
	case reflect.Slice, reflect.Array:
		if value.Kind() == reflect.Slice && value.IsNil() {
			return nil, nil
		}
		if value.Type().Elem().Kind() == reflect.Uint8 {
			return nil, masking.ErrFailed
		}
		items := make([]any, value.Len())
		for index := 0; index < value.Len(); index++ {
			items[index] = value.Index(index).Interface()
		}
		return maskSlice(policy, items, budget)
	case reflect.Struct:
		out := make(map[string]any, value.NumField())
		for index := 0; index < value.NumField(); index++ {
			field := value.Type().Field(index)
			name, ok := exportedJSONName(field)
			if !ok {
				continue
			}
			masked, err := maskReflectValue(policy, value.Field(index), budget)
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
		if value.IsNil() {
			return nil, nil
		}
		if value.Type().Key().Kind() != reflect.String {
			return nil, masking.ErrFailed
		}
		// Keep original values for one maskObject pass. Masking utf8 and base64
		// separately makes the siblings disagree.
		obj := make(map[string]any, value.Len())
		for _, key := range value.MapKeys() {
			obj[key.String()] = value.MapIndex(key).Interface()
		}
		return maskObject(policy, obj, false, budget)
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
