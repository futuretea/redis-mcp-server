# Redis MCP Server

[English](README.md)

Redis MCP Server 通过 MCP 和 CLI 提供封闭的 Redis 语义数据面工具。支持显式 standalone、Cluster、Sentinel 配置。默认只注册读取工具；只有 `redis.write_enabled=true` 才注册写工具。

## 快速开始

```bash
make build
./bin/redis-mcp-server tools list --config config.example.yaml
./bin/redis-mcp-server tools describe redis_string --config config.example.yaml
./bin/redis-mcp-server tools call redis_string --config config.example.yaml \
  --params '{"operation":"get","args":{"key":"sample"}}'
```

读取目录包含 `redis_key`、`redis_string`、`redis_hash`、`redis_list`、`redis_set`、`redis_sorted_set`、`redis_stream`、`redis_observe`。写模式额外注册七个对应的 `*_write` 工具；项目不提供 raw command、`Do` 入口或 `redis_observe_write`。

所有集合结果会在访问 Redis 前被限制为 1–1000 项。`SCAN`、`HSCAN`、`SSCAN`、`ZSCAN` 返回固定长度的 opaque base64url 续页引用，不会暴露 Redis 原生 cursor 或缓冲值。一次成功续页后，该引用的空闲有效期为 5 分钟；每个 adapter 最多保存 1,000 个 session、16 MiB 编码状态，并按 LRU 整体淘汰 session。已过期或被淘汰的引用返回 `cursor_expired`；无法接纳的新续页返回 `cursor_unavailable`。Stream 不提供 entry 读取；目录仅保留长度、受限的 pending 查询与 `XCLAIM JUSTID`。

## 配置

Redis topology 必须显式指定。密码只从 `MCP_REDIS_PASSWORD` 读取，不能写入 YAML。

```yaml
redis:
  topology: standalone # standalone | cluster | sentinel
  addresses: ["127.0.0.1:6379"]
  username: ""
  tls: false
  sentinel_master: ""
  write_enabled: false
```

Sentinel 必须设置 `sentinel_master`；Cluster 使用 `addresses` 作为 seed；Sentinel 使用它作为 Sentinel endpoint。

## 传输与部署

```bash
./bin/redis-mcp-server mcp --config config.example.yaml
./bin/redis-mcp-server mcp --config config.example.yaml --port 8080
```

项目保留 Streamable HTTP 和 SSE。若公开 HTTP/SSE，外部 fail-closed 认证边界必须在请求到达本服务前拒绝未认证请求。本仓库不实现认证、网关集成，也不解析或信任认证头。

## 开发

```bash
make test
make lint
make build
make integration
```

`make integration` 使用 `testdata/redis/images.lock` 固定的 Redis 8.8.1 镜像启动 standalone、Cluster、Sentinel。Docker、镜像、网络、compose 或集成测试失败时，该 target 必须失败。
