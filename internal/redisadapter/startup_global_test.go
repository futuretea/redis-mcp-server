package redisadapter

import (
	"testing"
)

func TestVerifyStartupRejectsGlobalOperationsInBusinessCategories(t *testing.T) {
	categories := map[string][]string{
		"write":     {"set", "function|flush", "function|delete", "function|load", "function|restore", "flushall"},
		"scripting": {"function|flush", "function|delete", "function|load", "function|restore", "function|kill", "script|flush", "script|debug", "script|kill", "script|load", "eval", "evalsha", "fcall", "fcall_ro"},
		"slow":      {"memory", "memory|purge", "memory|usage", "eval", "script"},
		"read":      {"get", "memory|usage"},
	}
	rules := []string{
		"-@all +@read +@write -@admin -@dangerous -flushall -flushdb -swapdb -migrate +ping +acl|whoami +acl|getuser +acl|cat",
		"-@all +function|flush +ping +acl|whoami +acl|getuser +acl|cat",
		"-@all +function|delete +ping +acl|whoami +acl|getuser +acl|cat",
		"-@all +function|load +ping +acl|whoami +acl|getuser +acl|cat",
		"-@all +function|restore +ping +acl|whoami +acl|getuser +acl|cat",
		"-@all +script|flush +ping +acl|whoami +acl|getuser +acl|cat",
		"-@all +script|debug +ping +acl|whoami +acl|getuser +acl|cat",
		"-@all +eval +ping +acl|whoami +acl|getuser +acl|cat",
		"-@all +fcall +ping +acl|whoami +acl|getuser +acl|cat",
		"-@all +memory|purge +ping +acl|whoami +acl|getuser +acl|cat",
		"+@all -@admin -@dangerous",
	}
	for _, rule := range rules {
		t.Run(rule, func(t *testing.T) {
			adapter, err := openStartupAdapter(t, false, fakeACLProbe{
				username:   "mcp-app",
				user:       aclUserView{CommandRules: []string{rule}},
				categories: categories,
			})
			if adapter != nil {
				t.Fatal("startup returned an adapter for a global operation")
			}
			assertStartupCode(t, err, StartupCodeUnsafeServiceRole)
		})
	}

	t.Run("function flush selector", func(t *testing.T) {
		adapter, err := openStartupAdapter(t, false, fakeACLProbe{
			username: "mcp-app",
			user: aclUserView{CommandRules: []string{
				"-@all +@read +ping +acl|whoami +acl|getuser +acl|cat",
				"-@all +function|flush",
			}},
			categories: categories,
		})
		if adapter != nil {
			t.Fatal("startup returned an adapter for a function flush selector")
		}
		assertStartupCode(t, err, StartupCodeUnsafeServiceRole)
	})
}

func TestVerifyStartupAllowsBusinessDataPlaneAfterGlobalRevokes(t *testing.T) {
	categories := map[string][]string{
		"write":      {"set", "append", "function|flush", "function|delete", "function|load", "function|restore", "flushall", "flushdb", "swapdb", "migrate"},
		"scripting":  {"function|flush", "function|delete", "function|load", "function|restore", "function|kill", "script|flush", "script|debug", "eval", "evalsha", "fcall"},
		"slow":       {"memory", "memory|purge", "memory|usage", "eval", "script", "flushall"},
		"read":       {"get", "scan", "memory|usage"},
		"string":     {"get", "set"},
		"connection": {"ping", "client|setname", "client|kill"},
		"keyspace":   {"dbsize", "scan", "flushall"},
	}
	rules := []string{
		"-@all +@read +@write +@string +@connection +@keyspace -@admin -@dangerous -@scripting -flushall -flushdb -swapdb -migrate -memory|purge +ping +acl|whoami +acl|getuser +acl|cat +cluster|slots +cluster|nodes +cluster|shards",
		"+@all -@admin -@dangerous -@scripting -function -script -eval -evalsha -fcall -memory -memory|purge",
		"-@all +@read +memory|usage +ping +acl|whoami +acl|getuser +acl|cat",
	}
	for _, rule := range rules {
		t.Run(rule, func(t *testing.T) {
			adapter, err := openStartupAdapter(t, false, fakeACLProbe{
				username:   "mcp-app",
				user:       aclUserView{CommandRules: []string{rule}},
				categories: categories,
			})
			if err != nil {
				t.Fatalf("VerifyStartup: %v", err)
			}
			if adapter == nil {
				t.Fatal("startup adapter = nil, want started adapter")
			}
		})
	}
}

func TestVerifyStartupRedis6ProbeSubcommandsStaySafe(t *testing.T) {
	categories := map[string][]string{
		"admin":      {"acl", "config", "client", "cluster", "debug", "shutdown", "module"},
		"read":       {"get", "keys", "memory"},
		"write":      {"set", "flushall", "flushdb", "swapdb", "migrate", "restore"},
		"dangerous":  {"acl", "config", "client", "cluster", "keys", "flushall", "flushdb", "info", "restore"},
		"scripting":  {"eval", "evalsha", "script"},
		"connection": {"client"},
	}
	t.Run("probe and read after memory revoke", func(t *testing.T) {
		adapter, err := openStartupAdapter(t, false, fakeACLProbe{
			username: "mcp-reader",
			user: aclUserView{CommandRules: []string{
				"-@all +@read -@admin -@dangerous -memory +ping +acl|whoami +acl|getuser +acl|cat",
			}},
			categories: categories,
		})
		if err != nil {
			t.Fatalf("VerifyStartup: %v", err)
		}
		if adapter == nil {
			t.Fatal("startup adapter = nil, want started adapter")
		}
	})

	t.Run("read grants whole memory command", func(t *testing.T) {
		adapter, err := openStartupAdapter(t, false, fakeACLProbe{
			username: "mcp-reader",
			user: aclUserView{CommandRules: []string{
				"-@all +@read -@admin -@dangerous +ping +acl|whoami +acl|getuser +acl|cat",
			}},
			categories: categories,
		})
		if adapter != nil {
			t.Fatal("startup returned an adapter for a Redis 6 read category that includes memory")
		}
		assertStartupCode(t, err, StartupCodeUnsafeServiceRole)
	})

	t.Run("safe client and cluster subcommands", func(t *testing.T) {
		adapter, err := openStartupAdapter(t, false, fakeACLProbe{
			username: "mcp-reader",
			user: aclUserView{CommandRules: []string{
				"-@all +get +cluster|slots +cluster|nodes +client|setname +ping +acl|whoami +acl|getuser +acl|cat",
			}},
			categories: categories,
		})
		if err != nil {
			t.Fatalf("VerifyStartup: %v", err)
		}
		if adapter == nil {
			t.Fatal("startup adapter = nil, want started adapter")
		}
	})

	t.Run("whole client command", func(t *testing.T) {
		adapter, err := openStartupAdapter(t, false, fakeACLProbe{
			username: "mcp-reader",
			user: aclUserView{CommandRules: []string{
				"-@all +client +ping +acl|whoami +acl|getuser +acl|cat",
			}},
			categories: categories,
		})
		if adapter != nil {
			t.Fatal("startup returned an adapter for the whole client command")
		}
		assertStartupCode(t, err, StartupCodeUnsafeServiceRole)
	})
}
