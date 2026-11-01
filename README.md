# Redis MCP Server

[中文文档](README.zh.md)

Redis MCP Server exposes a closed, semantic Redis data-plane catalog over MCP and the bundled CLI. It supports explicit standalone, Cluster, and Sentinel configurations. Tools are read-only by default; write tools are registered only when `redis.write_enabled=true`.

## Quick start

```bash
make build
./bin/redis-mcp-server tools list --config config.example.yaml
./bin/redis-mcp-server tools describe redis_string --config config.example.yaml
./bin/redis-mcp-server tools call redis_string --config config.example.yaml \
  --params '{"operation":"get","args":{"key":"sample"}}'
```

`tools list` and `tools describe` do not connect to Redis. `tools call` and `mcp` connect, then reject an account that can run administrative commands unless `allow_unsafe_database_role` is set. See Service account.

The catalog contains `redis_key`, `redis_string`, `redis_hash`, `redis_list`, `redis_set`, `redis_sorted_set`, `redis_stream`, and `redis_observe`. Write-mode additionally registers the corresponding seven `*_write` tools; there is no raw-command tool and no `redis_observe_write`.

Collection responses are bounded to 1–1000 items before reaching Redis. `SCAN`, `HSCAN`, `SSCAN`, and `ZSCAN` return a fixed-length opaque base64url continuation reference, never a Redis-native cursor or buffered value. A continuation remains idle-valid for five minutes after a successful page, with at most 1,000 sessions and 16 MiB of encoded state per adapter; whole sessions are evicted by LRU. Expired or evicted references return `cursor_expired`; a newly unadmittable continuation returns `cursor_unavailable`. Stream entry reads are intentionally absent: the stream catalog is limited to length, bounded pending inspection, and `XCLAIM JUSTID`.

## Use a release

Stable releases use tags in the form `vX.Y.Z`. npm uses `X.Y.Z`; Docker uses `vX.Y.Z`. npm packages support macOS (Intel and Apple Silicon), Linux (x64 and ARM64), and Windows x64. Docker images support Linux `amd64` and `arm64`.

### npm

```bash
export MCP_REDIS_PASSWORD='your-redis-password'
npx -y @futuretea/redis-mcp-server@X.Y.Z --config /path/to/redis-mcp.yaml
```

Configure an MCP client that supports stdio with the same version and configuration path:

```json
{
  "mcpServers": {
    "redis": {
      "command": "npx",
      "args": [
        "-y",
        "@futuretea/redis-mcp-server@X.Y.Z",
        "--config",
        "/absolute/path/to/redis-mcp.yaml"
      ],
      "env": {
        "MCP_REDIS_PASSWORD": "your-redis-password"
      }
    }
  }
}
```

### Docker

Mount the Redis configuration file read-only and keep the password in the runtime environment:

```bash
export MCP_REDIS_PASSWORD='your-redis-password'
docker run --rm -i \
  --env MCP_REDIS_PASSWORD \
  --mount type=bind,src="$PWD/redis-mcp.yaml",dst=/etc/redis-mcp-server/config.yaml,readonly \
  ghcr.io/futuretea/redis-mcp-server:vX.Y.Z \
  --config /etc/redis-mcp-server/config.yaml
```

Both release entry points start MCP over stdio by default. Pass `--port 8080 --listen 0.0.0.0` after the release command to start HTTP in a container, and publish the port only to the network that should reach it.

## Configuration

Redis topology is explicit. Password is read only from `MCP_REDIS_PASSWORD`; do not place it in YAML.

```yaml
allow_unsafe_database_role: false
redis:
  topology: standalone # standalone | cluster | sentinel
  addresses: ["127.0.0.1:6379"]
  username: ""
  tls: false
  sentinel_master: ""
  write_enabled: false
```

`allow_unsafe_database_role` defaults to false. The environment variable is `MCP_ALLOW_UNSAFE_DATABASE_ROLE`. Password remains `MCP_REDIS_PASSWORD` only.

Use `sentinel_master` for Sentinel. `addresses` lists cluster seeds for Cluster and Sentinel endpoints for Sentinel.

### Service account

After the process connects, and before any tool runs a Redis command, startup reads the current user's ACL. It sends `PING`, `ACL WHOAMI`, `ACL GETUSER`, and `ACL CAT`. The process exits with `[redis_unsafe_service_role] database service role is not permitted` when any root rule or selector can run:

- a command in the `@admin` category, including `CONFIG`, `DEBUG`, `SHUTDOWN`, `ACL SETUSER`, `BGSAVE`, `REPLICAOF`, `MODULE`, `MONITOR`, `CLIENT KILL`, and cluster administration subcommands such as `CLUSTER FAILOVER` and `CLUSTER RESET`
- `FLUSHALL`, `FLUSHDB`, `SWAPDB`, or `MIGRATE`
- the whole `ACL`, `CLIENT`, or `CLUSTER` command

These probe commands are not a rejection by themselves: `ACL WHOAMI`, `ACL GETUSER`, and `ACL CAT`. Cluster discovery commands such as `CLUSTER SLOTS`, `CLUSTER NODES`, and `CLUSTER SHARDS` are not a rejection either. The account must be allowed to run the probe, or startup fails with `[redis_privilege_probe_failed]` even when the opt-out is set. A refused connection fails with `[redis_connection_failed]` and is not skipped by the opt-out.

An account limited to the read commands it needs, plus the probe commands, starts with the default configuration. No opt-out is required. The same is true for a business account that grants read and write categories and then revokes `@admin`, `@dangerous`, `FLUSHALL`, `FLUSHDB`, `SWAPDB`, and `MIGRATE`. `+@write` includes `FLUSHALL`. `+@connection` includes `CLIENT KILL`. Grant the broad category first, then revoke the dangerous commands, then add the probe commands back.

```redis
ACL SETUSER mcp-reader on >strong-password ~app:* resetchannels &
  -@all
  +@read
  -@admin
  -@dangerous
  +ping
  +acl|whoami
  +acl|getuser
  +acl|cat
```

`allow_unsafe_database_role` defaults to `false`. Set it to `true` in the config file, or export `MCP_ALLOW_UNSAFE_DATABASE_ROLE=true`, only when you cannot provision a least-privilege account. The server logs a warning and skips this privilege rejection. A refused connection, a failed ACL probe, and every other startup check still stop startup. The Redis user can still run `FLUSHALL`, `CONFIG`, `DEBUG`, or `SHUTDOWN` outside the tool catalog: this server does not expose a raw command, but the account itself can. Enable the option only for a Redis instance you control.

The warning is logged at warn level. HTTP mode shows it at the default `info` log level. Stdio mode keeps logs disabled so MCP output stays intact. Do not use the Redis `default` user. A standard server grants that user `+@all`.

## Production safety guide

This server reduces the Redis capabilities exposed to an AI client; it is not an authentication gateway or a complete data-access policy. Read-only mode prevents mutations, but reads can still disclose data or consume Redis capacity.

### Keep writes disabled by default

Keep `redis.write_enabled: false` for untrusted or general-purpose AI clients. Enabling it registers mutation tools, including key deletion and collection trimming; it is not a per-request approval mode.

### Expose the smallest useful tool set

Use `enabled_tools` to expose only the tool families required by the client. Tool filtering is at the tool level, not the operation level: enabling `redis_string`, for example, exposes all of that tool's declared read operations. Do not expose `redis_key` or `redis_observe` to an untrusted client unless key discovery and keyspace metadata are required.

```yaml
enabled_tools:
  - redis_string
  - redis_hash
disabled_tools:
  - redis_key
  - redis_observe
```

### Restrict the Redis account and keyspace

Connect with a dedicated Redis ACL account. Grant it only the commands and key patterns needed by the selected tools; do not use an administrator or application-owner account. Startup rejects `@admin`, `FLUSHALL`, `FLUSHDB`, `CONFIG`, `DEBUG`, `SHUTDOWN`, and the other administrative commands listed under Service account unless `allow_unsafe_database_role` is set. A read-only Redis account can still disclose every key it can read, so keep secrets and unrelated tenant data outside its permitted key patterns.

### Put HTTP and SSE behind an authenticated boundary

Prefer stdio for local MCP clients. When using HTTP or SSE, place the server behind TLS, fail-closed authentication, network isolation, and request rate/concurrency limits. Do not expose the process directly to an untrusted network.

### Bound enumeration and verify the deployed catalog

`SCAN`-based operations avoid the blocking `KEYS` command and each collection page is bounded, but repeated requests can still enumerate a large keyspace. Apply gateway quotas and monitoring for request rate, scan volume, latency, and errors. Before deployment, inspect the actual catalog:

```bash
./bin/redis-mcp-server tools list --config production.yaml
```

Confirm that no `*_write` tool is registered and that every listed tool is intentional. Also verify that the deployment boundary rejects unauthenticated requests and that the Redis ACL denies keys outside the intended scope.

## Transports and deployment

```bash
./bin/redis-mcp-server mcp --config config.example.yaml
./bin/redis-mcp-server mcp --config config.example.yaml --port 8080
```

Streamable HTTP and SSE remain available. If HTTP/SSE is publicly exposed, an external fail-closed authentication boundary must reject unauthenticated requests before they reach this process. This repository does not implement authentication, gateway integration, or authentication-header authorization.

## Development

```bash
make test
make lint
make build
make integration
```

`make integration` uses the pinned Redis 8.8.1 image in `testdata/redis/images.lock` to start standalone, Cluster, and Sentinel test topologies. Docker, image, network, compose, or integration-test failures make the target fail.
