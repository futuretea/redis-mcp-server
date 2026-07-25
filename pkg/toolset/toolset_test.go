package toolset_test

import (
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/futuretea/redis-mcp-server/pkg/toolset"
)

func TestFilterTools(t *testing.T) {
	tools := []toolset.ServerTool{
		{Tool: mcp.NewTool("alpha"), Domain: "test"},
		{Tool: mcp.NewTool("beta"), Domain: "test"},
		{Tool: mcp.NewTool("other"), Domain: "other"},
	}

	tests := []struct {
		name string
		opts toolset.FilterOptions
		want []string
	}{
		{
			name: "enabled domain and disabled tool",
			opts: toolset.FilterOptions{
				EnabledDomains: []string{"test"},
				DisabledTools:  []string{"beta"},
			},
			want: []string{"alpha"},
		},
		{
			name: "enabled tools only",
			opts: toolset.FilterOptions{EnabledTools: []string{"beta"}},
			want: []string{"beta"},
		},
		{
			name: "disabled domain",
			opts: toolset.FilterOptions{DisabledDomains: []string{"test"}},
			want: []string{"other"},
		},
		{
			name: "filters everything",
			opts: toolset.FilterOptions{EnabledTools: []string{"missing"}},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filtered := toolset.FilterTools(tools, tt.opts)
			if len(filtered) != len(tt.want) {
				t.Fatalf("got %d tools, want %d (%+v)", len(filtered), len(tt.want), filtered)
			}
			for i, name := range tt.want {
				if filtered[i].Tool.Name != name {
					t.Fatalf("tool[%d]=%q, want %q", i, filtered[i].Tool.Name, name)
				}
			}
		})
	}
}
