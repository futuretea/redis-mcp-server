package cmd_test

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/futuretea/redis-mcp-server/internal/cmd"
)

func TestRootHelpListsSubcommands(t *testing.T) {
	var out bytes.Buffer
	root := cmd.NewRootCommand(cmd.IOStreams{Out: &out, ErrOut: &out})
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("help: %v", err)
	}
	text := out.String()
	for _, want := range []string{"mcp", "tools", "version", "completion"} {
		if !strings.Contains(text, want) {
			t.Fatalf("help missing %q:\n%s", want, text)
		}
	}
}

func TestRootWithoutArgsShowsHelp(t *testing.T) {
	var out bytes.Buffer
	root := cmd.NewRootCommand(cmd.IOStreams{Out: &out, ErrOut: &out})
	root.SetArgs([]string{})
	if err := root.Execute(); err != nil {
		t.Fatalf("root with no args: %v", err)
	}
	text := out.String()
	if !strings.Contains(text, "Available Commands") {
		t.Fatalf("expected help output, got:\n%s", text)
	}
	if strings.Contains(text, "registered MCP tools") || strings.Contains(text, "starting HTTP MCP server") {
		t.Fatalf("root must not start the MCP server:\n%s", text)
	}
}

func TestToolsListAndCall(t *testing.T) {
	configureRedis(t)
	var out bytes.Buffer
	root := cmd.NewRootCommand(cmd.IOStreams{Out: &out, ErrOut: &out})
	root.SetArgs([]string{"tools", "list"})
	if err := root.Execute(); err != nil {
		t.Fatalf("tools list: %v", err)
	}
	if !strings.Contains(out.String(), "redis_string") || strings.Contains(out.String(), "example") {
		t.Fatalf("unexpected Redis tool list:\n%s", out.String())
	}

	out.Reset()
	root = cmd.NewRootCommand(cmd.IOStreams{Out: &out, ErrOut: &out})
	root.SetArgs([]string{"tools", "call", "redis_string", "--params", `{"operation":"get","args":{"key":"sample"}}`})
	if err := root.Execute(); err != nil {
		t.Fatalf("tools call: %v", err)
	}
	if !strings.Contains(out.String(), `"category":"unavailable"`) {
		t.Fatalf("Redis call result = %q", out.String())
	}
}

func TestVersionCommand(t *testing.T) {
	var out bytes.Buffer
	root := cmd.NewRootCommand(cmd.IOStreams{Out: &out, ErrOut: &out})
	root.SetArgs([]string{"version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("version: %v", err)
	}
	if !strings.Contains(out.String(), "redis-mcp-server") {
		t.Fatalf("unexpected version output: %q", out.String())
	}
}

func TestToolsUnknownTool(t *testing.T) {
	configureRedis(t)
	var out bytes.Buffer
	root := cmd.NewRootCommand(cmd.IOStreams{Out: &out, ErrOut: &out})
	root.SetArgs([]string{"tools", "describe", "missing-tool"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("expected unknown tool error, got %v", err)
	}

	root = cmd.NewRootCommand(cmd.IOStreams{Out: &out, ErrOut: &out})
	root.SetArgs([]string{"tools", "call", "missing-tool", "--params", "{}"})
	err = root.Execute()
	if err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("expected unknown tool error on call, got %v", err)
	}
}

func TestToolsRejectsNullParams(t *testing.T) {
	configureRedis(t)
	var out bytes.Buffer
	root := cmd.NewRootCommand(cmd.IOStreams{Out: &out, ErrOut: &out})
	root.SetArgs([]string{"tools", "call", "redis_key", "--params", "null"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "expected an object") {
		t.Fatalf("expected JSON object error, got %v", err)
	}
}

func TestCommandsReturnOutputErrors(t *testing.T) {
	writeErr := errors.New("write failed")
	tests := []struct {
		name string
		args []string
	}{
		{name: "completion script", args: []string{"completion", "bash"}},
		{name: "completion install", args: []string{"completion", "install"}},
		{name: "version", args: []string{"version"}},
		{name: "tools list", args: []string{"tools", "list"}},
		{name: "tools describe", args: []string{"tools", "describe", "redis_string"}},
		{name: "tools call", args: []string{"tools", "call", "redis_string", "--params", `{"operation":"get","args":{"key":"sample"}}`}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configureRedis(t)
			root := cmd.NewRootCommand(cmd.IOStreams{
				In:     strings.NewReader(""),
				Out:    errorWriter{err: writeErr},
				ErrOut: io.Discard,
			})
			root.SetArgs(tt.args)
			if err := root.Execute(); !errors.Is(err, writeErr) {
				t.Fatalf("Execute() error = %v, want %v", err, writeErr)
			}
		})
	}
}

func configureRedis(t *testing.T) {
	t.Helper()
	t.Setenv("MCP_REDIS_TOPOLOGY", "standalone")
	t.Setenv("MCP_REDIS_ADDRESSES", "127.0.0.1:6379")
}

type errorWriter struct {
	err error
}

func (w errorWriter) Write([]byte) (int, error) {
	return 0, w.err
}
