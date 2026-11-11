package redisadapter

import (
	"context"
	"errors"
	"strings"
	"unicode"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
)

const startupRoleOptOutMessage = "database service role is not permitted; allow_unsafe_database_role is set, so the startup role check is skipped"

const (
	// StartupCodeUnsafeServiceRole is the default-deny rejection for an account
	// that can run administrative commands, global data erasure, server-side
	// code, or MEMORY PURGE.
	StartupCodeUnsafeServiceRole = "unsafe_service_role"
	// StartupCodeConnectionFailed means Redis could not be reached.
	StartupCodeConnectionFailed = "connection_failed"
	// StartupCodePrivilegeProbeFailed means the ACL probe did not complete.
	// allow_unsafe_database_role does not skip this failure.
	StartupCodePrivilegeProbeFailed = "privilege_probe_failed"
)

// StartupError is a fixed caller-facing startup failure. It does not include
// Redis replies, usernames, or command text.
type StartupError struct {
	Code string
}

func (e *StartupError) Error() string {
	if e == nil {
		return "[redis_privilege_probe_failed] Redis ACL probe failed"
	}
	switch e.Code {
	case StartupCodeUnsafeServiceRole:
		return "[redis_unsafe_service_role] database service role is not permitted"
	case StartupCodeConnectionFailed:
		return "[redis_connection_failed] connection failed"
	default:
		return "[redis_privilege_probe_failed] Redis ACL probe failed"
	}
}

func startupError(code string) error {
	return &StartupError{Code: code}
}

// VerifyStartup connects and rejects dangerous Redis ACL capabilities before
// any tool command runs. allowUnsafeDatabaseRole skips only that rejection.
// A refused connection or a failed ACL probe still aborts startup.
func (a *Adapter) VerifyStartup(ctx context.Context, allowUnsafeDatabaseRole bool) error {
	if a == nil {
		return startupError(StartupCodeConnectionFailed)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	probe := a.aclProbe
	if probe == nil {
		if a.client == nil {
			return startupError(StartupCodeConnectionFailed)
		}
		probe = redisACLProbe{client: a.client}
	}
	return verifyStartup(ctx, probe, allowUnsafeDatabaseRole)
}

func verifyStartup(ctx context.Context, probe aclProbe, allowUnsafeDatabaseRole bool) error {
	unsafe, err := inspectACL(ctx, probe)
	if err != nil {
		return err
	}
	if !unsafe {
		return nil
	}
	if allowUnsafeDatabaseRole {
		log.Warn().Msg(startupRoleOptOutMessage)
		return nil
	}
	return startupError(StartupCodeUnsafeServiceRole)
}

type aclProbe interface {
	Ping(context.Context) error
	WhoAmI(context.Context) (string, error)
	GetUser(context.Context, string) (aclUserView, error)
	Categories(context.Context) (map[string][]string, error)
}

type aclUserView struct {
	AllCommands  bool
	CommandRules []string
}

type redisACLProbe struct {
	client redis.UniversalClient
}

func (p redisACLProbe) Ping(ctx context.Context) error {
	return p.client.Ping(ctx).Err()
}

func (p redisACLProbe) WhoAmI(ctx context.Context) (string, error) {
	return p.client.ACLWhoAmI(ctx).Result()
}

func (p redisACLProbe) GetUser(ctx context.Context, username string) (aclUserView, error) {
	raw, err := p.client.Do(ctx, "acl", "getuser", username).Result()
	if err != nil {
		return aclUserView{}, err
	}
	return parseACLGetUser(raw)
}

func (p redisACLProbe) Categories(ctx context.Context) (map[string][]string, error) {
	names, err := p.client.ACLCat(ctx).Result()
	if err != nil {
		return nil, err
	}
	categories := make(map[string][]string, len(names))
	for _, name := range names {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" || name == "all" {
			continue
		}
		members, err := p.client.ACLCatArgs(ctx, &redis.ACLCatArgs{Category: name}).Result()
		if err != nil {
			return nil, err
		}
		copied := make([]string, 0, len(members))
		for _, member := range members {
			member = strings.ToLower(strings.TrimSpace(member))
			if member != "" {
				copied = append(copied, member)
			}
		}
		categories[name] = copied
	}
	return categories, nil
}

func inspectACL(ctx context.Context, probe aclProbe) (bool, error) {
	if err := probe.Ping(ctx); err != nil {
		return false, classifyProbeError(err)
	}
	username, err := probe.WhoAmI(ctx)
	if err != nil {
		return false, classifyProbeError(err)
	}
	if !validACLUsername(username) {
		return false, startupError(StartupCodePrivilegeProbeFailed)
	}
	user, err := probe.GetUser(ctx, username)
	if err != nil {
		return false, classifyProbeError(err)
	}
	categories, err := probe.Categories(ctx)
	if err != nil {
		return false, classifyProbeError(err)
	}
	if user.AllCommands {
		return true, nil
	}
	unsafe, err := rulesUnsafe(user.CommandRules, mergeACLCategories(categories))
	if err != nil {
		return false, startupError(StartupCodePrivilegeProbeFailed)
	}
	return unsafe, nil
}

func classifyProbeError(err error) error {
	if err == nil {
		return nil
	}
	var startup *StartupError
	if errors.As(err, &startup) {
		return startup
	}
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "noperm"),
		strings.Contains(text, "noauth"),
		strings.Contains(text, "wrongpass"),
		strings.Contains(text, "unknown command"):
		return startupError(StartupCodePrivilegeProbeFailed)
	default:
		return startupError(StartupCodeConnectionFailed)
	}
}

func validACLUsername(name string) bool {
	if name == "" || len(name) > 1024 {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func parseACLGetUser(raw any) (aclUserView, error) {
	fields, err := redisMap(raw)
	if err != nil {
		return aclUserView{}, err
	}
	commands, ok := redisString(fields["commands"])
	if !ok {
		return aclUserView{}, errMalformedACL
	}
	view := aclUserView{CommandRules: []string{commands}}
	if flags, ok := fields["flags"]; ok {
		items, err := redisArray(flags)
		if err != nil {
			return aclUserView{}, err
		}
		for _, item := range items {
			flag, ok := redisString(item)
			if !ok {
				return aclUserView{}, errMalformedACL
			}
			if strings.EqualFold(flag, "allcommands") {
				view.AllCommands = true
			}
		}
	}
	selectors, ok := fields["selectors"]
	if !ok || selectors == nil {
		return view, nil
	}
	items, err := redisArray(selectors)
	if err != nil {
		return aclUserView{}, err
	}
	for _, item := range items {
		selector, err := redisMap(item)
		if err != nil {
			return aclUserView{}, err
		}
		rule, ok := redisString(selector["commands"])
		if !ok {
			return aclUserView{}, errMalformedACL
		}
		view.CommandRules = append(view.CommandRules, rule)
	}
	return view, nil
}

var errMalformedACL = errProbe()

func errProbe() error { return startupError(StartupCodePrivilegeProbeFailed) }

func redisMap(value any) (map[string]any, error) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, nil
	case map[interface{}]interface{}:
		mapped := make(map[string]any, len(typed))
		for key, item := range typed {
			text, ok := redisString(key)
			if !ok {
				return nil, errMalformedACL
			}
			mapped[strings.ToLower(text)] = item
		}
		return mapped, nil
	default:
		items, err := redisArray(value)
		if err != nil {
			return nil, err
		}
		if len(items)%2 != 0 {
			return nil, errMalformedACL
		}
		mapped := make(map[string]any, len(items)/2)
		for i := 0; i < len(items); i += 2 {
			key, ok := redisString(items[i])
			if !ok || strings.TrimSpace(key) == "" {
				return nil, errMalformedACL
			}
			mapped[strings.ToLower(key)] = items[i+1]
		}
		return mapped, nil
	}
}

func redisArray(value any) ([]any, error) {
	switch typed := value.(type) {
	case nil:
		return []any{}, nil
	case []any:
		return typed, nil
	case []string:
		items := make([]any, len(typed))
		for i, item := range typed {
			items[i] = item
		}
		return items, nil
	default:
		return nil, errMalformedACL
	}
}

func redisString(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		return typed, true
	case []byte:
		return string(typed), true
	default:
		return "", false
	}
}

type dangerModel struct {
	parents      map[string]struct{}
	anySub       map[string]struct{}
	subs         map[string]struct{}
	subsByParent map[string]map[string]struct{}
}

type grantState struct {
	star   map[string]bool
	subs   map[string]map[string]bool
	denied map[string]map[string]struct{}
}

func rulesUnsafe(rules []string, categories map[string][]string) (bool, error) {
	model := buildDanger(categories)
	parents := knownParents(categories)
	for _, rule := range rules {
		unsafe, err := oneRuleUnsafe(rule, categories, model, parents)
		if err != nil {
			return false, err
		}
		if unsafe {
			return true, nil
		}
	}
	return false, nil
}

func oneRuleUnsafe(rule string, categories map[string][]string, model dangerModel, parents []string) (bool, error) {
	state := grantState{
		star:   map[string]bool{},
		subs:   map[string]map[string]bool{},
		denied: map[string]map[string]struct{}{},
	}
	for _, token := range strings.Fields(rule) {
		if err := state.apply(token, categories, model, parents); err != nil {
			return false, err
		}
	}
	return state.unsafe(model), nil
}

func (g *grantState) apply(token string, categories map[string][]string, model dangerModel, parents []string) error {
	token = strings.ToLower(strings.TrimSpace(token))
	switch token {
	case "":
		return nil
	case "allcommands":
		g.allowEverything(parents)
		return nil
	case "nocommands":
		g.denyEverything()
		return nil
	}
	if token[0] != '+' && token[0] != '-' {
		return errMalformedACL
	}
	allow := token[0] == '+'
	body := token[1:]
	command, sub, hasSub := strings.Cut(body, "|")
	if body == "" || command == "" || (hasSub && (sub == "" || strings.Contains(sub, "|"))) {
		return errMalformedACL
	}
	if strings.HasPrefix(body, "@") {
		category := strings.TrimPrefix(body, "@")
		if category == "" {
			return errMalformedACL
		}
		if category == "all" {
			if allow {
				g.allowEverything(parents)
			} else {
				g.denyEverything()
			}
			return nil
		}
		members, ok := categories[category]
		if !ok {
			if allow {
				return errMalformedACL
			}
			return nil
		}
		for _, member := range members {
			if allow {
				g.allowSpec(member)
			} else {
				g.denySpec(member, model)
			}
		}
		return nil
	}
	if allow {
		g.allowSpec(body)
	} else {
		g.denySpec(body, model)
	}
	return nil
}

func (g *grantState) allowEverything(parents []string) {
	g.star = make(map[string]bool, len(parents))
	g.subs = map[string]map[string]bool{}
	g.denied = map[string]map[string]struct{}{}
	for _, parent := range parents {
		g.star[parent] = true
	}
}

func (g *grantState) denyEverything() {
	g.star = map[string]bool{}
	g.subs = map[string]map[string]bool{}
	g.denied = map[string]map[string]struct{}{}
}

func (g *grantState) allowSpec(spec string) {
	command, sub, hasSub := strings.Cut(spec, "|")
	if command == "" || (hasSub && sub == "") {
		return
	}
	if !hasSub {
		g.star[command] = true
		delete(g.subs, command)
		delete(g.denied, command)
		return
	}
	if g.star[command] {
		if denied := g.denied[command]; denied != nil {
			delete(denied, sub)
		}
		return
	}
	if g.subs[command] == nil {
		g.subs[command] = map[string]bool{}
	}
	g.subs[command][sub] = true
}

func (g *grantState) denySpec(spec string, model dangerModel) {
	command, sub, hasSub := strings.Cut(spec, "|")
	if command == "" || (hasSub && sub == "") {
		return
	}
	if !hasSub {
		delete(g.star, command)
		delete(g.subs, command)
		delete(g.denied, command)
		return
	}
	if g.star[command] {
		if g.denied[command] == nil {
			g.denied[command] = map[string]struct{}{}
		}
		g.denied[command][sub] = struct{}{}
		if len(model.subsByParent[command]) > 0 && coversAll(model.subsByParent[command], g.denied[command]) {
			delete(g.star, command)
			delete(g.denied, command)
		}
		return
	}
	if g.subs[command] == nil {
		g.subs[command] = map[string]bool{}
	}
	g.subs[command][sub] = false
}

func coversAll(want map[string]struct{}, have map[string]struct{}) bool {
	if len(want) == 0 {
		return false
	}
	for sub := range want {
		if _, ok := have[sub]; !ok {
			return false
		}
	}
	return true
}

func (g *grantState) unsafe(model dangerModel) bool {
	for parent := range model.parents {
		if g.star[parent] {
			return true
		}
		if _, whole := model.anySub[parent]; whole {
			for sub, allowed := range g.subs[parent] {
				if !allowed {
					continue
				}
				if _, probe := probeCommandAllowlist[parent+"|"+sub]; probe {
					continue
				}
				return true
			}
		}
	}
	for spec := range model.subs {
		command, sub, _ := strings.Cut(spec, "|")
		if g.star[command] && !containsSub(g.denied[command], sub) {
			return true
		}
		if g.subs[command][sub] {
			return true
		}
	}
	return false
}

func containsSub(denied map[string]struct{}, sub string) bool {
	if denied == nil {
		return false
	}
	_, ok := denied[sub]
	return ok
}

func knownParents(categories map[string][]string) []string {
	seen := make(map[string]struct{})
	for _, members := range categories {
		for _, member := range members {
			command, _, _ := strings.Cut(member, "|")
			if command != "" {
				seen[command] = struct{}{}
			}
		}
	}
	parents := make([]string, 0, len(seen))
	for parent := range seen {
		parents = append(parents, parent)
	}
	return parents
}

func buildDanger(categories map[string][]string) dangerModel {
	model := dangerModel{
		parents:      map[string]struct{}{},
		anySub:       map[string]struct{}{},
		subs:         map[string]struct{}{},
		subsByParent: map[string]map[string]struct{}{},
	}
	for _, parent := range whollyDangerousCommands {
		model.parents[parent] = struct{}{}
		model.anySub[parent] = struct{}{}
	}
	for _, parent := range starUnsafeCommands {
		model.parents[parent] = struct{}{}
	}
	for _, spec := range dangerousSubcommands {
		addDangerousSub(&model, spec)
	}
	for _, member := range categories["scripting"] {
		addReportedMember(&model, member, true)
	}
	// @dangerous is the server's own set. Redis 8 puts TRIMSLOTS and BACKUP
	// here without putting them in @admin or @scripting. Commands this server
	// does not advertise are not added.
	for _, member := range categories["dangerous"] {
		addReportedMember(&model, member, false)
	}
	for _, member := range categories["admin"] {
		if _, allowed := probeCommandAllowlist[member]; allowed {
			continue
		}
		command, _, hasSub := strings.Cut(member, "|")
		if command == "" {
			continue
		}
		model.parents[command] = struct{}{}
		if hasSub {
			addDangerousSub(&model, member)
			continue
		}
		// A bare @admin name makes the whole command unsafe. On Redis 6 that
		// name is the ACL object for every subcommand, and the probe
		// subcommands are still safe on their own.
	}
	return model
}

func addReportedMember(model *dangerModel, member string, anySub bool) {
	member = strings.ToLower(strings.TrimSpace(member))
	if member == "" {
		return
	}
	if _, allowed := probeCommandAllowlist[member]; allowed {
		return
	}
	command, _, hasSub := strings.Cut(member, "|")
	if command == "" {
		return
	}
	model.parents[command] = struct{}{}
	if hasSub {
		addDangerousSub(model, member)
		return
	}
	if anySub {
		model.anySub[command] = struct{}{}
	}
}

func addDangerousSub(model *dangerModel, spec string) {
	command, sub, hasSub := strings.Cut(spec, "|")
	if !hasSub || command == "" || sub == "" {
		return
	}
	if _, allowed := probeCommandAllowlist[spec]; allowed {
		return
	}
	model.parents[command] = struct{}{}
	model.subs[spec] = struct{}{}
	if model.subsByParent[command] == nil {
		model.subsByParent[command] = map[string]struct{}{}
	}
	model.subsByParent[command][sub] = struct{}{}
}

func mergeACLCategories(extra map[string][]string) map[string][]string {
	merged := make(map[string][]string, len(extra)+8)
	for category, members := range builtinACLCategories() {
		merged[category] = append([]string(nil), members...)
	}
	for category, members := range extra {
		category = strings.ToLower(strings.TrimSpace(category))
		if category == "" || category == "all" {
			continue
		}
		seen := make(map[string]struct{}, len(merged[category]))
		for _, existing := range merged[category] {
			seen[existing] = struct{}{}
		}
		for _, member := range members {
			member = strings.ToLower(strings.TrimSpace(member))
			if member == "" {
				continue
			}
			if _, ok := seen[member]; ok {
				continue
			}
			seen[member] = struct{}{}
			merged[category] = append(merged[category], member)
		}
	}
	return merged
}

// probeCommandAllowlist is the introspection this process needs. ACL GETUSER
// cannot be limited to the current user, so the account can also read other
// users' password hashes. The reply is not logged or returned.
var probeCommandAllowlist = map[string]struct{}{
	"acl|whoami":  {},
	"acl|getuser": {},
	"acl|cat":     {},
}

var whollyDangerousCommands = []string{
	"flushall", "flushdb", "swapdb", "migrate",
	"config", "debug", "shutdown",
	"bgrewriteaof", "bgsave", "save",
	"replicaof", "slaveof", "failover",
	"module", "monitor", "sync", "psync", "replconf",
	"latency", "slowlog", "lastsave", "role",
	"pfdebug", "pfselftest", "sentinel",
	"function", "script",
	"eval", "evalsha", "eval_ro", "evalsha_ro", "fcall", "fcall_ro",
	// Keyless name enumeration. A key pattern does not apply. SCAN exists on
	// every supported server. TRIMSLOTS does not, so it is not added to a
	// builtin category; Redis 8 reports it through @dangerous.
	"randomkey", "scan", "trimslots",
}

// starUnsafeCommands are unsafe when every subcommand is granted. A single
// non-purge memory subcommand, such as MEMORY USAGE, stays allowed.
var starUnsafeCommands = []string{"memory"}

var dangerousSubcommands = []string{
	"config|get", "config|set", "config|resetstat", "config|rewrite",
	"acl|deluser", "acl|dryrun", "acl|list", "acl|load", "acl|log", "acl|save", "acl|setuser", "acl|users",
	"cluster|addslots", "cluster|addslotsrange", "cluster|bumpepoch",
	"cluster|delslots", "cluster|delslotsrange", "cluster|failover",
	"cluster|flushslots", "cluster|forget", "cluster|meet",
	"cluster|replicas", "cluster|replicate", "cluster|reset",
	"cluster|saveconfig", "cluster|set-config-epoch", "cluster|setslot",
	"cluster|slaves", "cluster|count-failure-reports",
	"client|kill", "client|list", "client|no-evict", "client|pause", "client|unblock", "client|unpause",
	"function|flush", "function|delete", "function|load", "function|restore", "function|kill",
	"script|flush", "script|debug", "script|kill", "script|load",
	"memory|purge",
}

func builtinACLCategories() map[string][]string {
	admin := []string{
		"bgrewriteaof", "bgsave", "save", "shutdown", "debug", "config",
		"replicaof", "slaveof", "failover", "monitor", "sync", "psync", "replconf",
		"lastsave", "role", "pfdebug", "pfselftest", "sentinel",
		"latency", "slowlog", "module",
		"config|get", "config|set", "config|resetstat", "config|rewrite",
		"acl|deluser", "acl|dryrun", "acl|getuser", "acl|list", "acl|load", "acl|log", "acl|save", "acl|setuser", "acl|users",
		"cluster|addslots", "cluster|addslotsrange", "cluster|bumpepoch",
		"cluster|delslots", "cluster|delslotsrange", "cluster|failover",
		"cluster|flushslots", "cluster|forget", "cluster|meet",
		"cluster|replicas", "cluster|replicate", "cluster|reset",
		"cluster|saveconfig", "cluster|set-config-epoch", "cluster|setslot",
		"cluster|slaves", "cluster|count-failure-reports",
		"client|kill", "client|list", "client|no-evict", "client|pause", "client|unblock", "client|unpause",
	}
	dataDestruction := []string{"flushall", "flushdb", "swapdb", "migrate"}
	dangerous := append(append([]string{}, admin...), dataDestruction...)
	dangerous = append(dangerous, "keys", "info", "restore", "sort")
	return map[string][]string{
		"admin":      admin,
		"dangerous":  dangerous,
		"write":      append([]string{"set", "append"}, dataDestruction...),
		"keyspace":   append([]string{"dbsize", "scan"}, dataDestruction...),
		"slow":       []string{"flushall", "flushdb", "migrate", "shutdown", "debug", "config", "keys", "scan", "save", "bgsave", "bgrewriteaof"},
		"connection": []string{"ping", "auth", "select", "command", "echo", "client|setname", "client|kill", "client|list", "client|no-evict", "client|pause", "client|unblock", "client|unpause"},
		"read":       []string{"get", "scan", "hget", "dbsize"},
		"string":     []string{"get", "set", "append"},
		"fast":       []string{"get", "set", "ping"},
	}
}
