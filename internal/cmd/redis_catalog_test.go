package cmd

import (
	"sort"
	"testing"

	"github.com/futuretea/redis-mcp-server/pkg/core/config"
)

func TestBuildToolCatalogUsesRedisReadOnlyCatalogByDefault(t *testing.T) {
	tools, err := buildToolCatalog(&config.StaticConfig{})
	if err != nil {
		t.Fatalf("buildToolCatalog: %v", err)
	}

	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Tool.Name)
	}
	sort.Strings(names)
	want := []string{"redis_hash", "redis_key", "redis_list", "redis_observe", "redis_set", "redis_sorted_set", "redis_stream", "redis_string"}
	if len(names) != len(want) {
		t.Fatalf("tool names = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("tool names = %v, want %v", names, want)
		}
	}
}
