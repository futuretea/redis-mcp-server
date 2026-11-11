//go:build e2e

package e2e_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/redis/go-redis/v9"
)

const (
	e2eDockerNetworkEnv = "REDIS_MCP_E2E_DOCKER_NETWORK"
	e2eRedisAddressEnv  = "REDIS_MCP_E2E_REDIS_ADDRESS"
	e2eServerBinaryEnv  = "REDIS_MCP_E2E_SERVER_BINARY"
	e2eKey              = "redis-mcp-server-e2e"
	e2eValue            = "MCP end-to-end value"
)

type redisTarget struct {
	name           string
	addresses      []string
	sentinelMaster string
}

func TestMCPServerEndToEnd(t *testing.T) {
	serverBinary := buildServerBinary(t)
	for _, target := range redisTargets(t) {
		t.Run(target.name, func(t *testing.T) {
			seedRedis(t, target)
			configPath := writeConfig(t, target)
			exerciseAllTransports(t, serverBinary, configPath)
		})
	}
}

func redisTargets(t *testing.T) []redisTarget {
	t.Helper()
	if os.Getenv(e2eDockerNetworkEnv) == "1" {
		return []redisTarget{
			{name: "standalone", addresses: []string{"standalone:6379"}},
			{name: "sentinel", addresses: []string{"sentinel:26379"}, sentinelMaster: "mymaster"},
			{name: "cluster", addresses: []string{"cluster-1:6379", "cluster-2:6379", "cluster-3:6379"}},
		}
	}
	if address := os.Getenv(e2eRedisAddressEnv); address != "" {
		return []redisTarget{{name: "standalone", addresses: []string{address}}}
	}
	t.Fatalf("set %s=1 or %s", e2eDockerNetworkEnv, e2eRedisAddressEnv)
	return nil
}

func exerciseAllTransports(t *testing.T, serverBinary, configPath string) {
	t.Helper()
	t.Run("stdio", func(t *testing.T) {
		mcpClient, err := client.NewStdioMCPClient(serverBinary, nil, "mcp", "--config", configPath)
		if err != nil {
			t.Fatalf("start stdio MCP client: %v", err)
		}
		t.Cleanup(func() { _ = mcpClient.Close() })
		exerciseMCP(t, mcpClient)
	})

	server := startHTTPServer(t, serverBinary, configPath)
	t.Run("streamable_http", func(t *testing.T) {
		mcpClient, err := client.NewStreamableHttpClient(server.baseURL + "/mcp")
		if err != nil {
			t.Fatalf("create streamable HTTP MCP client: %v", err)
		}
		t.Cleanup(func() { _ = mcpClient.Close() })
		startAndExerciseMCP(t, mcpClient)
	})
	t.Run("sse", func(t *testing.T) {
		mcpClient, err := client.NewSSEMCPClient(
			server.baseURL+"/sse",
			transport.WithEndpointTimeout(5*time.Second),
			transport.WithResponseTimeout(5*time.Second),
		)
		if err != nil {
			t.Fatalf("create SSE MCP client: %v", err)
		}
		t.Cleanup(func() { _ = mcpClient.Close() })
		startAndExerciseMCP(t, mcpClient)
	})
}

func seedRedis(t *testing.T, target redisTarget) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	redisClient := redis.NewUniversalClient(&redis.UniversalOptions{
		Addrs:      target.addresses,
		MasterName: target.sentinelMaster,
	})
	t.Cleanup(func() { _ = redisClient.Close() })
	if err := redisClient.Set(ctx, e2eKey, e2eValue, 0).Err(); err != nil {
		t.Fatalf("seed %s Redis fixture: %v", target.name, err)
	}
	t.Cleanup(func() { _ = redisClient.Del(context.Background(), e2eKey).Err() })
}

func buildServerBinary(t *testing.T) string {
	t.Helper()
	if serverBinary := os.Getenv(e2eServerBinaryEnv); serverBinary != "" {
		if info, err := os.Stat(serverBinary); err != nil {
			t.Fatalf("stat %s: %v", e2eServerBinaryEnv, err)
		} else if info.IsDir() {
			t.Fatalf("%s = %q is a directory", e2eServerBinaryEnv, serverBinary)
		}
		return serverBinary
	}
	serverBinary := filepath.Join(t.TempDir(), "redis-mcp-server")
	command := exec.Command("go", "build", "-o", serverBinary, "./cmd/redis-mcp-server")
	command.Dir = repositoryRoot(t)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("build redis-mcp-server: %v\n%s", err, output)
	}
	return serverBinary
}

func writeConfig(t *testing.T, target redisTarget) string {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	addresses, err := json.Marshal(target.addresses)
	if err != nil {
		t.Fatalf("marshal Redis addresses: %v", err)
	}
	// The compose Redis default user can run every command, including @admin
	// and FLUSHALL. This fixture opts out of that rejection. Unit tests cover
	// the default-deny path with restricted ACL rules.
	config := fmt.Sprintf(
		"allow_unsafe_database_role: true\nredis:\n  topology: %s\n  addresses: %s\n  sentinel_master: %q\n  write_enabled: false\n",
		target.name,
		addresses,
		target.sentinelMaster,
	)
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatalf("write E2E configuration: %v", err)
	}
	return configPath
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate E2E test source")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "..", ".."))
}

type httpServerProcess struct {
	baseURL string
	command *exec.Cmd
	done    <-chan error
	logPath string
	waited  bool
}

func startHTTPServer(t *testing.T, serverBinary, configPath string) *httpServerProcess {
	t.Helper()
	port := availablePort(t)
	logPath := filepath.Join(t.TempDir(), "redis-mcp-server.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("create server log: %v", err)
	}

	command := exec.Command(serverBinary, "mcp", "--config", configPath, "--port", strconv.Itoa(port))
	command.Stdout, command.Stderr = logFile, logFile
	if err := command.Start(); err != nil {
		_ = logFile.Close()
		t.Fatalf("start HTTP MCP server: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	server := &httpServerProcess{
		baseURL: "http://127.0.0.1:" + strconv.Itoa(port),
		command: command,
		done:    done,
		logPath: logPath,
	}
	t.Cleanup(func() {
		stopHTTPServer(t, server)
		_ = logFile.Close()
		if t.Failed() {
			if output, err := os.ReadFile(logPath); err == nil {
				t.Logf("HTTP MCP server log:\n%s", output)
			}
		}
	})
	waitForHealth(t, server)
	return server
}

func availablePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve HTTP port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func waitForHealth(t *testing.T, server *httpServerProcess) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		response, err := (&http.Client{Timeout: time.Second}).Get(server.baseURL + "/healthz")
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		select {
		case err := <-server.done:
			server.waited = true
			t.Fatalf("HTTP MCP server exited before becoming healthy: %v", err)
		default:
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("HTTP MCP server did not become healthy within 10 seconds")
}

func stopHTTPServer(t *testing.T, server *httpServerProcess) {
	t.Helper()
	if server.waited || server.command.Process == nil {
		return
	}
	if err := server.command.Process.Signal(os.Interrupt); err != nil && !strings.Contains(err.Error(), "process already finished") {
		t.Errorf("stop HTTP MCP server: %v", err)
		return
	}
	select {
	case err := <-server.done:
		server.waited = true
		if err != nil {
			t.Errorf("HTTP MCP server exited unsuccessfully: %v", err)
		}
	case <-time.After(10 * time.Second):
		if err := server.command.Process.Kill(); err != nil {
			t.Errorf("kill HTTP MCP server: %v", err)
			return
		}
		if err := <-server.done; err != nil {
			t.Errorf("HTTP MCP server did not exit cleanly: %v", err)
		}
		server.waited = true
	}
}

func startAndExerciseMCP(t *testing.T, mcpClient *client.Client) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := mcpClient.Start(ctx); err != nil {
		t.Fatalf("start MCP client: %v", err)
	}
	exerciseMCP(t, mcpClient)
}

func exerciseMCP(t *testing.T, mcpClient client.MCPClient) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	initialized, err := mcpClient.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo:      mcp.Implementation{Name: "redis-mcp-server-e2e", Version: "test"},
		},
	})
	if err != nil {
		t.Fatalf("initialize MCP session: %v", err)
	}
	if initialized.ServerInfo.Name != "redis-mcp-server" {
		t.Fatalf("MCP server name = %q, want redis-mcp-server", initialized.ServerInfo.Name)
	}

	tools, err := mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("list MCP tools: %v", err)
	}
	assertReadOnlyToolCatalog(t, tools)
	assertRedisStringValue(t, ctx, mcpClient)
}

func assertRedisStringValue(t *testing.T, ctx context.Context, mcpClient client.MCPClient) {
	t.Helper()
	result, err := mcpClient.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "redis_string",
			Arguments: map[string]any{
				"operation": "get",
				"args":      map[string]any{"key": e2eKey},
			},
		},
	})
	if err != nil {
		t.Fatalf("call redis_string/get through MCP: %v", err)
	}
	if result.IsError {
		t.Fatalf("redis_string/get returned MCP error: %#v", result.Content)
	}
	if len(result.Content) != 1 {
		t.Fatalf("redis_string/get returned %d content items, want 1", len(result.Content))
	}
	text, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("redis_string/get content type = %T, want mcp.TextContent", result.Content[0])
	}
	var payload struct {
		Status    string `json:"status"`
		Operation string `json:"operation"`
		Data      struct {
			Value struct {
				Base64 string `json:"base64"`
			} `json:"value"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(text.Text), &payload); err != nil {
		t.Fatalf("decode redis_string/get response %q: %v", text.Text, err)
	}
	if payload.Status != "ok" || payload.Operation != "get" {
		t.Fatalf("redis_string/get envelope = status=%q operation=%q, want ok/get", payload.Status, payload.Operation)
	}
	want := base64.StdEncoding.EncodeToString([]byte(e2eValue))
	if payload.Data.Value.Base64 != want {
		t.Fatalf("redis_string/get base64 value = %q, want %q", payload.Data.Value.Base64, want)
	}
}

func assertReadOnlyToolCatalog(t *testing.T, result *mcp.ListToolsResult) {
	t.Helper()
	if result == nil {
		t.Fatal("MCP tool list is nil")
	}
	names := make([]string, 0, len(result.Tools))
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
		if strings.HasSuffix(tool.Name, "_write") {
			t.Fatalf("read-only server exposed write tool %q", tool.Name)
		}
	}
	sort.Strings(names)
	want := []string{
		"redis_hash",
		"redis_key",
		"redis_list",
		"redis_observe",
		"redis_set",
		"redis_sorted_set",
		"redis_stream",
		"redis_string",
	}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("read-only MCP tool catalog = %v, want %v", names, want)
	}
}
