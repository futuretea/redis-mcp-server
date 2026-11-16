# Redis MCP Server

[中文文档](README.zh.md)

Redis MCP Server exposes a closed Redis catalog over MCP. The default catalog is read-only. Write tools are registered only when `redis.write_enabled` is true. The process does not authenticate clients, and output masking stays off until you enable it.

The current release is npm `0.2.0`, container tag `v0.2.0`, and git tag `v0.2.0`. Field lists, errors, and the tool catalog are in the [reference](docs/reference.md). Build and release layout are in [development and release](docs/development.md).

## Install 0.2.0

The npm package and the container image are the program builds. The [v0.2.0 GitHub release](https://github.com/futuretea/redis-mcp-server/releases/tag/v0.2.0) has notes and GitHub's source archive. It does not attach a `redis-mcp-server` binary. Do not look for a binary download on that page.

The npm tarball for 0.2.0 contains the README from that publish. This file is the updated guide. The Chinese guide is [README.zh.md](README.zh.md) in the repository. The npm tarball does not include it.

### npm

Copy [config.example.yaml](config.example.yaml) to the path you will pass to `--config`. The tracked file is the whole configuration. A shorter file with only the `redis` keys is a different document.

```bash
cp config.example.yaml redis-mcp.yaml
export MCP_REDIS_PASSWORD='your-redis-password'
npx -y @futuretea/redis-mcp-server@0.2.0 --config "$PWD/redis-mcp.yaml"
```

`npx` starts `redis-mcp-server mcp`. The process speaks MCP on stdin and stdout. It connects to Redis before it serves. With the example addresses and no Redis on `127.0.0.1:6379`, the process exits non-zero and writes `[redis_connection_failed] connection failed` to stderr. Stdout stays empty.

A stdio client uses the same command. Give the client an absolute configuration path.

```json
{
  "mcpServers": {
    "redis": {
      "command": "npx",
      "args": [
        "-y",
        "@futuretea/redis-mcp-server@0.2.0",
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

The wrapper always inserts the `mcp` subcommand. `npx -y @futuretea/redis-mcp-server@0.2.0 version` does not print the version. It starts `mcp` and then fails if topology is missing. After `npm install @futuretea/redis-mcp-server@0.2.0`, run the platform binary for `version`, `tools`, and `completion`.

| Host | Package | Binary |
| --- | --- | --- |
| macOS Intel | `@futuretea/redis-mcp-server-darwin-amd64` | `bin/redis-mcp-server` |
| macOS Apple Silicon | `@futuretea/redis-mcp-server-darwin-arm64` | `bin/redis-mcp-server` |
| Linux x64 | `@futuretea/redis-mcp-server-linux-amd64` | `bin/redis-mcp-server` |
| Linux ARM64 | `@futuretea/redis-mcp-server-linux-arm64` | `bin/redis-mcp-server` |
| Windows x64 | `@futuretea/redis-mcp-server-windows-amd64` | `bin/redis-mcp-server.exe` |

The linux/amd64 binary from 0.2.0 prints:

```text
redis-mcp-server version=0.2.0 commit=b519d1d9c7430c96abd07eea736d01c3e5f69676 date=2026-11-15T00:38:32-08:00
```

Other platform packages are published at the same version. This check executed the linux/amd64 binary only.

### Docker

The image entrypoint is `/usr/local/bin/redis-mcp-server mcp` and the user is `mcp`. `v0.2.0` contains runnable `linux/amd64` and `linux/arm64` images. The release workflow also tags that image as `latest` until a later stable tag moves `latest`.

Mount the copy of `config.example.yaml`. The repository does not contain `redis-mcp.yaml`.

```bash
cp config.example.yaml redis-mcp.yaml
export MCP_REDIS_PASSWORD='your-redis-password'
docker run --rm -i \
  --env MCP_REDIS_PASSWORD \
  --mount type=bind,src="$PWD/redis-mcp.yaml",dst=/etc/redis-mcp-server/config.yaml,readonly \
  ghcr.io/futuretea/redis-mcp-server:v0.2.0 \
  --config /etc/redis-mcp-server/config.yaml
```

The container waits on stdio the same way `npx` does. To run `version` or `tools`, replace the entrypoint:

```bash
docker run --rm --entrypoint /usr/local/bin/redis-mcp-server \
  ghcr.io/futuretea/redis-mcp-server:v0.2.0 version
```

The container command above was checked against the published image config. It was not executed here because this environment has no Docker daemon.

### Build from a checkout

```bash
make build
./bin/redis-mcp-server version
./bin/redis-mcp-server tools list --config config.example.yaml
```

`tools list` does not connect to Redis. The table includes `redis_string` and omits `redis_string_write` while `write_enabled` is false. The version line from `make build` comes from `git describe` and can differ from `0.2.0`. Go, test, and release details are in [development and release](docs/development.md).

## Configure Redis

Keep the password in `MCP_REDIS_PASSWORD`. Do not add it to YAML. A `password` key in YAML is ignored.

`config.example.yaml` is a valid file. Copy it, then change `redis.addresses`. You can skip the file when the environment supplies topology and addresses:

```bash
export MCP_REDIS_TOPOLOGY=standalone
export MCP_REDIS_ADDRESSES=127.0.0.1:6379
export MCP_REDIS_PASSWORD='your-redis-password'
npx -y @futuretea/redis-mcp-server@0.2.0
```

For Sentinel, also set `MCP_REDIS_SENTINEL_MASTER`. Separate several addresses with a comma. `standalone` uses the first address. `cluster` uses every address as a seed. `sentinel` uses every address as a Sentinel endpoint.

Precedence, TLS, database 0, and the rest of the fields are in the [reference](docs/reference.md).

```yaml
port: 0
listen: 127.0.0.1
sse_base_url: ""
log_level: info
enabled_tools: []
disabled_tools: []
enabled_domains: []
disabled_domains: []
allow_unsafe_database_role: false
redis:
  topology: standalone
  addresses: ["127.0.0.1:6379"]
  username: ""
  tls: false
  sentinel_master: ""
  write_enabled: false
```

The block above is the values in [config.example.yaml](config.example.yaml). The tracked file also has comments for the environment names and for the commented-out `masking` object. Masking stays off while that object is commented out.

## Call a tool

`tools describe` prints the schema and does not connect.

```bash
./bin/redis-mcp-server tools describe redis_string --config config.example.yaml
./bin/redis-mcp-server tools call redis_string --config config.example.yaml \
  --params '{"operation":"get","args":{"key":"sample"}}'
```

`tools call` connects. With no Redis at the example address, it exits non-zero and writes `[redis_connection_failed] connection failed` to stderr.

Write tools appear only after `redis.write_enabled` is true or `MCP_REDIS_WRITE_ENABLED=true`. Enabling writes registers delete and trim operations. It is not a per-call approval step. Leave it false for a general-purpose client.

`enabled_tools` selects whole tools. Enabling `redis_string` exposes every read operation of that tool, not one operation. Leave `redis_key` and `redis_observe` out of the list unless the client needs key names or keyspace metadata. `RANDOMKEY` and `SCAN` return key names from the whole keyspace.

Use a Redis ACL account that can run the catalog and the startup probe, and that cannot run admin, flush, script, or keyless enumeration commands. The rejection list and the account shapes used by the startup test are in the [reference](docs/reference.md). `allow_unsafe_database_role` defaults to false. Set it only when you cannot create that account and you control the Redis instance.

## Choose stdio or HTTP

Prefer stdio for a local client. The `npx` and `docker run` commands above are stdio.

For HTTP, set a port. The default listen address is `127.0.0.1`.

```bash
./bin/redis-mcp-server mcp --config redis-mcp.yaml --port 8080
```

Streamable HTTP is `http://127.0.0.1:8080/mcp`. SSE is `http://127.0.0.1:8080/sse`. `GET http://127.0.0.1:8080/healthz` returns the body `healthy` after startup. `HEAD` returns `200` and no body. The process does not check an `Authorization` header.

Inside a container, `127.0.0.1` is the container loopback. To publish the port, set `--listen 0.0.0.0` and publish it only on a network that should reach an unauthenticated process.

```bash
docker run --rm -i \
  -p 127.0.0.1:8080:8080 \
  --env MCP_REDIS_PASSWORD \
  --mount type=bind,src="$PWD/redis-mcp.yaml",dst=/etc/redis-mcp-server/config.yaml,readonly \
  ghcr.io/futuretea/redis-mcp-server:v0.2.0 \
  --config /etc/redis-mcp-server/config.yaml \
  --port 8080 \
  --listen 0.0.0.0
```

Put TLS and authentication in front of any listener that other machines can reach.

## Enable masking

Masking is off in the example file. To turn it on, set `enabled` to true and at least one rule. `MCP_MASKING_JSON` replaces the whole YAML object when the variable is set.

```bash
export MCP_MASKING_JSON='{"enabled":true,"names":["synthetic_field"],"values":["synthetic-token-value"]}'
```

Names, patterns, placeholder, limits, and the Redis cases that fail closed are in the [reference](docs/reference.md). Masking does not find every private value.
