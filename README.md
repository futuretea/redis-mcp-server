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

The catalog contains `redis_key`, `redis_string`, `redis_hash`, `redis_list`, `redis_set`, `redis_sorted_set`, `redis_stream`, and `redis_observe`. Write-mode additionally registers the corresponding seven `*_write` tools; there is no raw-command tool and no `redis_observe_write`.

Collection responses are bounded to 1–1000 items before reaching Redis. `SCAN`, `HSCAN`, `SSCAN`, and `ZSCAN` return a fixed-length opaque base64url continuation reference, never a Redis-native cursor or buffered value. A continuation remains idle-valid for five minutes after a successful page, with at most 1,000 sessions and 16 MiB of encoded state per adapter; whole sessions are evicted by LRU. Expired or evicted references return `cursor_expired`; a newly unadmittable continuation returns `cursor_unavailable`. Stream entry reads are intentionally absent: the stream catalog is limited to length, bounded pending inspection, and `XCLAIM JUSTID`.

## Configuration

Redis topology is explicit. Password is read only from `MCP_REDIS_PASSWORD`; do not place it in YAML.

```yaml
redis:
  topology: standalone # standalone | cluster | sentinel
  addresses: ["127.0.0.1:6379"]
  username: ""
  tls: false
  sentinel_master: ""
  write_enabled: false
```

Use `sentinel_master` for Sentinel. `addresses` lists cluster seeds for Cluster and Sentinel endpoints for Sentinel.

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
