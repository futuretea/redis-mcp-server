# Redis MCP Server

[English](README.md)

Redis MCP Server 通过 MCP 提供封闭的 Redis 目录。默认目录只读。只有 `redis.write_enabled` 为 true 时才注册写工具。进程不认证客户端。输出脱敏在你启用之前保持关闭。

当前发布版本是 npm `0.2.0`、容器标签 `v0.2.0` 和 git 标签 `v0.2.0`。字段、错误和工具目录见[参考](docs/reference.zh.md)。构建和发布布局见[开发和发布](docs/development.zh.md)。

## 安装 0.2.0

npm 包和容器镜像是程序构建产物。[v0.2.0 GitHub Release](https://github.com/futuretea/redis-mcp-server/releases/tag/v0.2.0) 包含说明和 GitHub 的源码归档。它不附带 `redis-mcp-server` 二进制文件。不要在该页面查找二进制下载。

0.2.0 的 npm 压缩包包含发布时的 README。本文件是更新后的说明。中文说明是仓库里的 [README.zh.md](README.zh.md)。npm 压缩包不包含它。

### npm

把 [config.example.yaml](config.example.yaml) 复制到你传给 `--config` 的路径。被跟踪的文件才是完整配置。只有 `redis` 键的更短文件是另一份文档。

```bash
cp config.example.yaml redis-mcp.yaml
export MCP_REDIS_PASSWORD='your-redis-password'
npx -y @futuretea/redis-mcp-server@0.2.0 --config "$PWD/redis-mcp.yaml"
```

`npx` 启动的是 `redis-mcp-server mcp`。进程在 stdin 和 stdout 上使用 MCP。它先连接 Redis，再提供服务。示例地址上没有 Redis 时，进程以非零状态退出，并向 stderr 写入 `[redis_connection_failed] connection failed`。stdout 保持为空。

stdio 客户端使用同一条命令。把绝对配置路径交给客户端。

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

包装脚本总会插入 `mcp` 子命令。`npx -y @futuretea/redis-mcp-server@0.2.0 version` 不会打印版本。它会启动 `mcp`，并在缺少 `redis.topology` 时失败。执行 `npm install @futuretea/redis-mcp-server@0.2.0` 之后，用平台二进制运行 `version`、`tools` 和 `completion`。

| 主机 | 包 | 二进制 |
| --- | --- | --- |
| macOS Intel | `@futuretea/redis-mcp-server-darwin-amd64` | `bin/redis-mcp-server` |
| macOS Apple Silicon | `@futuretea/redis-mcp-server-darwin-arm64` | `bin/redis-mcp-server` |
| Linux x64 | `@futuretea/redis-mcp-server-linux-amd64` | `bin/redis-mcp-server` |
| Linux ARM64 | `@futuretea/redis-mcp-server-linux-arm64` | `bin/redis-mcp-server` |
| Windows x64 | `@futuretea/redis-mcp-server-windows-amd64` | `bin/redis-mcp-server.exe` |

0.2.0 的 linux/amd64 二进制打印：

```text
redis-mcp-server version=0.2.0 commit=b519d1d9c7430c96abd07eea736d01c3e5f69676 date=2026-11-15T00:38:32-08:00
```

其他平台包以同一版本发布。这次检查只执行了 linux/amd64 二进制。

### Docker

镜像入口点是 `/usr/local/bin/redis-mcp-server mcp`，用户是 `mcp`。`v0.2.0` 包含可运行的 `linux/amd64` 和 `linux/arm64` 镜像。发布流程也会把该镜像标为 `latest`，直到之后的稳定标签移动 `latest`。

挂载 `config.example.yaml` 的副本。仓库不包含 `redis-mcp.yaml`。

```bash
cp config.example.yaml redis-mcp.yaml
export MCP_REDIS_PASSWORD='your-redis-password'
docker run --rm -i \
  --env MCP_REDIS_PASSWORD \
  --mount type=bind,src="$PWD/redis-mcp.yaml",dst=/etc/redis-mcp-server/config.yaml,readonly \
  ghcr.io/futuretea/redis-mcp-server:v0.2.0 \
  --config /etc/redis-mcp-server/config.yaml
```

容器和 `npx` 一样在 stdio 上等待。要运行 `version` 或 `tools`，替换入口点：

```bash
docker run --rm --entrypoint /usr/local/bin/redis-mcp-server \
  ghcr.io/futuretea/redis-mcp-server:v0.2.0 version
```

上面的容器命令已对照已发布镜像配置核对。这里没有 Docker 守护进程，所以没有执行容器。

### 从检出构建

```bash
make build
./bin/redis-mcp-server version
./bin/redis-mcp-server tools list --config config.example.yaml
```

`tools list` 不连接 Redis。`write_enabled` 为 false 时，表中有 `redis_string`，没有 `redis_string_write`。`make build` 的版本行来自 `git describe`，可以和 `0.2.0` 不同。Go、测试和发布细节见[开发和发布](docs/development.zh.md)。

## 配置 Redis

把密码放在 `MCP_REDIS_PASSWORD` 中。不要把它写入 YAML。YAML 里的 `password` 键会被忽略。

`config.example.yaml` 是有效文件。复制它，然后修改 `redis.addresses`。环境变量已经提供 `redis.topology` 和地址时，可以不使用文件：

```bash
export MCP_REDIS_TOPOLOGY=standalone
export MCP_REDIS_ADDRESSES=127.0.0.1:6379
export MCP_REDIS_PASSWORD='your-redis-password'
npx -y @futuretea/redis-mcp-server@0.2.0
```

Sentinel 还要设置 `MCP_REDIS_SENTINEL_MASTER`。多个地址用逗号分隔。`standalone` 使用第一个地址。`cluster` 把每个地址当作集群种子。`sentinel` 把每个地址当作 Sentinel 端点。

优先级、TLS、数据库 0 和其他字段见[参考](docs/reference.zh.md)。

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

上面的块是 [config.example.yaml](config.example.yaml) 中的值。被跟踪的文件还有环境变量名称注释，以及被注释掉的 `masking` 对象。该对象保持注释时，脱敏关闭。

## 调用工具

`tools describe` 打印 schema，并且不连接 Redis。

```bash
./bin/redis-mcp-server tools describe redis_string --config config.example.yaml
./bin/redis-mcp-server tools call redis_string --config config.example.yaml \
  --params '{"operation":"get","args":{"key":"sample"}}'
```

`tools call` 会连接 Redis。示例地址上没有 Redis 时，它以非零状态退出，并向 stderr 写入 `[redis_connection_failed] connection failed`。

只有 `redis.write_enabled` 为 true 或 `MCP_REDIS_WRITE_ENABLED=true` 时才出现写工具。开启写入会注册删除和裁剪操作。它不是逐次调用的审批。通用客户端应保持关闭。

`enabled_tools` 选择整个工具。启用 `redis_string` 会暴露该工具的每个读取操作，而不是其中一个操作。除非客户端需要 key 名称或 keyspace 元数据，否则不要把 `redis_key` 和 `redis_observe` 放进列表。`RANDOMKEY` 和 `SCAN` 返回整个 keyspace 的 key 名称。

使用一个 Redis ACL 账号。它应能运行目录和启动探测，并且不能运行管理、清空、脚本或无 key 枚举命令。拒绝列表和启动测试使用的账号形状见[参考](docs/reference.zh.md)。`allow_unsafe_database_role` 默认为 false。只有在你无法创建该账号并且你控制 Redis 实例时才设置它。

## 选择 stdio 或 HTTP

本地客户端优先使用 stdio。上面的 `npx` 和 `docker run` 命令都是 stdio。

使用 HTTP 时设置端口。默认监听地址是 `127.0.0.1`。

```bash
./bin/redis-mcp-server mcp --config redis-mcp.yaml --port 8080
```

Streamable HTTP 是 `http://127.0.0.1:8080/mcp`。SSE 是 `http://127.0.0.1:8080/sse`。启动后 `GET http://127.0.0.1:8080/healthz` 的正文是 `healthy`。`HEAD` 返回 `200`，并且没有正文。进程不检查 `Authorization` 头。

在容器内，`127.0.0.1` 是容器回环。要发布端口，设置 `--listen 0.0.0.0`，并且只把它发布到允许访问这个未认证进程的网络。

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

其他机器能够访问的监听地址前面应放置 TLS 和认证。

## 启用脱敏

示例文件中的脱敏是关闭的。要打开它，把 `enabled` 设为 true，并至少配置一条规则。变量 `MCP_MASKING_JSON` 一旦设置，就替换整个 YAML 对象。

```bash
export MCP_MASKING_JSON='{"enabled":true,"names":["synthetic_field"],"values":["synthetic-token-value"]}'
```

名称、模式、占位符、限制，以及会整份失败的 Redis 情形，见[参考](docs/reference.zh.md)。脱敏不会发现每一个私密值。
