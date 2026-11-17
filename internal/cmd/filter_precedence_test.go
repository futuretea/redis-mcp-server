package cmd_test

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/futuretea/redis-mcp-server/internal/cmd"
)

func TestToolFilterBlankValuesDoNotCollapse(t *testing.T) {
	base := "redis:\n  topology: standalone\n  addresses: [\"127.0.0.1:6379\"]\n"
	tests := []struct {
		name    string
		yaml    string
		env     map[string]string
		args    []string
		wantErr string
		present []string
		absent  []string
	}{
		{
			name:    "space env denylist reexposes yaml",
			yaml:    base + "disabled_tools: [\"redis_string\"]\n",
			env:     map[string]string{"MCP_DISABLED_TOOLS": " "},
			wantErr: "disabled_tools contains no names",
		},
		{
			name:    "comma env denylist reexposes yaml",
			yaml:    base + "disabled_tools: [\"redis_string\"]\n",
			env:     map[string]string{"MCP_DISABLED_TOOLS": ","},
			wantErr: "disabled_tools contains no names",
		},
		{
			name:    "comma space env denylist reexposes yaml",
			yaml:    base + "disabled_tools: [\"redis_string\"]\n",
			env:     map[string]string{"MCP_DISABLED_TOOLS": " , "},
			wantErr: "disabled_tools contains no names",
		},
		{
			name:    "space flag denylist reexposes yaml",
			yaml:    base + "disabled_tools: [\"redis_string\"]\n",
			args:    []string{"--disabled-tools", " "},
			wantErr: "disabled_tools contains no names",
		},
		{
			name:    "comma flag denylist reexposes yaml",
			yaml:    base + "disabled_tools: [\"redis_string\"]\n",
			args:    []string{"--disabled-tools", ","},
			wantErr: "disabled_tools contains no names",
		},
		{
			name:    "space env allowlist widens yaml",
			yaml:    base + "enabled_tools: [\"redis_string\"]\n",
			env:     map[string]string{"MCP_ENABLED_TOOLS": " "},
			wantErr: "enabled_tools contains no names",
		},
		{
			name:    "space flag allowlist widens yaml",
			yaml:    base + "enabled_tools: [\"redis_string\"]\n",
			args:    []string{"--enabled-tools", " "},
			wantErr: "enabled_tools contains no names",
		},
		{
			name:    "space env disabled domain reexposes tools",
			yaml:    base + "disabled_domains: [\"redis\"]\n",
			env:     map[string]string{"MCP_DISABLED_DOMAINS": " "},
			wantErr: "disabled_domains contains no names",
		},
		{
			name:    "space flag disabled domain reexposes tools",
			yaml:    base + "disabled_domains: [\"redis\"]\n",
			args:    []string{"--disable-domains", " "},
			wantErr: "disabled_domains contains no names",
		},
		{
			name:    "space env enabled domain reexposes tools",
			yaml:    base + "enabled_domains: [\"other\"]\n",
			env:     map[string]string{"MCP_ENABLED_DOMAINS": " "},
			wantErr: "enabled_domains contains no names",
		},
		{
			name:    "space flag enabled domain reexposes tools",
			yaml:    base + "enabled_domains: [\"other\"]\n",
			args:    []string{"--enable-domains", " "},
			wantErr: "enabled_domains contains no names",
		},
		{
			name:    "yaml blank denylist",
			yaml:    base + "disabled_tools: [\" \", \"\"]\n",
			wantErr: "disabled_tools contains no names",
		},
		{
			name:    "yaml blank allowlist",
			yaml:    base + "enabled_tools: [\" \", \"\"]\n",
			wantErr: "enabled_tools contains no names",
		},
		{
			name:    "yaml blank disabled domain",
			yaml:    base + "disabled_domains: [\" \"]\n",
			wantErr: "disabled_domains contains no names",
		},
		{
			name:    "yaml blank enabled domain",
			yaml:    base + "enabled_domains: [\" \"]\n",
			wantErr: "enabled_domains contains no names",
		},
		{
			name:    "empty env preserves yaml denylist",
			yaml:    base + "disabled_tools: [\"redis_string\"]\n",
			env:     map[string]string{"MCP_DISABLED_TOOLS": ""},
			absent:  []string{"redis_string"},
			present: []string{"redis_hash"},
		},
		{
			name:    "empty env preserves yaml allowlist",
			yaml:    base + "enabled_tools: [\"redis_string\"]\n",
			env:     map[string]string{"MCP_ENABLED_TOOLS": ""},
			present: []string{"redis_string"},
			absent:  []string{"redis_hash"},
		},
		{
			name:   "empty env preserves disabled domain",
			yaml:   base + "disabled_domains: [\"redis\"]\n",
			env:    map[string]string{"MCP_DISABLED_DOMAINS": ""},
			absent: []string{"redis_string", "redis_hash"},
		},
		{
			name:    "omitted flag preserves yaml denylist",
			yaml:    base + "disabled_tools: [\"redis_string\"]\n",
			absent:  []string{"redis_string"},
			present: []string{"redis_hash"},
		},
		{
			name:    "explicit empty denylist flag clears yaml",
			yaml:    base + "disabled_tools: [\"redis_string\"]\n",
			args:    []string{"--disabled-tools="},
			present: []string{"redis_string", "redis_hash"},
		},
		{
			name:    "explicit empty allowlist flag clears yaml",
			yaml:    base + "enabled_tools: [\"redis_string\"]\n",
			args:    []string{"--enabled-tools="},
			present: []string{"redis_string", "redis_hash"},
		},
		{
			name:    "explicit empty disabled domain flag clears yaml",
			yaml:    base + "disabled_domains: [\"redis\"]\n",
			args:    []string{"--disable-domains="},
			present: []string{"redis_string"},
		},
		{
			name:    "explicit empty enabled domain flag clears yaml",
			yaml:    base + "enabled_domains: [\"other\"]\n",
			args:    []string{"--enable-domains="},
			present: []string{"redis_string"},
		},
		{
			name:    "real empty lists stay unfiltered",
			yaml:    base + "enabled_tools: []\ndisabled_tools: []\nenabled_domains: []\ndisabled_domains: []\n",
			present: []string{"redis_string", "redis_hash"},
		},
		{
			name:    "mixed env keeps named tool",
			yaml:    base + "disabled_tools: [\"redis_string\"]\n",
			env:     map[string]string{"MCP_DISABLED_TOOLS": "redis_hash, "},
			absent:  []string{"redis_hash"},
			present: []string{"redis_string"},
		},
		{
			name:    "mixed yaml keeps named tool",
			yaml:    base + "enabled_tools: [\"redis_string\", \" \"]\ndisabled_tools: [\"redis_hash\", \"\"]\n",
			present: []string{"redis_string"},
			absent:  []string{"redis_hash"},
		},
		{
			name:    "unknown denylist name stays legal",
			yaml:    base,
			env:     map[string]string{"MCP_DISABLED_TOOLS": "not_a_tool"},
			present: []string{"redis_string", "redis_hash"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateFilterEnv(t)
			for key, value := range tt.env {
				t.Setenv(key, value)
			}
			path := filepath.Join(t.TempDir(), "redis.yaml")
			if err := os.WriteFile(path, []byte(tt.yaml), 0o600); err != nil {
				t.Fatal(err)
			}
			out, err := runToolsList(t, path, tt.args...)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) || strings.TrimSpace(out) != "" {
					t.Fatalf("tools list error = %v, want %q\n%s", err, tt.wantErr, out)
				}
				return
			}
			if err != nil {
				t.Fatalf("tools list: %v", err)
			}
			names := listedToolNames(t, out)
			for _, name := range tt.present {
				if !containsName(names, name) {
					t.Fatalf("tools = %v, missing %s", names, name)
				}
			}
			for _, name := range tt.absent {
				if containsName(names, name) {
					t.Fatalf("tools = %v, unexpectedly contains %s", names, name)
				}
			}
		})
	}
}

func isolateFilterEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"MCP_ENABLED_TOOLS",
		"MCP_DISABLED_TOOLS",
		"MCP_ENABLED_DOMAINS",
		"MCP_DISABLED_DOMAINS",
		"MCP_REDIS_TOPOLOGY",
		"MCP_REDIS_ADDRESSES",
		"MCP_REDIS_WRITE_ENABLED",
		"MCP_REDIS_USERNAME",
		"MCP_REDIS_TLS",
		"MCP_REDIS_SENTINEL_MASTER",
		"MCP_PORT",
		"MCP_LISTEN",
		"MCP_LOG_LEVEL",
		"MCP_SSE_BASE_URL",
		"MCP_MASKING_JSON",
		"MCP_ALLOW_UNSAFE_DATABASE_ROLE",
	} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
}

func runToolsList(t *testing.T, configPath string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	root := cmd.NewRootCommand(cmd.IOStreams{Out: &out, ErrOut: io.Discard})
	root.SetArgs(append([]string{"tools", "list", "--json", "--config", configPath}, args...))
	err := root.Execute()
	return out.String(), err
}

func listedToolNames(t *testing.T, out string) []string {
	t.Helper()
	var tools []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal([]byte(out), &tools); err != nil {
		t.Fatalf("parse tool list: %v\n%s", err, out)
	}
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	return names
}

func containsName(names []string, name string) bool {
	for _, candidate := range names {
		if candidate == name {
			return true
		}
	}
	return false
}
