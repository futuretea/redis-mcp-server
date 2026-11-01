package redisadapter

import (
	"context"
	"io"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/futuretea/redis-mcp-server/pkg/core/config"
)

func TestVerifyStartupAgainstLocalRedis(t *testing.T) {
	if _, err := exec.LookPath("redis-server"); err != nil {
		t.Skip("redis-server is not installed")
	}
	addr := startLocalRedis(t)
	admin := redis.NewClient(&redis.Options{Addr: addr, MaxRetries: -1})
	t.Cleanup(func() { _ = admin.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := admin.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping redis: %v", err)
	}

	readerRule := []any{"mcp-reader", "reset", "on", ">reader-secret", "~app:*", "resetchannels", "&", "-@all", "+@read", "-@admin", "-@dangerous", "+ping", "+acl|whoami", "+acl|getuser", "+acl|cat"}
	if err := admin.Do(ctx, append([]any{"acl", "setuser"}, readerRule...)...).Err(); err != nil {
		t.Fatalf("ACL SETUSER reader: %v", err)
	}
	businessRule := []any{"mcp-app", "reset", "on", ">app-secret", "~app:*", "resetchannels", "&", "-@all", "+@read", "+@write", "+@connection", "+@keyspace", "+@string", "+@hash", "+@list", "+@set", "+@sortedset", "+@stream", "+@fast", "+@slow", "-@admin", "-@dangerous", "-flushall", "-flushdb", "-swapdb", "-migrate", "+ping", "+acl|whoami", "+acl|getuser", "+acl|cat", "+restore", "+cluster|slots", "+cluster|nodes", "+cluster|shards"}
	if err := admin.Do(ctx, append([]any{"acl", "setuser"}, businessRule...)...).Err(); err != nil {
		t.Fatalf("ACL SETUSER app: %v", err)
	}
	selectorRule := []any{"mcp-sel", "reset", "on", ">sel-secret", "~app:*", "resetchannels", "&", "-@all", "+@read", "+ping", "+acl|whoami", "+acl|getuser", "+acl|cat", "(~other:* +flushall)"}
	if err := admin.Do(ctx, append([]any{"acl", "setuser"}, selectorRule...)...).Err(); err != nil {
		t.Fatalf("ACL SETUSER selector: %v", err)
	}

	t.Run("default user", func(t *testing.T) {
		output := captureStartupLog(t)
		_, err := openLiveAdapter(t, addr, "default", "")
		assertStartupCode(t, err, StartupCodeUnsafeServiceRole)
		assertNoStartupOptOut(t, output.String(), "")
	})

	t.Run("default user opt out", func(t *testing.T) {
		output := captureStartupLog(t)
		adapter, err := openLiveAdapterOpt(t, addr, "default", "", true)
		if err != nil {
			t.Fatalf("VerifyStartup: %v", err)
		}
		if adapter == nil {
			t.Fatal("startup adapter = nil")
		}
		if !strings.Contains(output.String(), startupRoleOptOutMessage) {
			t.Fatalf("log = %q, want opt-out warning", output.String())
		}
	})

	t.Run("read only", func(t *testing.T) {
		output := captureStartupLog(t)
		adapter, err := openLiveAdapter(t, addr, "mcp-reader", "reader-secret")
		if err != nil {
			t.Fatalf("VerifyStartup: %v\nlog=%s", err, output.String())
		}
		if adapter == nil {
			t.Fatal("startup adapter = nil")
		}
		assertNoStartupOptOut(t, output.String(), "reader-secret")
	})

	t.Run("business", func(t *testing.T) {
		output := captureStartupLog(t)
		adapter, err := openLiveAdapter(t, addr, "mcp-app", "app-secret")
		if err != nil {
			t.Fatalf("VerifyStartup: %v\nlog=%s", err, output.String())
		}
		if adapter == nil {
			t.Fatal("startup adapter = nil")
		}
		assertNoStartupOptOut(t, output.String(), "app-secret")
	})

	t.Run("flushall selector", func(t *testing.T) {
		output := captureStartupLog(t)
		_, err := openLiveAdapter(t, addr, "mcp-sel", "sel-secret")
		assertStartupCode(t, err, StartupCodeUnsafeServiceRole)
		assertNoStartupOptOut(t, output.String(), "sel-secret")
	})
}

func openLiveAdapter(t *testing.T, addr, username, password string) (*Adapter, error) {
	t.Helper()
	return openLiveAdapterOpt(t, addr, username, password, false)
}

func openLiveAdapterOpt(t *testing.T, addr, username, password string, allow bool) (*Adapter, error) {
	t.Helper()
	t.Setenv("MCP_REDIS_PASSWORD", password)
	adapter, err := New(config.RedisConfig{Topology: "standalone", Addresses: []string{addr}, Username: username})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := adapter.VerifyStartup(ctx, allow); err != nil {
		_ = adapter.Close()
		return nil, err
	}
	t.Cleanup(func() { _ = adapter.Close() })
	return adapter, nil
}

func startLocalRedis(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("close port: %v", err)
	}
	command := exec.Command("redis-server", "--port", strconv.Itoa(port), "--bind", "127.0.0.1", "--save", "", "--appendonly", "no", "--protected-mode", "no", "--daemonize", "no")
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		t.Fatalf("start redis: %v", err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_, _ = command.Process.Wait()
	})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:" + strconv.Itoa(port), MaxRetries: -1})
		err := client.Ping(context.Background()).Err()
		_ = client.Close()
		if err == nil {
			return "127.0.0.1:" + strconv.Itoa(port)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("redis did not accept connections")
	return ""
}
