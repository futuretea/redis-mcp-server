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

`tools list` 和 `tools describe` 不会连接 Redis。`tools call` 和 `mcp` 会先连接，再拒绝具备管理命令能力的账号，除非设置了 `allow_unsafe_database_role`。见“服务账号”。

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
allow_unsafe_database_role: false
redis:
  topology: standalone # standalone | cluster | sentinel
  addresses: ["127.0.0.1:6379"]
  username: ""
  tls: false
  sentinel_master: ""
  write_enabled: false
```

`allow_unsafe_database_role` 默认为 false。环境变量是 `MCP_ALLOW_UNSAFE_DATABASE_ROLE`。密码仍然只从 `MCP_REDIS_PASSWORD` 读取。

Sentinel 必须设置 `sentinel_master`；Cluster 使用 `addresses` 作为 seed；Sentinel 使用它作为 Sentinel endpoint。

### 服务账号

进程连上 Redis 之后、任何工具执行命令之前，启动检查会读取当前用户的 ACL。它发送 `PING`、`ACL WHOAMI`、`ACL GETUSER` 和 `ACL CAT`。根规则或任一 selector 能够执行下列命令时，进程以 `[redis_unsafe_service_role] database service role is not permitted` 退出：

- `@admin` 类别中的命令，包括 `CONFIG`、`DEBUG`、`SHUTDOWN`、`ACL SETUSER`、`BGSAVE`、`REPLICAOF`、`MODULE`、`MONITOR`、`CLIENT KILL`，以及 `CLUSTER FAILOVER`、`CLUSTER RESET` 等集群管理子命令
- `FLUSHALL`、`FLUSHDB`、`SWAPDB` 或 `MIGRATE`
- 服务端代码以及全局函数或脚本控制：`@scripting`，包括 `EVAL`、`EVALSHA`、`FCALL`、`SCRIPT DEBUG`、`SCRIPT FLUSH`、`SCRIPT LOAD`、`SCRIPT KILL`、`FUNCTION FLUSH`、`FUNCTION DELETE`、`FUNCTION LOAD`、`FUNCTION RESTORE` 和 `FUNCTION KILL`
- `MEMORY PURGE`
- 该服务器 `@dangerous` 类别中的每个命令。Redis 8 把 `TRIMSLOTS` 和 `BACKUP` 放在这里，它们不在 `@admin` 或 `@scripting` 中，而 `@write` 和 `@keyspace` 会授予 `TRIMSLOTS`
- `RANDOMKEY` 和 `SCAN`。两者都返回 key 名称，并且不接收 key 参数，因此 key pattern 对它们不起作用
- 整个 `ACL`、`CLIENT`、`CLUSTER`、`FUNCTION`、`SCRIPT` 或 `MEMORY` 命令

下列探测命令本身不会导致拒绝：`ACL WHOAMI`、`ACL GETUSER`、`ACL CAT`。Redis 不能把 `ACL GETUSER` 限制为当前用户名，因此该账号也能读取所有用户的 ACL 密码哈希。这种探测披露与 `RANDOMKEY` 或 `SCAN` 返回的 key 名称不是一回事。启动时只对已认证用户调用 `ACL GETUSER`，不会记录或返回该回复。`CLUSTER SLOTS`、`CLUSTER NODES`、`CLUSTER SHARDS` 等集群发现命令不会导致拒绝，`MEMORY USAGE` 和 `CLIENT SETNAME` 也不会。账号必须能够执行这些探测命令，否则即使打开 opt-out，启动也会以 `[redis_privilege_probe_failed]` 失败。连接被拒绝时以 `[redis_connection_failed]` 失败，opt-out 不会跳过它。

只拥有所需读取命令和探测命令的账号可以用默认配置启动，不需要 opt-out。`@read` 包含 `RANDOMKEY` 和 `SCAN`，因此读取账号还需要 `-randomkey` 和 `-scan`。Redis 6.2 的 `@read` 包含 `memory`，且 `MEMORY PURGE` 不是独立的 ACL 名称，因此该账号还需要 `-memory`。业务账号也一样：先授予数据类别，再撤销全局操作。`+@write` 包含 `FLUSHALL`，在 Redis 7 上还包含 `FUNCTION FLUSH`、`FUNCTION DELETE`、`FUNCTION LOAD` 和 `FUNCTION RESTORE`。Redis 8 的 `+@write` 还包含 `TRIMSLOTS`。`+@connection` 包含 `CLIENT KILL`。`+@slow` 和 `+@scripting` 包含 `EVAL` 和 `SCRIPT DEBUG`。先授予宽泛类别，再撤销 `@admin`、`@dangerous` 和 `@scripting`，然后撤销 `RANDOMKEY`、`SCAN` 以及其他单独的全局命令，最后把探测命令加回去。Redis 6.2 会把 `-function`、`-memory|purge` 这类未知名称视为 ACL 错误。在该版本使用 `-memory` 和 `-@scripting`。`-randomkey` 和 `-scan` 在 Redis 6.2 上有效。

```redis
ACL SETUSER mcp-reader on >strong-password ~app:* resetchannels &
  -@all
  +@read
  -@admin
  -@dangerous
  -randomkey
  -scan
  +ping
  +acl|whoami
  +acl|getuser
  +acl|cat
```

```redis
ACL SETUSER mcp-app on >strong-password ~app:* resetchannels &
  -@all
  +@read +@write +@string +@hash +@list +@set +@sortedset +@stream +@fast +@keyspace +@connection
  -@admin -@dangerous -@scripting
  -flushall -flushdb -swapdb -migrate -memory|purge
  -randomkey -scan
  +ping +acl|whoami +acl|getuser +acl|cat
  +cluster|slots +cluster|nodes +cluster|shards
```

`allow_unsafe_database_role` 默认为 `false`。只有无法配置最小权限账号时，才在配置文件中设为 `true`，或导出 `MCP_ALLOW_UNSAFE_DATABASE_ROLE=true`。服务会记录一条警告并跳过这次权限拒绝。连接失败、ACL 探测失败，以及其他启动检查仍会阻止启动。该 Redis 用户仍然可以在工具目录之外执行 `FLUSHALL`、`CONFIG`、`DEBUG` 或 `SHUTDOWN`：本服务不提供 raw command，但账号本身具备这些能力。只在你控制的 Redis 实例上启用该选项。

警告以 warn 级别记录。HTTP 模式在默认 `info` 日志级别下可以看到它。Stdio 模式会关闭日志，以免污染 MCP 输出。不要使用 Redis 的 `default` 用户。标准服务会授予该用户 `+@all`。

## 生产安全指南

本服务会收缩 AI 客户端可使用的 Redis 能力；它不是认证网关，也不是完整的数据访问策略。只读模式会阻止写入，但读取操作仍可能泄露数据或消耗 Redis 容量。

### 默认保持写入关闭

面对不完全可信或通用用途的 AI 客户端，应保持 `redis.write_enabled: false`。开启后会注册修改数据的工具，包括删除 key、裁剪集合；它不是逐请求审批模式。

### 只暴露最小可用工具集

使用 `enabled_tools` 仅暴露客户端所需的工具族。过滤粒度是“工具”，不是“操作”：例如启用 `redis_string` 会暴露该工具声明的全部读取操作。除非确实需要发现 key 或查看 keyspace 元数据，否则不要向不可信客户端暴露 `redis_key` 或 `redis_observe`。

```yaml
enabled_tools:
  - redis_string
  - redis_hash
disabled_tools:
  - redis_key
  - redis_observe
```

### 限制 Redis 账号和 keyspace

使用专用 Redis ACL 账号连接。只授予所选工具所需的命令和 key pattern，不要使用管理员或应用所有者账号。除非设置 `allow_unsafe_database_role`，启动时会拒绝 `@admin`、`@dangerous`、`@scripting`、`FLUSHALL`、`FLUSHDB`、`TRIMSLOTS`、`FUNCTION FLUSH`、`SCRIPT DEBUG`、`MEMORY PURGE`、`RANDOMKEY`、`SCAN`、`CONFIG`、`DEBUG`、`SHUTDOWN` 以及“服务账号”一节列出的其他命令。key pattern 只限制那些带 key 参数的命令。它不适用于无 key 命令。`RANDOMKEY` 和 `SCAN` 会返回整个 keyspace 的 key 名称，包括该 pattern 无法 `GET` 的名称。`DBSIZE` 仍会把这些 key 计入总数。`redis_key` 的 `random_key` 和 `redis_observe` 的 `scan` 会调用 `RANDOMKEY` 和 `SCAN`，因此撤销了这些命令的租户账号不能使用这两个操作。`ACL GETUSER` 仍是单独的探测权限，不是 key 名称列表。

### 将 HTTP 和 SSE 放在认证边界之后

本地 MCP 客户端优先使用 stdio。使用 HTTP 或 SSE 时，应在服务前配置 TLS、fail-closed 认证、网络隔离，以及请求速率和并发限制。不要把该进程直接暴露给不可信网络。

### 限制枚举并核对实际工具目录

`SCAN` 不接收 key 参数，因此 key pattern 不能限制它。租户账号应撤销 `SCAN` 和 `RANDOMKEY`。如果这两个命令仍然被授予，重复的 `SCAN` 请求可以列出该 pattern 之外的 key 名称。每页集合结果仍有上限。应在网关层对请求频率、扫描量、延迟和错误设置配额与监控。部署前检查实际目录：

```bash
./bin/redis-mcp-server tools list --config production.yaml
```

确认没有注册 `*_write` 工具，且每个已列出的工具都是有意暴露的。同时验证部署边界会拒绝未认证请求，Redis ACL 会拒绝目标范围之外的 key。

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
