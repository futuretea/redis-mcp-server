package redisadapter

import (
	"context"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/futuretea/redis-mcp-server/pkg/core/config"
)

func TestVerifyStartupAgainstLocalRedis(t *testing.T) {
	binary, err := exec.LookPath("redis-server")
	if err != nil {
		t.Skip("redis-server is not installed")
	}
	exerciseStartupRedis(t, binary)
}

func TestVerifyStartupRedisVersionMatrix(t *testing.T) {
	raw := os.Getenv("REDIS_MATRIX_BINARIES")
	if strings.TrimSpace(raw) == "" {
		t.Skip("REDIS_MATRIX_BINARIES is not set")
	}
	seen := map[string]struct{}{}
	for _, binary := range strings.Split(raw, string(os.PathListSeparator)) {
		binary = strings.TrimSpace(binary)
		if binary == "" {
			continue
		}
		if _, ok := seen[binary]; ok {
			continue
		}
		seen[binary] = struct{}{}
		t.Run(binary, func(t *testing.T) {
			exerciseStartupRedis(t, binary)
		})
	}
}

func exerciseStartupRedis(t *testing.T, binary string) {
	t.Helper()
	addr := startRedisBinary(t, binary)
	admin := redis.NewClient(&redis.Options{Addr: addr, MaxRetries: -1})
	t.Cleanup(func() { _ = admin.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := admin.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping redis: %v", err)
	}
	version := redisVersion(t, ctx, admin)
	major := redisMajor(t, version)
	t.Logf("redis %s binary %s", version, binary)

	probe := []any{"+ping", "+acl|whoami", "+acl|getuser", "+acl|cat"}
	if major >= 7 {
		setRedisUser(t, ctx, admin, append([]any{"mcp-reader", "reset", "on", ">reader-secret", "~app:*", "resetchannels", "&", "-@all", "+@read", "-@admin", "-@dangerous", "-randomkey", "-scan"}, probe...)...)
		setRedisUser(t, ctx, admin, append([]any{"mcp-app", "reset", "on", ">app-secret", "~app:*", "resetchannels", "&", "-@all", "+@read", "+@write", "+@connection", "+@keyspace", "+@string", "+@hash", "+@list", "+@set", "+@sortedset", "+@stream", "+@fast", "-@admin", "-@dangerous", "-@scripting", "-flushall", "-flushdb", "-swapdb", "-migrate", "-memory|purge", "-randomkey", "-scan", "+cluster|slots", "+cluster|nodes", "+cluster|shards"}, probe...)...)
		setRedisUser(t, ctx, admin, append([]any{"mcp-legacy", "reset", "on", ">legacy-secret", "~app:*", "resetchannels", "&", "-@all", "+@read", "+@write", "-@admin", "-@dangerous", "-flushall", "-flushdb", "-swapdb", "-migrate"}, probe...)...)
		setRedisUser(t, ctx, admin, append([]any{"mcp-fun", "reset", "on", ">fun-secret", "~app:*", "resetchannels", "&", "-@all", "+@read", "(~* +function|flush)"}, probe...)...)
		setRedisUser(t, ctx, admin, append([]any{"mcp-script", "reset", "on", ">script-secret", "~app:*", "resetchannels", "&", "-@all", "+@read", "(~* +script|debug)"}, probe...)...)
		setRedisUser(t, ctx, admin, append([]any{"mcp-sel", "reset", "on", ">sel-secret", "~app:*", "resetchannels", "&", "-@all", "+@read", "(~other:* +flushall)"}, probe...)...)
		if major >= 8 {
			setRedisUser(t, ctx, admin, append([]any{"mcp-trim", "reset", "on", ">trim-secret", "~*", "-@all", "+trimslots"}, probe...)...)
			setRedisUser(t, ctx, admin, append([]any{"mcp-trimcat", "reset", "on", ">trimcat-secret", "~*", "-@all", "+@read", "+@write", "+@keyspace", "+@slow", "-@admin", "-@scripting", "-flushall", "-flushdb", "-swapdb", "-migrate", "-memory|purge"}, probe...)...)
			setRedisUser(t, ctx, admin, append([]any{"mcp-trimsel", "reset", "on", ">trimsel-secret", "~*", "-@all", "+@read", "-randomkey", "-scan", "(~* +trimslots)"}, probe...)...)
		}
	} else {
		setRedisUser(t, ctx, admin, append([]any{"mcp-reader", "reset", "on", ">reader-secret", "~app:*", "resetchannels", "&", "-@all", "+@read", "-@admin", "-@dangerous", "-memory", "-randomkey", "-scan"}, probe...)...)
		setRedisUser(t, ctx, admin, append([]any{"mcp-app", "reset", "on", ">app-secret", "~app:*", "resetchannels", "&", "-@all", "+@read", "+@write", "+@connection", "+@keyspace", "+@string", "+@hash", "+@list", "+@set", "+@sortedset", "+@stream", "+@fast", "-@admin", "-@dangerous", "-@scripting", "-memory", "-flushall", "-flushdb", "-swapdb", "-migrate", "-randomkey", "-scan"}, probe...)...)
		setRedisUser(t, ctx, admin, append([]any{"mcp-legacy", "reset", "on", ">legacy-secret", "~app:*", "resetchannels", "&", "-@all", "+@read", "-@admin", "-@dangerous"}, probe...)...)
	}
	setRedisUser(t, ctx, admin, "mcp-noprobe", "reset", "on", ">noprobe-secret", "~app:*", "-@all", "+@read", "+ping")
	if err := admin.Set(ctx, "app:sample", "hello-review", 0).Err(); err != nil {
		t.Fatalf("SET app:sample: %v", err)
	}
	if err := admin.Set(ctx, "secret:hidden", "topsecret", 0).Err(); err != nil {
		t.Fatalf("SET secret:hidden: %v", err)
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
		user := userClient(t, addr, "mcp-reader", "reader-secret")
		if got, err := user.Get(ctx, "app:sample").Result(); err != nil || got != "hello-review" {
			t.Fatalf("GET = %q, %v", got, err)
		}
		assertNoPerm(t, ctx, user, "config", "get", "dir")
		assertNoPerm(t, ctx, user, "flushall")
		assertNoPerm(t, ctx, user, "memory", "purge")
		assertNoPerm(t, ctx, user, "eval", "return 1", "0")
		assertNoPerm(t, ctx, user, "randomkey")
		assertNoPerm(t, ctx, user, "scan", "0")
		assertNoPerm(t, ctx, user, "get", "secret:hidden")
		hash := passwordHash(t, ctx, user, "mcp-app")
		if strings.Contains(output.String(), hash) {
			t.Fatal("startup log contained another user's password hash")
		}
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
		user := userClient(t, addr, "mcp-app", "app-secret")
		if err := user.Set(ctx, "app:sample", "hello-review", 0).Err(); err != nil {
			t.Fatalf("SET: %v", err)
		}
		if got, err := user.Get(ctx, "app:sample").Result(); err != nil || got != "hello-review" {
			t.Fatalf("GET = %q, %v", got, err)
		}
		denied := [][]any{
			{"flushall"},
			{"flushdb"},
			{"config", "get", "dir"},
			{"acl", "setuser", "ghost", "on", "nopass"},
			{"memory", "purge"},
			{"eval", "return 1", "0"},
			{"script", "flush"},
			{"script", "debug", "no"},
		}
		denied = append(denied, []any{"randomkey"}, []any{"scan", "0"}, []any{"get", "secret:hidden"})
		if major >= 7 {
			denied = append(denied,
				[]any{"function", "flush"},
				[]any{"function", "delete", "nosuch"},
				[]any{"function", "load", "#!lua name=lib\nredis.register_function('k', function() return 1 end)"},
			)
		}
		if major >= 8 {
			denied = append(denied, []any{"trimslots", "ranges", "1", "0", "0"})
		}
		for _, args := range denied {
			assertNoPerm(t, ctx, user, args...)
		}
	})

	t.Run("legacy business profile", func(t *testing.T) {
		_, err := openLiveAdapter(t, addr, "mcp-legacy", "legacy-secret")
		assertStartupCode(t, err, StartupCodeUnsafeServiceRole)
		if major >= 7 {
			if !dryRunAllowed(t, ctx, admin, "mcp-legacy", "function", "flush") {
				t.Fatal("legacy profile cannot run FUNCTION FLUSH, so the rejection is not the reported hole")
			}
			return
		}
		user := userClient(t, addr, "mcp-legacy", "legacy-secret")
		if err := user.Do(ctx, "memory", "purge").Err(); err != nil {
			t.Fatalf("MEMORY PURGE = %v, want the Redis 6 read category to allow it", err)
		}
	})

	if major >= 7 {
		t.Run("function selector", func(t *testing.T) {
			_, err := openLiveAdapter(t, addr, "mcp-fun", "fun-secret")
			assertStartupCode(t, err, StartupCodeUnsafeServiceRole)
			if !dryRunAllowed(t, ctx, admin, "mcp-fun", "function", "flush") {
				t.Fatal("function selector cannot run FUNCTION FLUSH")
			}
		})
		t.Run("script debug selector", func(t *testing.T) {
			_, err := openLiveAdapter(t, addr, "mcp-script", "script-secret")
			assertStartupCode(t, err, StartupCodeUnsafeServiceRole)
			if !dryRunAllowed(t, ctx, admin, "mcp-script", "script", "debug", "no") {
				t.Fatal("script selector cannot run SCRIPT DEBUG")
			}
		})
		t.Run("flushall selector", func(t *testing.T) {
			output := captureStartupLog(t)
			_, err := openLiveAdapter(t, addr, "mcp-sel", "sel-secret")
			assertStartupCode(t, err, StartupCodeUnsafeServiceRole)
			assertNoStartupOptOut(t, output.String(), "sel-secret")
		})
	}

	if major >= 8 {
		t.Run("trimslots direct", func(t *testing.T) {
			_, err := openLiveAdapter(t, addr, "mcp-trim", "trim-secret")
			assertStartupCode(t, err, StartupCodeUnsafeServiceRole)
			assertTrimslotsPermitted(t, ctx, admin, addr, "mcp-trim", "trim-secret")
		})
		t.Run("trimslots through categories", func(t *testing.T) {
			_, err := openLiveAdapter(t, addr, "mcp-trimcat", "trimcat-secret")
			assertStartupCode(t, err, StartupCodeUnsafeServiceRole)
			assertTrimslotsPermitted(t, ctx, admin, addr, "mcp-trimcat", "trimcat-secret")
		})
		t.Run("trimslots selector", func(t *testing.T) {
			_, err := openLiveAdapter(t, addr, "mcp-trimsel", "trimsel-secret")
			assertStartupCode(t, err, StartupCodeUnsafeServiceRole)
			assertTrimslotsPermitted(t, ctx, admin, addr, "mcp-trimsel", "trimsel-secret")
		})
	}

	t.Run("opt out does not skip probe failure", func(t *testing.T) {
		_, err := openLiveAdapterOpt(t, addr, "mcp-noprobe", "noprobe-secret", true)
		assertStartupCode(t, err, StartupCodePrivilegeProbeFailed)
		if err != nil && strings.Contains(err.Error(), "noprobe-secret") {
			t.Fatalf("probe error leaked password: %v", err)
		}
	})
}

func assertTrimslotsPermitted(t *testing.T, ctx context.Context, admin redis.UniversalClient, addr, username, password string) {
	t.Helper()
	user := userClient(t, addr, username, password)
	err := user.Do(ctx, "trimslots", "ranges", "1", "0", "0").Err()
	if err == nil || strings.Contains(strings.ToLower(err.Error()), "noperm") {
		t.Fatalf("TRIMSLOTS error = %v, want a non-permission failure", err)
	}
	if got, getErr := admin.Get(ctx, "secret:hidden").Result(); getErr != nil || got != "topsecret" {
		t.Fatalf("secret:hidden = %q, %v after TRIMSLOTS", got, getErr)
	}
}

func setRedisUser(t *testing.T, ctx context.Context, admin redis.UniversalClient, args ...any) {
	t.Helper()
	if err := admin.Do(ctx, append([]any{"acl", "setuser"}, args...)...).Err(); err != nil {
		t.Fatalf("ACL SETUSER %v: %v", args[0], err)
	}
}

func userClient(t *testing.T, addr, username, password string) *redis.Client {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: addr, Username: username, Password: password, MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func assertNoPerm(t *testing.T, ctx context.Context, client redis.UniversalClient, args ...any) {
	t.Helper()
	err := client.Do(ctx, args...).Err()
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "noperm") {
		t.Fatalf("%v error = %v, want NOPERM", args, err)
	}
}

func dryRunAllowed(t *testing.T, ctx context.Context, admin redis.UniversalClient, username string, args ...any) bool {
	t.Helper()
	command := append([]any{"acl", "dryrun", username}, args...)
	res, err := admin.Do(ctx, command...).Text()
	if err != nil {
		t.Fatalf("ACL DRYRUN %v: %v", args, err)
	}
	return strings.EqualFold(strings.TrimSpace(res), "OK")
}

func passwordHash(t *testing.T, ctx context.Context, client redis.UniversalClient, username string) string {
	t.Helper()
	raw, err := client.Do(ctx, "acl", "getuser", username).Result()
	if err != nil {
		t.Fatalf("ACL GETUSER %s: %v", username, err)
	}
	encoded := flattenRedis(raw)
	for _, item := range encoded {
		if len(item) == 64 && isHex(item) {
			return item
		}
	}
	t.Fatalf("ACL GETUSER %s returned no password hash", username)
	return ""
}

func flattenRedis(value any) []string {
	switch typed := value.(type) {
	case string:
		return []string{typed}
	case []byte:
		return []string{string(typed)}
	case []any:
		var out []string
		for _, item := range typed {
			out = append(out, flattenRedis(item)...)
		}
		return out
	case []string:
		return append([]string(nil), typed...)
	case map[string]any:
		var out []string
		for _, item := range typed {
			out = append(out, flattenRedis(item)...)
		}
		return out
	case map[any]any:
		var out []string
		for _, item := range typed {
			out = append(out, flattenRedis(item)...)
		}
		return out
	default:
		return nil
	}
}

func isHex(value string) bool {
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

func redisVersion(t *testing.T, ctx context.Context, admin redis.UniversalClient) string {
	t.Helper()
	info, err := admin.Info(ctx, "server").Result()
	if err != nil {
		t.Fatalf("INFO server: %v", err)
	}
	for _, line := range strings.Split(info, "\n") {
		if strings.HasPrefix(line, "redis_version:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "redis_version:"))
		}
	}
	t.Fatal("INFO server did not include redis_version")
	return ""
}

func redisMajor(t *testing.T, version string) int {
	t.Helper()
	major, err := strconv.Atoi(strings.Split(version, ".")[0])
	if err != nil {
		t.Fatalf("redis version %q: %v", version, err)
	}
	return major
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

func startRedisBinary(t *testing.T, binary string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("close port: %v", err)
	}
	command := exec.Command(binary, "--port", strconv.Itoa(port), "--bind", "127.0.0.1", "--save", "", "--appendonly", "no", "--protected-mode", "no", "--daemonize", "no")
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
