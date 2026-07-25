package redis

import "testing"

func TestStreamKeyShapeRequiresNonEmptyKey(t *testing.T) {
	if err := validateArray("streams", "stream", []any{map[string]any{"key": "", "id": "0-0"}}); err == nil {
		t.Fatal("empty streams[].key was accepted")
	}
	item, _ := arraySchema("stream")["items"].(map[string]any)
	properties, _ := item["properties"].(map[string]any)
	key, _ := properties["key"].(map[string]any)
	if got, ok := key["minLength"].(int); !ok || got != 1 {
		t.Fatalf("streams[].key minLength = %#v, want 1", key["minLength"])
	}
}
