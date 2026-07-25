package mcp_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/futuretea/redis-mcp-server/pkg/core/config"
	mcpserver "github.com/futuretea/redis-mcp-server/pkg/server/mcp"
	"github.com/futuretea/redis-mcp-server/pkg/toolset"
	redistoolset "github.com/futuretea/redis-mcp-server/pkg/toolset/redis"
)

func TestNewServerRegistersRedisTools(t *testing.T) {
	server, err := mcpserver.NewServer(mcpserver.Configuration{
		StaticConfig: &config.StaticConfig{LogLevel: "info"},
		Toolsets:     []toolset.Toolset{redistoolset.NewToolset(nil, false)},
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer server.Close()

	if !server.IsHealthy() {
		t.Fatal("expected healthy server")
	}
	tools := server.GetEnabledTools()
	if len(tools) != 8 {
		t.Fatalf("expected 8 tools, got %v", tools)
	}
	names := map[string]bool{}
	for _, name := range tools {
		names[name] = true
	}
	if !names["redis_key"] || !names["redis_string"] {
		t.Fatalf("expected Redis tools, got %v", tools)
	}
}

func TestNewServerRequiresStaticConfig(t *testing.T) {
	_, err := mcpserver.NewServer(mcpserver.Configuration{})
	if err == nil || !strings.Contains(err.Error(), "static config is required") {
		t.Fatalf("expected static config error, got %v", err)
	}
}

func TestNewServerRejectsEmptyToolFilter(t *testing.T) {
	_, err := mcpserver.NewServer(mcpserver.Configuration{
		StaticConfig: &config.StaticConfig{
			LogLevel:     "info",
			EnabledTools: []string{"missing"},
		},
		Toolsets: []toolset.Toolset{redistoolset.NewToolset(nil, false)},
	})
	if err == nil || !strings.Contains(err.Error(), "no tools registered") {
		t.Fatalf("expected no tools registered error, got %v", err)
	}
}

func TestNewServerRejectsDuplicateToolNames(t *testing.T) {
	_, err := mcpserver.NewServer(mcpserver.Configuration{
		StaticConfig: &config.StaticConfig{LogLevel: "info"},
		Toolsets: []toolset.Toolset{
			redistoolset.NewToolset(nil, false),
			redistoolset.NewToolset(nil, false),
		},
	})
	if err == nil || !strings.Contains(err.Error(), `duplicate tool name "redis_key"`) {
		t.Fatalf("expected duplicate tool name error, got %v", err)
	}
}

func TestNewTextResult(t *testing.T) {
	ok := mcpserver.NewTextResult("hello", nil)
	if ok.IsError || len(ok.Content) != 1 {
		t.Fatalf("unexpected ok result: %+v", ok)
	}

	fail := mcpserver.NewTextResult("", errString("boom"))
	if !fail.IsError {
		t.Fatal("expected error result")
	}
}

func TestServerClosesOwnedResourceOnce(t *testing.T) {
	closer := &recordingCloser{}
	server, err := mcpserver.NewServer(mcpserver.Configuration{
		StaticConfig: &config.StaticConfig{LogLevel: "info"},
		Toolsets:     []toolset.Toolset{redistoolset.NewToolset(nil, false)},
		Closer:       closer,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	server.Close()
	server.Close()
	if closer.calls != 1 {
		t.Fatalf("Close calls = %d, want 1", closer.calls)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

type recordingCloser struct{ calls int }

func (c *recordingCloser) Close() error {
	c.calls++
	return errors.New("fixture close error")
}
