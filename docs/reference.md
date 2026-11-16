# Redis MCP Server reference

[中文参考](reference.zh.md)

This page lists the settings, tools, errors, and limits of redis-mcp-server 0.2.0. Install steps are in the [English guide](../README.md).

## Configuration order

The process loads values in this order. A later source replaces one field from an earlier source.

1. Built-in defaults.
2. The YAML file passed to `--config`, when you set that flag.
3. `MCP_` environment variables.
4. An explicit flag.

Omitting a flag leaves the file and environment values in place. The flag default does not clear them.

`tools` binds `--config`, `--enabled-tools`, `--disabled-tools`, `--enable-domains`, and `--disable-domains`. `mcp` also binds `--port`, `--listen`, `--log-level`, and `--sse-base-url`.

There is no Redis URL. The server does not read `redis://` or `rediss://`.

A `password` field in YAML is ignored. The password is read from `MCP_REDIS_PASSWORD` when a client is constructed. The configuration struct does not store it. An unset variable is an empty password.

`MCP_MASKING_JSON` is not a single field override. `LoadConfig` does the replacement. When the variable is present, that string is parsed and the YAML `masking` object is not used. `ParseJSON` only parses the text it is given. It does not read the environment. A blank value fails with `invalid masking config`. The CLI prints `load config: invalid masking config`. When the variable is unset, the YAML object is used. Commented-out YAML is the same as an absent object.

List environment values are split on commas, and each item is trimmed. A space does not separate items. `MCP_REDIS_ADDRESSES=127.0.0.1:6379,10.0.0.2:6379` and `MCP_REDIS_ADDRESSES=127.0.0.1:6379, 10.0.0.2:6379` are two addresses. `MCP_REDIS_ADDRESSES=127.0.0.1:1 127.0.0.1:2` is one address. Standalone dials that whole string. The process then reports `dial tcp: address 127.0.0.1:1 127.0.0.1:2: too many colons in address` and `[redis_connection_failed] connection failed`. `MCP_ENABLED_TOOLS=redis_string, redis_hash` enables those two tools. `MCP_ENABLED_TOOLS=redis_string redis_hash` is one name, matches no tool, and `tools list` prints only the header. The same comma split applies to `MCP_DISABLED_TOOLS`, `MCP_ENABLED_DOMAINS`, and `MCP_DISABLED_DOMAINS`. `--enabled-tools redis_string,redis_hash` is two tools. One argument `redis_string redis_hash` registers none.

A boolean field accepts a YAML boolean, or a string from the set `1`, `t`, `T`, `TRUE`, `true`, `True`, `0`, `f`, `F`, `FALSE`, `false`, and `False`. The 0.2.0 program treated those values of `MCP_REDIS_WRITE_ENABLED` as booleans. `yes`, `on`, `y`, `n`, `no`, and `off` are rejected. The CLI prints `load config: invalid config`. The same rejection happens for YAML `yes` or `on`. A quoted YAML string `"true"` uses that accepted set. An empty `MCP_REDIS_WRITE_ENABLED` left YAML `write_enabled: true` in place. `MCP_PORT` is an integer.

## Defaults

| Field | Default | Environment | Flag |
| --- | --- | --- | --- |
| `port` | `0` | `MCP_PORT` | `mcp --port` |
| `listen` | `127.0.0.1` | `MCP_LISTEN` | `mcp --listen` |
| `sse_base_url` | empty | `MCP_SSE_BASE_URL` | `mcp --sse-base-url` |
| `log_level` | `info` | `MCP_LOG_LEVEL` | `mcp --log-level` |
| `enabled_tools` | empty | `MCP_ENABLED_TOOLS` | `--enabled-tools` |
| `disabled_tools` | empty | `MCP_DISABLED_TOOLS` | `--disabled-tools` |
| `enabled_domains` | empty | `MCP_ENABLED_DOMAINS` | `--enable-domains` |
| `disabled_domains` | empty | `MCP_DISABLED_DOMAINS` | `--disable-domains` |
| `allow_unsafe_database_role` | `false` | `MCP_ALLOW_UNSAFE_DATABASE_ROLE` | none |
| `redis.topology` | empty, and empty is rejected | `MCP_REDIS_TOPOLOGY` | none |
| `redis.addresses` | empty, and empty is rejected | `MCP_REDIS_ADDRESSES` | none |
| `redis.username` | empty | `MCP_REDIS_USERNAME` | none |
| `redis.tls` | `false` | `MCP_REDIS_TLS` | none |
| `redis.sentinel_master` | empty | `MCP_REDIS_SENTINEL_MASTER` | none |
| `redis.write_enabled` | `false` | `MCP_REDIS_WRITE_ENABLED` | none |

`log_level` is `trace`, `debug`, `info`, `warn`, `error`, `fatal`, `panic`, or `disabled`. Any other value fails with `invalid log_level`.

`port` must be from 0 through 65535. `0` selects stdio and does not open a listening socket. A non-zero port requires a non-empty `listen` value.

Tool names in flags are comma-separated. `--enabled-tools redis_string,redis_hash` enables those two tools.

The only tool domain is `redis`. An `enabled_domains` list that does not contain `redis` removes every tool. `tools list` then prints the table header and exits 0. `mcp` refuses to start and reports `no tools registered; check enabled_tools / disabled_tools / domain filters`.

When `enabled_tools` is non-empty, a tool must be in that list. `disabled_tools` then removes matches. A name that is in both lists is not registered.

## Redis topology

`redis.topology` is trimmed and lowercased. A missing, empty, or whitespace-only value fails with `load config: redis.topology must be explicitly set to standalone, cluster, or sentinel`. A non-empty value other than `standalone`, `cluster`, or `sentinel`, such as `nope`, fails with `load config: redis.topology must be standalone, cluster, or sentinel`.

`standalone` dials `redis.addresses` element 0. Later addresses are ignored.

`cluster` uses every address as a cluster seed.

`sentinel` uses every address as a Sentinel endpoint and requires `redis.sentinel_master`. The same username and password are sent to Sentinel and to the data node. There is no separate Sentinel password field. `sentinel_master` is stored for the other topologies and is not used by them.

`redis.tls` true uses TLS 1.2 or newer and the default system roots. The client does not load a custom CA, a client certificate, or `InsecureSkipVerify`.

There is no database index setting. The client uses database 0.

Command retries are disabled with `MaxRetries` set to -1. A refused TCP address still produces a Redis pool dial line on stderr, then the process error `[redis_connection_failed] connection failed`.

## Tool catalog

The catalog is closed. A caller cannot pass a Redis command name. There is no raw-command tool and no `redis_observe_write`.

With `redis.write_enabled` false, the registered tools are `redis_key`, `redis_string`, `redis_hash`, `redis_list`, `redis_set`, `redis_sorted_set`, `redis_stream`, and `redis_observe`.

With `redis.write_enabled` true, these write tools are added: `redis_key_write`, `redis_string_write`, `redis_hash_write`, `redis_list_write`, `redis_set_write`, `redis_sorted_set_write`, and `redis_stream_write`.

`tools list` and `tools describe` load the configuration and do not connect to Redis. `tools call` and `mcp` connect, run the ACL probe, and then dispatch.

`tools describe redis_string --config config.example.yaml` prints the input schema. `--json` prints the list or the schema as JSON.

A call body is one object:

```json
{"operation":"get","args":{"key":"sample"}}
```

`tools call` reads that object from `--params` or from `--params-file`. `--params-file -` reads stdin. The default `--params` value is `{}`. `null` is rejected. With masking off, `tools describe` and `tools call` for a missing name print `unknown tool "no-such-tool"; run 'redis-mcp-server tools list' to see available tools`, using the name you passed. With masking on, both commands print `unknown tool` and omit the name. Bad JSON for `tools call` is then `invalid tool params`. An MCP `tools/call` for a name that is not registered is rejected by the MCP library before the Redis handler. The JSON-RPC code is invalid params. The message is `tool '<name>' not found: tool not found`, and it includes the name. Masking does not change that protocol message.

Read operations:

| Tool | Operations |
| --- | --- |
| `redis_key` | `exists`, `random_key`, `type`, `expiry`, `dump`, `object_attribute` |
| `redis_string` | `get`, `get_range`, `get_many`, `length`, `digest`, `longest_common_substring` |
| `redis_hash` | `field_exists`, `get`, `length`, `get_many`, `field_expiry`, `random_field`, `scan`, `value_length` |
| `redis_list` | `index`, `length`, `position`, `range` |
| `redis_set` | `cardinality`, `intersection_cardinality`, `member_exists`, `members_exist`, `random_members`, `scan` |
| `redis_sorted_set` | `cardinality`, `count_by_score`, `intersection_cardinality`, `lex_count`, `scores`, `random_members`, `range`, `rank`, `scan`, `score` |
| `redis_stream` | `length`, `pending` |
| `redis_observe` | `scan`, `db_size`, `memory_usage` |

Write operations:

| Tool | Operations |
| --- | --- |
| `redis_key_write` | `copy`, `delete`, `set_expiry`, `persist_expiry`, `rename`, `restore`, `touch` |
| `redis_string_write` | `append`, `decrement`, `delete_if`, `get_and_delete`, `get_and_set_expiry`, `get_and_set`, `increment`, `set_many`, `set`, `set_range` |
| `redis_hash_write` | `delete_fields`, `set_field_expiry`, `persist_field_expiry`, `get_and_delete`, `get_and_set_expiry`, `increment`, `set_fields`, `set_fields_with_expiry`, `set_if_absent` |
| `redis_list_write` | `insert`, `move`, `pop`, `pop_many`, `push`, `push_if_exists`, `remove`, `set`, `trim` |
| `redis_set_write` | `add`, `store_difference`, `store_intersection`, `move`, `pop`, `remove`, `store_union` |
| `redis_sorted_set_write` | `add`, `store_difference`, `increment`, `store_intersection`, `pop`, `remove`, `remove_range`, `store_range`, `store_union` |
| `redis_stream_write` | `group`, `ack`, `add`, `claim`, `delete`, `nack`, `trim` |

`redis_stream` does not read stream entries. `redis_stream_write` `claim` requires `justid` to be true and calls `XCLAIM JUSTID`.

Array arguments contain 1 through 1000 items. Scan, range, and pending counts that the contract requires are integers from 1 through 1000, checked before the Redis command. A scan response returns at most that many items. Extra values from one Redis reply stay in the continuation and are not returned in that response.

`SCAN`, `HSCAN`, `SSCAN`, and `ZSCAN` continuations are 22-character base64url tokens. The pattern is `^[A-Za-z0-9_-]{22}$`. The token is not the Redis cursor. A successful page keeps the session idle-valid for five minutes. One adapter holds at most 1,000 sessions and 16 MiB of encoded state. Eviction drops a whole session by least recent use. An expired or evicted token returns category `cursor_expired`. A token that cannot be admitted returns category `cursor_unavailable`. A token from a different key, match, type, or `no_values` value returns `cursor does not match this scan`.

On a cluster client, `redis_observe` `db_size` sums `DBSIZE` across current primaries and reports `primary_count`. Other topologies report one server.

Binary input is an object with `utf8` or an object with `base64`, not both in the published schema. If a caller sends both keys anyway, the adapter uses `utf8` and ignores `base64`. Binary output always includes standard `base64`. It includes `utf8` only when the bytes are valid UTF-8.

A successful call is encoded as text, not as an MCP structured object:

```json
{"status":"ok","operation":"get","data":{}}
```

`data` holds the operation fields. A missing Redis value is `{"status":"not_found","operation":"get"}`. A failed write condition is `{"status":"condition_not_met","operation":"set"}`. Any other tool failure is:

```json
{"status":"error","operation":"get","error":{"category":"unavailable","message":"Redis operation failed"}}
```

Redis reply text is not copied into that message. The categories include `invalid_argument`, `unavailable`, `not_found`, `condition_not_met`, `deadline`, `deadline_outcome_unknown`, `wrong_type`, `auth_acl`, `cross_slot`, `cursor_expired`, `cursor_unavailable`, and `topology_changed`.

`WRONGTYPE` becomes `wrong_type`. `NOAUTH` and `NOPERM` become `auth_acl`. `CROSSSLOT` becomes `cross_slot`. A reply that starts with `ERR ` becomes `invalid_argument` and the message `Redis rejected an operation argument`. A write that hits its deadline uses `deadline_outcome_unknown` because the write may have landed. A read deadline uses `deadline`.

## Startup ACL check

`tools call` and `mcp` run this check after the connection succeeds and before any tool command. The probe commands are `PING`, `ACL WHOAMI`, `ACL GETUSER`, and `ACL CAT`. The account must be allowed to run them. A failed probe returns `[redis_privilege_probe_failed] Redis ACL probe failed`. `allow_unsafe_database_role` does not skip a failed probe or a refused connection.

`ACL GETUSER` cannot be limited to the current user, so this account can read every user's ACL password hash. The process calls it only for the authenticated user and does not log or return that reply.

The check rejects the account when any root rule or any selector grants any of these:

- `flushall`, `flushdb`, `swapdb`, `migrate`, `config`, `debug`, `shutdown`, `bgrewriteaof`, `bgsave`, `save`, `replicaof`, `slaveof`, `failover`, `module`, `monitor`, `sync`, `psync`, `replconf`, `latency`, `slowlog`, `lastsave`, `role`, `pfdebug`, `pfselftest`, `sentinel`, `function`, `script`, `eval`, `evalsha`, `eval_ro`, `evalsha_ro`, `fcall`, `fcall_ro`, `randomkey`, `scan`, or `trimslots`
- the whole `memory` command, or `memory|purge`
- `config|get`, `config|set`, `config|resetstat`, `config|rewrite`
- `acl|deluser`, `acl|dryrun`, `acl|list`, `acl|load`, `acl|log`, `acl|save`, `acl|setuser`, `acl|users`
- `cluster|addslots`, `cluster|addslotsrange`, `cluster|bumpepoch`, `cluster|delslots`, `cluster|delslotsrange`, `cluster|failover`, `cluster|flushslots`, `cluster|forget`, `cluster|meet`, `cluster|replicas`, `cluster|replicate`, `cluster|reset`, `cluster|saveconfig`, `cluster|set-config-epoch`, `cluster|setslot`, `cluster|slaves`, `cluster|count-failure-reports`
- `client|kill`, `client|list`, `client|no-evict`, `client|pause`, `client|unblock`, `client|unpause`
- `function|flush`, `function|delete`, `function|load`, `function|restore`, `function|kill`
- `script|flush`, `script|debug`, `script|kill`, `script|load`

It also rejects commands the connected server reports in `@admin`, `@dangerous`, or `@scripting`, except the probe commands `acl|whoami`, `acl|getuser`, and `acl|cat`. The checker merges a built-in category set before that reply. The built-in `@dangerous` set includes `keys`, `info`, `restore`, and `sort`, so those names are rejected even before the server list is added.

These are not rejections by themselves: `acl|whoami`, `acl|getuser`, `acl|cat`, `cluster|slots`, `cluster|nodes`, `cluster|shards`, `memory|usage`, and `client|setname`. Granting the whole `ACL`, `CLIENT`, `CLUSTER`, `FUNCTION`, `SCRIPT`, or `MEMORY` command is a rejection because that grant includes a dangerous subcommand.

A key pattern does not apply to `RANDOMKEY` or `SCAN`. `redis_key` `random_key` calls `RANDOMKEY`. `redis_observe` `scan` calls `SCAN`. An account that revokes those commands cannot use those operations. `DBSIZE` still counts keys outside the pattern.

The rejection text is `[redis_unsafe_service_role] database service role is not permitted`.

`allow_unsafe_database_role` true, or `MCP_ALLOW_UNSAFE_DATABASE_ROLE=true`, skips only that rejection. The process logs `database service role is not permitted; allow_unsafe_database_role is set, so the startup role check is skipped`. The Redis user can still run `FLUSHALL`, `CONFIG`, `DEBUG`, or `SHUTDOWN` outside this catalog. Set the option only for an instance you control. Do not use the Redis `default` user for normal operation. A normal server grants that user every command, and the check rejects it unless the option is set.

`mcp` with `port` 0 disables zerolog before the check, so that warning is not written by zerolog. `mcp` with a non-zero port writes zerolog to stderr at `log_level`. The default `info` level includes `warn`. `tools call` does not disable zerolog. The Redis client dial line is separate from zerolog and can still appear on stderr.

When the live startup test sees major version 7 or greater, the reader account is `-@all +@read -@admin -@dangerous -randomkey -scan` plus `+ping +acl|whoami +acl|getuser +acl|cat`. That shape does not revoke `memory`. The app account in that same branch grants the data categories, then revokes `@admin`, `@dangerous`, `@scripting`, `flushall`, `flushdb`, `swapdb`, `migrate`, `memory|purge`, `randomkey`, and `scan`, and adds `cluster|slots`, `cluster|nodes`, and `cluster|shards`. When the reported major version is below 7, the test source revokes the whole `memory` command on both the reader account and the app account, and the app account does not add those cluster subcommands. The repository workflows do not set `REDIS_MATRIX_BINARIES`, so `TestVerifyStartupRedisVersionMatrix` does not run. `make ci` runs `make test`, and `make test` runs `go test ./...`. That command includes `TestVerifyStartupAgainstLocalRedis`. The local test calls the same `exerciseStartupRedis` helper when `redis-server` is on `PATH`, and it skips when that binary is absent. These pages do not use CI logs to say whether a runner had that binary or which version it was. The below-7 branch is source in that helper. It is selected only when the started `redis-server` reports a major version below 7.

`TRIMSLOTS` is in the hardcoded rejection list. The startup comment records that Redis 8 also places `TRIMSLOTS` and `BACKUP` in `@dangerous`, and that `@write` and `@keyspace` grant `TRIMSLOTS`. `BACKUP` is rejected when the connected server reports it in `@dangerous`. It is not in the hardcoded name list.

## Output masking

Masking is off when the `masking` object is absent, when `enabled` is omitted, or when `enabled` is false. Results, CLI text, and errors then keep their current JSON. `enabled` true with no names, name patterns, values, or value patterns fails startup with `invalid masking config`. The error does not include the document.

```yaml
masking:
  enabled: true
  placeholder: "[masked]"
  names: ["synthetic_field"]
  name_patterns: ["token_.+"]
  values: ["synthetic-token-value"]
  value_patterns: ["sk_live_[0-9]+"]
```

```bash
export MCP_MASKING_JSON='{"enabled":true,"names":["synthetic_field"],"values":["synthetic-token-value"]}'
```

Unknown JSON fields are rejected. The placeholder defaults to `[masked]`, must be non-empty, and must be at most 64 bytes. Each of the four rule lists holds at most 32 entries. A name is at most 128 bytes. A value literal is at most 256 bytes. A pattern is at most 128 bytes. Empty entries are rejected.

Name patterns are parsed on their own and then anchored to the whole name. `a)|(b` is rejected. `(?:a)|(b)` matches only the whole names `a` and `b`. Value rules are substring matches, including a one-character literal, and they run once. The placeholder is not searched again. A pattern that can match empty input, including a word-boundary branch, is rejected. RE2 returns the leftmost-first match, so `sk_|sk_live_[0-9]+` replaces `sk_` and does not then search for the longer branch.

These caps apply only when masking is on. One result shares a budget of 10,000 nodes, 1 MiB, 4,096 spans, JSON depth 32, and two JSON parse rounds. The 1 MiB cap counts visited input. It also rejects a replaced string, or a re-encoded JSON document, longer than 1 MiB. A base64 cell longer than 1 MiB is rejected before decode. Crossing a cap fails the whole result. There is no partial payload. Masking off does not apply this budget.

A matched string becomes the placeholder. A matched number, bool, or other non-string becomes JSON `null`. An original null stays null. A JSON value is counted again as its container, so it can exhaust the budget before a plain string of the same size.

Redis binary cells are masked from the original bytes. The result writes `base64` again, and writes `utf8` again when the masked bytes are valid UTF-8. A `utf8` and `base64` pair that disagree is rejected. Invalid UTF-8 stays in `base64` only. JSON objects and arrays stored as text are masked as JSON, including escaped keys.

`redis_key` `dump` is opaque. With an active policy the call returns `[redis_masking_failed] result was not returned` and no payload. With masking off, `DUMP` is returned as a binary object. `metadata.next_cursor` is the continuation token. If a rule matches that token, the page fails instead of returning a raw or rewritten cursor. Two output keys that mask to the same key fail the same way.

When masking is on, a tool message that contains a double quote is replaced, except for a fixed message list in the Redis toolset. `invalid_argument` then becomes `Redis rejected an operation argument`. Other quoted messages become `[redis_masking_failed] result was not returned`. The Redis command and its arguments are not rewritten.

This does not detect every private value, compressed payload, unknown encoding, or arbitrary personal data.

## HTTP

`port` 0 uses stdio. MCP messages are stdout. Process errors are stderr.

A non-zero port serves these paths and does not check credentials, cookies, or an `Authorization` header:

| Path | Role |
| --- | --- |
| `/mcp` | stateless streamable HTTP |
| `/sse` | SSE |
| `/message` | SSE messages, with the request query appended to the endpoint |
| `/healthz` | After tools are registered, `GET` returns `200` and the body `healthy`. `HEAD` returns `200`, an empty body, and `Content-Length` 7. Other methods return `405` and no body. |

`mcp` does not open the port when no tools are registered. A `GET` to the handler is `503` and the body `unhealthy` when the server has no tools. The repository test checks `HEAD` only for a server that has tools.

An empty `sse_base_url` leaves the SSE base URL to the request host. A non-empty value is the public base URL for SSE message endpoints.

The server allows 5 seconds to read headers, 30 seconds to read a request, and 120 seconds of idle time. The write timeout is 0 so an open SSE stream is not cut off from the header time. Shutdown waits 9 seconds.

Debug logs record `mcp`, `sse`, `message`, or `other`. They do not record the raw path or the query. `/healthz` is not logged.

`listen` defaults to loopback. `0.0.0.0` accepts every interface of the host. Put TLS, authentication, and network policy in front of any HTTP listener that other machines can reach. This process does not provide them.

## Versions

The module and the CI workflows use Go 1.25.10. That is the language version in `go.mod`. The workflows do not build with another Go version.

`make integration` uses the Redis image pinned in [testdata/redis/images.lock](../testdata/redis/images.lock), which is Redis 8.8.1, for standalone, cluster, and Sentinel. The default CI command does not run a Redis 6 or Redis 7 matrix. `TestVerifyStartupRedisVersionMatrix` runs only when `REDIS_MATRIX_BINARIES` is set. The workflows do not set it.

Startup requires `PING` and the ACL probe commands. The source does not declare a minimum Redis version. Some operations call commands that older servers do not implement, including `DIGEST`, hash field expiry, and stream commands such as `XNACK`. Those calls use the normal tool error path when Redis rejects them. The integration image is Redis 8.8.1.

The npm publish job uses Node.js 22. The wrapper `package.json` does not set `engines`.
