package redis

import "testing"

func TestValidateVariantsPreservesOperationDomainErrorsAndOrder(t *testing.T) {
	testCases := []struct {
		name      string
		tool      string
		operation string
		args      map[string]any
		want      string
	}{
		{"list range", "redis_list", "range", map[string]any{"start": float64(-1)}, "argument start must be a non-negative integer for list range"},
		{"stream pending", "redis_stream", "pending", map[string]any{"start": float64(0)}, "argument start must be a stream ID string"},
		{"conditional set order", "redis_string_write", "set", map[string]any{"condition": "ifne", "return_previous": true}, "argument return_previous is unavailable for condition \"ifne\""},
		{"sorted add", "redis_sorted_set_write", "add", map[string]any{"increment": true, "items": []any{}}, "argument items must contain exactly one item when increment is true"},
		{"stream trim", "redis_stream_write", "trim", map[string]any{"strategy": "maxlen", "threshold": float64(-1)}, "argument threshold must be a non-negative integer for maxlen"},
		{"stream add", "redis_stream_write", "add", map[string]any{}, "exactly one of maxlen or minid is required for stream add"},
		{"sorted range", "redis_sorted_set", "range", map[string]any{"by": "rank", "start": float64(-1)}, "argument start must be a non-negative integer for rank range"},
		{"key expiry", "redis_key_write", "set_expiry", map[string]any{"condition": "nx", "expiry": map[string]any{"kind": "px"}}, "expiry conditions are only supported with ex"},
		{"stream group", "redis_stream_write", "group", map[string]any{"action": "create"}, "argument \"id\" is required for group action \"create\""},
		{"stream claim", "redis_stream_write", "claim", map[string]any{"justid": false}, "argument justid must be true for stream claim"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := validateVariants(testCase.tool, testCase.operation, testCase.args)
			if err == nil || err.Error() != testCase.want {
				t.Fatalf("validateVariants() error = %v, want %q", err, testCase.want)
			}
		})
	}
}
