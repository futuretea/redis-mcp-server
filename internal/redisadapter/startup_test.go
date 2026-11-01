package redisadapter

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/futuretea/redis-mcp-server/pkg/core/config"
)

func TestVerifyStartupRejectsDangerousACL(t *testing.T) {
	output := captureStartupLog(t)
	rules := []string{
		"+@all",
		"allcommands",
		"+@admin",
		"-@all +flushall",
		"-@all +flushdb",
		"-@all +swapdb",
		"-@all +migrate",
		"-@all +config|get",
		"-@all +debug",
		"-@all +shutdown",
		"-@all +acl",
		"-@all +acl|setuser",
		"-@all +@write",
		"-@all +@connection",
		"-@all +@slow",
		"-@all +@keyspace",
		"-@all +cluster",
		"-@all +cluster|failover",
		"-@all +client",
		"-@all +client|kill",
		"-@all +module|load",
		"-@all +sentinel|get-master-addr-by-name",
	}
	for _, rule := range rules {
		t.Run(rule, func(t *testing.T) {
			output.Reset()
			adapter, err := openStartupAdapter(t, false, fakeACLProbe{
				username: "secret-user",
				user:     aclUserView{CommandRules: []string{rule}},
			})
			if adapter != nil {
				t.Fatal("startup returned an adapter for a dangerous ACL")
			}
			assertStartupCode(t, err, StartupCodeUnsafeServiceRole)
			assertNoStartupOptOut(t, output.String(), "secret-user")
		})
	}

	t.Run("allcommands flag", func(t *testing.T) {
		output.Reset()
		adapter, err := openStartupAdapter(t, false, fakeACLProbe{
			username: "secret-user",
			user: aclUserView{
				AllCommands:  true,
				CommandRules: []string{"-@all +@read +acl|whoami +acl|getuser +acl|cat"},
			},
		})
		if adapter != nil {
			t.Fatal("startup returned an adapter for allcommands")
		}
		assertStartupCode(t, err, StartupCodeUnsafeServiceRole)
		assertNoStartupOptOut(t, output.String(), "secret-user")
	})

	t.Run("dangerous selector", func(t *testing.T) {
		output.Reset()
		adapter, err := openStartupAdapter(t, false, fakeACLProbe{
			username: "secret-user",
			user: aclUserView{CommandRules: []string{
				"-@all +@read +ping +acl|whoami +acl|getuser +acl|cat",
				"+flushall",
			}},
		})
		if adapter != nil {
			t.Fatal("startup returned an adapter for a dangerous selector")
		}
		assertStartupCode(t, err, StartupCodeUnsafeServiceRole)
	})

	t.Run("server admin command", func(t *testing.T) {
		output.Reset()
		adapter, err := openStartupAdapter(t, false, fakeACLProbe{
			username:   "secret-user",
			user:       aclUserView{CommandRules: []string{"-@all +newadmin"}},
			categories: map[string][]string{"admin": {"newadmin"}},
		})
		if adapter != nil {
			t.Fatal("startup returned an adapter for a server-reported admin command")
		}
		assertStartupCode(t, err, StartupCodeUnsafeServiceRole)
	})
}

func TestVerifyStartupAllowsDangerousACLWhenOptedIn(t *testing.T) {
	output := captureStartupLog(t)
	adapter, err := openStartupAdapter(t, true, fakeACLProbe{
		username: "secret-user",
		user:     aclUserView{CommandRules: []string{"+@all"}},
	})
	if err != nil {
		t.Fatalf("VerifyStartup: %v", err)
	}
	if adapter == nil {
		t.Fatal("startup adapter = nil, want started adapter")
	}
	text := output.String()
	for _, want := range []string{`"level":"warn"`, startupRoleOptOutMessage} {
		if !strings.Contains(text, want) {
			t.Fatalf("log = %q, want %q", text, want)
		}
	}
	if strings.Contains(text, "secret-user") || strings.Contains(text, "+@all") {
		t.Fatalf("opt-out log leaked account details: %s", text)
	}
}

func TestVerifyStartupStartsRestrictedACLWithoutOptOut(t *testing.T) {
	output := captureStartupLog(t)
	rules := []string{
		"-@all +@read -@admin -@dangerous +ping +acl|whoami +acl|getuser +acl|cat",
		"-@all +@read +@write +@connection +@string +@fast +@slow -@admin -@dangerous -flushall -flushdb -swapdb -migrate +ping +acl|whoami +acl|getuser +acl|cat +restore +cluster|slots +cluster|nodes +cluster|shards",
		"-@all +cluster|slots +cluster|nodes +cluster|shards +ping +acl|whoami +acl|getuser +acl|cat",
		"-@all +@connection -@admin +ping +acl|whoami +acl|getuser +acl|cat",
		"+@all -@admin -@dangerous",
	}
	for _, rule := range rules {
		t.Run(rule, func(t *testing.T) {
			output.Reset()
			adapter, err := openStartupAdapter(t, false, fakeACLProbe{
				username: "mcp-reader",
				user:     aclUserView{CommandRules: []string{rule}},
			})
			if err != nil {
				t.Fatalf("VerifyStartup: %v", err)
			}
			if adapter == nil {
				t.Fatal("startup adapter = nil, want started adapter")
			}
			assertNoStartupOptOut(t, output.String(), "mcp-reader")
		})
	}
}

func TestVerifyStartupOptOutDoesNotSkipOtherFailures(t *testing.T) {
	output := captureStartupLog(t)

	t.Run("connection failure", func(t *testing.T) {
		output.Reset()
		adapter, err := openStartupAdapter(t, true, fakeACLProbe{
			pingErr: errors.New("dial tcp 127.0.0.1:1: connect: connection refused secret-user"),
		})
		if adapter != nil {
			t.Fatal("startup returned an adapter after the connection check failed")
		}
		assertStartupCode(t, err, StartupCodeConnectionFailed)
		if strings.Contains(err.Error(), "secret-user") {
			t.Fatalf("connection error leaked probe text: %v", err)
		}
		assertNoStartupOptOut(t, output.String(), "secret-user")
	})

	t.Run("acl probe denied", func(t *testing.T) {
		output.Reset()
		adapter, err := openStartupAdapter(t, true, fakeACLProbe{
			username: "secret-user",
			userErr:  errors.New("NOPERM this user has no permissions to run the 'acl|getuser' command secret-user"),
		})
		if adapter != nil {
			t.Fatal("startup returned an adapter after the ACL probe was denied")
		}
		assertStartupCode(t, err, StartupCodePrivilegeProbeFailed)
		if strings.Contains(err.Error(), "secret-user") || strings.Contains(err.Error(), "NOPERM") {
			t.Fatalf("probe error leaked Redis text: %v", err)
		}
		assertNoStartupOptOut(t, output.String(), "secret-user")
	})

	t.Run("malformed acl reply", func(t *testing.T) {
		output.Reset()
		adapter, err := openStartupAdapter(t, true, fakeACLProbe{
			username: "secret-user",
			rawUser:  []any{"flags", []any{"on"}},
		})
		if adapter != nil {
			t.Fatal("startup returned an adapter for a malformed ACL reply")
		}
		assertStartupCode(t, err, StartupCodePrivilegeProbeFailed)
		assertNoStartupOptOut(t, output.String(), "secret-user")
	})

	t.Run("unknown allow category", func(t *testing.T) {
		adapter, err := openStartupAdapter(t, true, fakeACLProbe{
			username: "secret-user",
			user:     aclUserView{CommandRules: []string{"-@all +@custom"}},
		})
		if adapter != nil {
			t.Fatal("startup returned an adapter for an unknown ACL category")
		}
		assertStartupCode(t, err, StartupCodePrivilegeProbeFailed)
	})
}

func TestParseACLGetUserReadsRootRulesAndSelectors(t *testing.T) {
	view, err := parseACLGetUser([]any{
		"flags", []any{"on", "allkeys"},
		"passwords", []any{},
		"commands", "-@all +@read +acl|whoami +acl|getuser +acl|cat",
		"keys", "~app:*",
		"channels", "&*",
		"selectors", []any{
			[]any{"commands", "+@admin", "keys", "~other:*", "channels", "&*"},
		},
	})
	if err != nil {
		t.Fatalf("parseACLGetUser: %v", err)
	}
	if view.AllCommands {
		t.Fatal("AllCommands = true, want false")
	}
	want := []string{
		"-@all +@read +acl|whoami +acl|getuser +acl|cat",
		"+@admin",
	}
	if strings.Join(view.CommandRules, "\n") != strings.Join(want, "\n") {
		t.Fatalf("rules = %#v, want %#v", view.CommandRules, want)
	}

	flagged, err := parseACLGetUser(map[string]any{
		"flags":    []any{"on", "allcommands"},
		"commands": "+@all",
	})
	if err != nil {
		t.Fatalf("parseACLGetUser flags: %v", err)
	}
	if !flagged.AllCommands || len(flagged.CommandRules) != 1 || flagged.CommandRules[0] != "+@all" {
		t.Fatalf("flagged view = %#v", flagged)
	}
}

func openStartupAdapter(t *testing.T, allow bool, probe fakeACLProbe) (*Adapter, error) {
	t.Helper()
	adapter, err := New(config.RedisConfig{Topology: "standalone", Addresses: []string{"127.0.0.1:1"}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	adapter.aclProbe = probe
	if err := adapter.VerifyStartup(context.Background(), allow); err != nil {
		_ = adapter.Close()
		return nil, err
	}
	t.Cleanup(func() { _ = adapter.Close() })
	return adapter, nil
}

func assertStartupCode(t *testing.T, err error, want string) {
	t.Helper()
	var startup *StartupError
	if !errors.As(err, &startup) || startup.Code != want {
		t.Fatalf("startup error = %v, want code %s", err, want)
	}
	if !strings.Contains(err.Error(), "redis_"+want) {
		t.Fatalf("error = %q, want redis_%s", err.Error(), want)
	}
}

func assertNoStartupOptOut(t *testing.T, text, secret string) {
	t.Helper()
	if strings.Contains(text, startupRoleOptOutMessage) || (secret != "" && strings.Contains(text, secret)) {
		t.Fatalf("startup log = %q, want no opt-out warning or account details", text)
	}
}

func captureStartupLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var output bytes.Buffer
	previousLogger := log.Logger
	previousLevel := zerolog.GlobalLevel()
	log.Logger = zerolog.New(&output)
	zerolog.SetGlobalLevel(zerolog.TraceLevel)
	t.Cleanup(func() {
		log.Logger = previousLogger
		zerolog.SetGlobalLevel(previousLevel)
	})
	return &output
}

type fakeACLProbe struct {
	pingErr       error
	username      string
	whoErr        error
	user          aclUserView
	userErr       error
	rawUser       any
	categories    map[string][]string
	categoriesErr error
}

func (p fakeACLProbe) Ping(context.Context) error { return p.pingErr }

func (p fakeACLProbe) WhoAmI(context.Context) (string, error) {
	if p.whoErr != nil {
		return "", p.whoErr
	}
	if p.username == "" {
		return "mcp-reader", nil
	}
	return p.username, nil
}

func (p fakeACLProbe) GetUser(_ context.Context, username string) (aclUserView, error) {
	if p.userErr != nil {
		return aclUserView{}, p.userErr
	}
	if username != p.username && !(p.username == "" && username == "mcp-reader") {
		return aclUserView{}, errors.New("probe queried the wrong user")
	}
	if p.rawUser != nil {
		return parseACLGetUser(p.rawUser)
	}
	return p.user, nil
}

func (p fakeACLProbe) Categories(context.Context) (map[string][]string, error) {
	if p.categoriesErr != nil {
		return nil, p.categoriesErr
	}
	return p.categories, nil
}
