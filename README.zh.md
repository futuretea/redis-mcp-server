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

## 使用已发布版本

稳定发布标签使用 `vX.Y.Z`。npm 使用 `X.Y.Z`；Docker 使用 `vX.Y.Z`。npm 包支持 macOS（Intel 和 Apple Silicon）、Linux（x64 和 ARM64）与 Windows x64；Docker 镜像支持 Linux `amd64` 和 `arm64`。

### npm

```bash
export MCP_REDIS_PASSWORD='your-redis-password'
npx -y @futuretea/redis-mcp-server@X.Y.Z --config /path/to/redis-mcp.yaml
```

支持 stdio 的 MCP 客户端可固定同一版本和配置文件路径：

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

将 Redis 配置文件以只读方式挂载，并将密码保留在运行时环境变量中：

```bash
export MCP_REDIS_PASSWORD='your-redis-password'
docker run --rm -i \
  --env MCP_REDIS_PASSWORD \
  --mount type=bind,src="$PWD/redis-mcp.yaml",dst=/etc/redis-mcp-server/config.yaml,readonly \
  ghcr.io/futuretea/redis-mcp-server:vX.Y.Z \
  --config /etc/redis-mcp-server/config.yaml
```

两种发布入口默认都以 stdio 启动 MCP。容器中如需 HTTP，可在发布命令后追加 `--port 8080 --listen 0.0.0.0`，并只向需要访问的网络暴露端口。

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
