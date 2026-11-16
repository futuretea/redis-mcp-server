# Redis MCP Server 参考

[English reference](reference.md)

本页列出 redis-mcp-server 0.2.0 的设置、工具、错误和限制。安装步骤在[中文指南](../README.zh.md)。

## 配置顺序

进程按这个顺序加载值。后面的来源替换前面来源中的同一个字段。

1. 内置默认值。
2. `--config` 指向的 YAML 文件，前提是你设置了该标志。
3. `MCP_` 环境变量。
4. 显式给出的标志。

省略标志时，文件和环境变量的值保留。标志的默认值不会清掉它们。

`tools` 绑定 `--config`、`--enabled-tools`、`--disabled-tools`、`--enable-domains` 和 `--disable-domains`。`mcp` 另外绑定 `--port`、`--listen`、`--log-level` 和 `--sse-base-url`。

没有 Redis URL。服务器不读取 `redis://` 或 `rediss://`。

YAML 中的 `password` 字段被忽略。构造客户端时从 `MCP_REDIS_PASSWORD` 读取密码。配置结构不保存它。未设置该变量时密码为空。

`MCP_MASKING_JSON` 不是单个字段覆盖。替换由 `LoadConfig` 完成。变量存在时，解析该字符串，并且不使用 YAML 的 `masking` 对象。`ParseJSON` 只解析交给它的文本。它不读取环境变量。空白值失败，错误是 `invalid masking config`。CLI 打印 `load config: invalid masking config`。变量未设置时使用 YAML 对象。注释掉的 YAML 和缺少对象相同。

列表环境变量按逗号拆分，然后去掉每一项两端的空白。空格不会把一项拆开。`MCP_REDIS_ADDRESSES=127.0.0.1:6379,10.0.0.2:6379` 和 `MCP_REDIS_ADDRESSES=127.0.0.1:6379, 10.0.0.2:6379` 是两个地址。`MCP_REDIS_ADDRESSES=127.0.0.1:1 127.0.0.1:2` 是一个地址。standalone 会拨号这整段字符串。进程随后报告 `dial tcp: address 127.0.0.1:1 127.0.0.1:2: too many colons in address` 和 `[redis_connection_failed] connection failed`。`MCP_ENABLED_TOOLS=redis_string, redis_hash` 启用这两个工具。`MCP_ENABLED_TOOLS=redis_string redis_hash` 是一个名称，不匹配任何工具，`tools list` 只打印表头。`MCP_DISABLED_TOOLS`、`MCP_ENABLED_DOMAINS` 和 `MCP_DISABLED_DOMAINS` 使用同样的逗号拆分。`--enabled-tools redis_string,redis_hash` 是两个工具。一个参数 `redis_string redis_hash` 不会注册任何工具。

布尔字段接受 YAML 布尔值，或字符串 `1`、`t`、`T`、`TRUE`、`true`、`True`、`0`、`f`、`F`、`FALSE`、`false` 和 `False`。0.2.0 程序把 `MCP_REDIS_WRITE_ENABLED` 的这些值当作布尔值。`yes`、`on`、`y`、`n`、`no` 和 `off` 会被拒绝。CLI 打印 `load config: invalid config`。YAML 中的 `yes` 或 `on` 同样被拒绝。带引号的 YAML 字符串 `"true"` 使用同一组可接受拼写。空的 `MCP_REDIS_WRITE_ENABLED` 不会改变 YAML 里的 `write_enabled: true`。`MCP_PORT` 是整数。

## 默认值

| 字段 | 默认值 | 环境变量 | 标志 |
| --- | --- | --- | --- |
| `port` | `0` | `MCP_PORT` | `mcp --port` |
| `listen` | `127.0.0.1` | `MCP_LISTEN` | `mcp --listen` |
| `sse_base_url` | 空 | `MCP_SSE_BASE_URL` | `mcp --sse-base-url` |
| `log_level` | `info` | `MCP_LOG_LEVEL` | `mcp --log-level` |
| `enabled_tools` | 空 | `MCP_ENABLED_TOOLS` | `--enabled-tools` |
| `disabled_tools` | 空 | `MCP_DISABLED_TOOLS` | `--disabled-tools` |
| `enabled_domains` | 空 | `MCP_ENABLED_DOMAINS` | `--enable-domains` |
| `disabled_domains` | 空 | `MCP_DISABLED_DOMAINS` | `--disable-domains` |
| `allow_unsafe_database_role` | `false` | `MCP_ALLOW_UNSAFE_DATABASE_ROLE` | 无 |
| `redis.topology` | 空，空值会被拒绝 | `MCP_REDIS_TOPOLOGY` | 无 |
| `redis.addresses` | 空，空值会被拒绝 | `MCP_REDIS_ADDRESSES` | 无 |
| `redis.username` | 空 | `MCP_REDIS_USERNAME` | 无 |
| `redis.tls` | `false` | `MCP_REDIS_TLS` | 无 |
| `redis.sentinel_master` | 空 | `MCP_REDIS_SENTINEL_MASTER` | 无 |
| `redis.write_enabled` | `false` | `MCP_REDIS_WRITE_ENABLED` | 无 |

`log_level` 是 `trace`、`debug`、`info`、`warn`、`error`、`fatal`、`panic` 或 `disabled`。其他值失败，错误是 `invalid log_level`。

`port` 必须在 0 到 65535 之间。`0` 选择 stdio，并且不打开监听套接字。非零端口要求 `listen` 非空。

标志中的工具名用逗号分隔。`--enabled-tools redis_string,redis_hash` 启用这两个工具。

唯一的工具域是 `redis`。`enabled_domains` 列表不包含 `redis` 时会移除每个工具。`tools list` 然后只打印表头并以 0 退出。`mcp` 拒绝启动，并报告 `no tools registered; check enabled_tools / disabled_tools / domain filters`。

`enabled_tools` 非空时，工具必须在该列表中。`disabled_tools` 再移除匹配项。同时出现在两个列表中的名称不会注册。

## Redis topology

`redis.topology` 会去掉两端空白并转成小写。缺失、空值或只有空白时失败，错误是 `load config: redis.topology must be explicitly set to standalone, cluster, or sentinel`。非空且不是 `standalone`、`cluster` 或 `sentinel` 的值，例如 `nope`，失败，错误是 `load config: redis.topology must be standalone, cluster, or sentinel`。

`standalone` 拨号 `redis.addresses` 的第 0 个元素。后面的地址被忽略。

`cluster` 把每个地址当作集群种子。

`sentinel` 把每个地址当作 Sentinel 端点，并且要求 `redis.sentinel_master`。同一个用户名和密码会发给 Sentinel 和数据节点。没有单独的 Sentinel 密码字段。`sentinel_master` 在其他 topology 中会被保存，但不会被使用。

`redis.tls` 为 true 时使用 TLS 1.2 或更新版本，以及默认系统根证书。客户端不加载自定义 CA、客户端证书，也不设置 `InsecureSkipVerify`。

没有数据库编号设置。客户端使用数据库 0。

`MaxRetries` 设为 -1，命令重试被关闭。被拒绝的 TCP 地址仍会在 stderr 上产生一行 Redis 连接池拨号日志，然后进程错误是 `[redis_connection_failed] connection failed`。

## 工具目录

目录是封闭的。调用者不能传入 Redis 命令名。没有原始命令工具，也没有 `redis_observe_write`。

`redis.write_enabled` 为 false 时，注册的工具是 `redis_key`、`redis_string`、`redis_hash`、`redis_list`、`redis_set`、`redis_sorted_set`、`redis_stream` 和 `redis_observe`。

`redis.write_enabled` 为 true 时，额外加入这些写工具：`redis_key_write`、`redis_string_write`、`redis_hash_write`、`redis_list_write`、`redis_set_write`、`redis_sorted_set_write` 和 `redis_stream_write`。

`tools list` 和 `tools describe` 加载配置，不连接 Redis。`tools call` 和 `mcp` 会连接，运行 ACL 探测，然后分发调用。

`tools describe redis_string --config config.example.yaml` 打印输入 schema。`--json` 把列表或 schema 打印为 JSON。

调用正文是一个对象：

```json
{"operation":"get","args":{"key":"sample"}}
```

`tools call` 从 `--params` 或 `--params-file` 读取该对象。`--params-file -` 读取 stdin。`--params` 的默认值是 `{}`。`null` 会被拒绝。脱敏关闭时，`tools describe` 和 `tools call` 对缺少的名称打印 `unknown tool "no-such-tool"; run 'redis-mcp-server tools list' to see available tools`，其中的名称是你传入的名称。脱敏开启时，两个命令都打印 `unknown tool`，并且不包含名称。此时 `tools call` 的错误 JSON 文本是 `invalid tool params`。未注册名称的 MCP `tools/call` 在 Redis 处理函数之前被 MCP 库拒绝。JSON-RPC 代码是 invalid params。消息是 `tool '<name>' not found: tool not found`，并且包含名称。脱敏不会改变这条协议消息。

读取操作：

| 工具 | 操作 |
| --- | --- |
| `redis_key` | `exists`、`random_key`、`type`、`expiry`、`dump`、`object_attribute` |
| `redis_string` | `get`、`get_range`、`get_many`、`length`、`digest`、`longest_common_substring` |
| `redis_hash` | `field_exists`、`get`、`length`、`get_many`、`field_expiry`、`random_field`、`scan`、`value_length` |
| `redis_list` | `index`、`length`、`position`、`range` |
| `redis_set` | `cardinality`、`intersection_cardinality`、`member_exists`、`members_exist`、`random_members`、`scan` |
| `redis_sorted_set` | `cardinality`、`count_by_score`、`intersection_cardinality`、`lex_count`、`scores`、`random_members`、`range`、`rank`、`scan`、`score` |
| `redis_stream` | `length`、`pending` |
| `redis_observe` | `scan`、`db_size`、`memory_usage` |

写入操作：

| 工具 | 操作 |
| --- | --- |
| `redis_key_write` | `copy`、`delete`、`set_expiry`、`persist_expiry`、`rename`、`restore`、`touch` |
| `redis_string_write` | `append`、`decrement`、`delete_if`、`get_and_delete`、`get_and_set_expiry`、`get_and_set`、`increment`、`set_many`、`set`、`set_range` |
| `redis_hash_write` | `delete_fields`、`set_field_expiry`、`persist_field_expiry`、`get_and_delete`、`get_and_set_expiry`、`increment`、`set_fields`、`set_fields_with_expiry`、`set_if_absent` |
| `redis_list_write` | `insert`、`move`、`pop`、`pop_many`、`push`、`push_if_exists`、`remove`、`set`、`trim` |
| `redis_set_write` | `add`、`store_difference`、`store_intersection`、`move`、`pop`、`remove`、`store_union` |
| `redis_sorted_set_write` | `add`、`store_difference`、`increment`、`store_intersection`、`pop`、`remove`、`remove_range`、`store_range`、`store_union` |
| `redis_stream_write` | `group`、`ack`、`add`、`claim`、`delete`、`nack`、`trim` |

`redis_stream` 不读取 stream entry。`redis_stream_write` 的 `claim` 要求 `justid` 为 true，并调用 `XCLAIM JUSTID`。

数组参数包含 1 到 1000 项。契约要求的 scan、range 和 pending 的 count 是 1 到 1000 的整数，并在 Redis 命令之前检查。一次 scan 响应最多返回那么多项。一次 Redis 回复中的额外值留在续页状态里，不在该次响应中返回。

`SCAN`、`HSCAN`、`SSCAN` 和 `ZSCAN` 的续页引用是 22 个字符的 base64url token。模式是 `^[A-Za-z0-9_-]{22}$`。token 不是 Redis cursor。成功的一页使该 session 在五分钟内保持空闲有效。一个 adapter 最多保存 1,000 个 session 和 16 MiB 编码状态。淘汰按最近最少使用丢弃整个 session。过期或被淘汰的 token 返回类别 `cursor_expired`。无法接纳的 token 返回类别 `cursor_unavailable`。来自不同 key、match、type 或 `no_values` 的 token 返回 `cursor does not match this scan`。

在 cluster 客户端上，`redis_observe` 的 `db_size` 对当前主节点的 `DBSIZE` 求和，并报告 `primary_count`。其他 topology 报告一台服务器。

二进制输入是带 `utf8` 的对象，或带 `base64` 的对象。已发布 schema 不允许两者同时出现。如果调用者仍然同时发送两个键，adapter 使用 `utf8` 并忽略 `base64`。二进制输出始终包含标准 `base64`。只有字节是合法 UTF-8 时才包含 `utf8`。

成功调用编码为文本，不是 MCP 结构化对象：

```json
{"status":"ok","operation":"get","data":{}}
```

`data` 保存操作字段。Redis 缺少该值时是 `{"status":"not_found","operation":"get"}`。写入条件失败时是 `{"status":"condition_not_met","operation":"set"}`。其他工具失败是：

```json
{"status":"error","operation":"get","error":{"category":"unavailable","message":"Redis operation failed"}}
```

Redis 回复文本不会复制进该消息。类别包括 `invalid_argument`、`unavailable`、`not_found`、`condition_not_met`、`deadline`、`deadline_outcome_unknown`、`wrong_type`、`auth_acl`、`cross_slot`、`cursor_expired`、`cursor_unavailable` 和 `topology_changed`。

`WRONGTYPE` 变成 `wrong_type`。`NOAUTH` 和 `NOPERM` 变成 `auth_acl`。`CROSSSLOT` 变成 `cross_slot`。以 `ERR ` 开头的回复变成 `invalid_argument`，消息是 `Redis rejected an operation argument`。写入遇到截止时间时使用 `deadline_outcome_unknown`，因为写入可能已经落地。读取截止时间使用 `deadline`。

## 启动 ACL 检查

`tools call` 和 `mcp` 在连接成功之后、任何工具命令之前运行这项检查。探测命令是 `PING`、`ACL WHOAMI`、`ACL GETUSER` 和 `ACL CAT`。账号必须能够运行它们。探测失败返回 `[redis_privilege_probe_failed] Redis ACL probe failed`。`allow_unsafe_database_role` 不会跳过失败的探测或被拒绝的连接。

`ACL GETUSER` 不能限制为当前用户，因此该账号可以读取每个用户的 ACL 密码哈希。进程只对已认证用户调用它，并且不记录或返回该回复。

任何根规则或任何 selector 授予下列能力时，检查拒绝该账号：

- `flushall`、`flushdb`、`swapdb`、`migrate`、`config`、`debug`、`shutdown`、`bgrewriteaof`、`bgsave`、`save`、`replicaof`、`slaveof`、`failover`、`module`、`monitor`、`sync`、`psync`、`replconf`、`latency`、`slowlog`、`lastsave`、`role`、`pfdebug`、`pfselftest`、`sentinel`、`function`、`script`、`eval`、`evalsha`、`eval_ro`、`evalsha_ro`、`fcall`、`fcall_ro`、`randomkey`、`scan` 或 `trimslots`
- 整个 `memory` 命令，或 `memory|purge`
- `config|get`、`config|set`、`config|resetstat`、`config|rewrite`
- `acl|deluser`、`acl|dryrun`、`acl|list`、`acl|load`、`acl|log`、`acl|save`、`acl|setuser`、`acl|users`
- `cluster|addslots`、`cluster|addslotsrange`、`cluster|bumpepoch`、`cluster|delslots`、`cluster|delslotsrange`、`cluster|failover`、`cluster|flushslots`、`cluster|forget`、`cluster|meet`、`cluster|replicas`、`cluster|replicate`、`cluster|reset`、`cluster|saveconfig`、`cluster|set-config-epoch`、`cluster|setslot`、`cluster|slaves`、`cluster|count-failure-reports`
- `client|kill`、`client|list`、`client|no-evict`、`client|pause`、`client|unblock`、`client|unpause`
- `function|flush`、`function|delete`、`function|load`、`function|restore`、`function|kill`
- `script|flush`、`script|debug`、`script|kill`、`script|load`

连接的服务器在 `@admin`、`@dangerous` 或 `@scripting` 中报告的命令也会被拒绝，探测命令 `acl|whoami`、`acl|getuser` 和 `acl|cat` 除外。检查器在该回复之前合并一套内置类别。内置 `@dangerous` 集合包含 `keys`、`info`、`restore` 和 `sort`，因此这些名称在加入服务器列表之前就会被拒绝。

这些命令本身不会造成拒绝：`acl|whoami`、`acl|getuser`、`acl|cat`、`cluster|slots`、`cluster|nodes`、`cluster|shards`、`memory|usage` 和 `client|setname`。授予整个 `ACL`、`CLIENT`、`CLUSTER`、`FUNCTION`、`SCRIPT` 或 `MEMORY` 命令会被拒绝，因为该授权包含危险子命令。

key pattern 不适用于 `RANDOMKEY` 或 `SCAN`。`redis_key` 的 `random_key` 调用 `RANDOMKEY`。`redis_observe` 的 `scan` 调用 `SCAN`。撤销了这些命令的账号不能使用这些操作。`DBSIZE` 仍会计入 pattern 之外的 key。

拒绝文本是 `[redis_unsafe_service_role] database service role is not permitted`。

`allow_unsafe_database_role` 为 true，或 `MCP_ALLOW_UNSAFE_DATABASE_ROLE=true`，只跳过这次拒绝。进程记录 `database service role is not permitted; allow_unsafe_database_role is set, so the startup role check is skipped`。该 Redis 用户仍可以在本目录之外运行 `FLUSHALL`、`CONFIG`、`DEBUG` 或 `SHUTDOWN`。只在你控制的实例上设置该选项。日常操作不要使用 Redis 的 `default` 用户。普通服务器授予该用户全部命令，除非设置该选项，否则检查会拒绝它。

`port` 为 0 的 `mcp` 在检查前关闭 zerolog，因此 zerolog 不写这条警告。非零端口的 `mcp` 按 `log_level` 把 zerolog 写到 stderr。默认 `info` 级别包含 `warn`。`tools call` 不关闭 zerolog。Redis 客户端的拨号行独立于 zerolog，仍可能出现在 stderr。

现场启动测试看到主版本大于或等于 7 时，读取账号是 `-@all +@read -@admin -@dangerous -randomkey -scan`，再加上 `+ping +acl|whoami +acl|getuser +acl|cat`。这个形状不撤销 `memory`。同一分支里的应用账号先授予数据类别，再撤销 `@admin`、`@dangerous`、`@scripting`、`flushall`、`flushdb`、`swapdb`、`migrate`、`memory|purge`、`randomkey` 和 `scan`，并加入 `cluster|slots`、`cluster|nodes` 和 `cluster|shards`。报告的主版本低于 7 时，测试源码在读取账号和应用账号上都撤销整个 `memory` 命令，并且应用账号不加入那些 cluster 子命令。仓库工作流不设置 `REDIS_MATRIX_BINARIES`，因此 `make ci` 不运行这个测试。低于 7 的分支是源码文本，不是 CI 执行过的 Redis 版本。

`TRIMSLOTS` 在硬编码拒绝列表中。启动注释记录 Redis 8 也把 `TRIMSLOTS` 和 `BACKUP` 放在 `@dangerous` 中，并且 `@write` 和 `@keyspace` 授予 `TRIMSLOTS`。连接的服务器在 `@dangerous` 中报告 `BACKUP` 时，`BACKUP` 会被拒绝。它不在硬编码名称列表中。

## 输出脱敏

`masking` 对象缺失、省略 `enabled`，或 `enabled` 为 false 时，脱敏关闭。结果、CLI 文本和错误保持现有 JSON。`enabled` 为 true 但没有名称、名称模式、值或值模式时，启动失败，错误是 `invalid masking config`。错误不包含配置文档。

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

未知 JSON 字段会被拒绝。占位符默认是 `[masked]`，必须非空，并且最多 64 字节。四份规则列表各自最多 32 项。名称最多 128 字节。值字面量最多 256 字节。模式最多 128 字节。空项会被拒绝。

名称模式先单独解析，再锚定到整个名称。`a)|(b` 会被拒绝。`(?:a)|(b)` 只匹配完整名称 `a` 和 `b`。值规则是子串匹配，包括单字符字面量，并且只运行一次。占位符不会被再次搜索。能够匹配空输入的模式会被拒绝，包括词边界分支。RE2 返回最左优先匹配，因此 `sk_|sk_live_[0-9]+` 替换 `sk_`，然后不再搜索更长的分支。

这些上限只在脱敏开启时生效。一次结果共用的 budget 是 10,000 个节点、1 MiB、4,096 个 span、JSON 深度 32，以及两轮 JSON 解析。1 MiB 上限计入已访问的输入。它也会拒绝超过 1 MiB 的替换后字符串，或重新编码后的 JSON 文档。超过 1 MiB 的 base64 单元在解码前被拒绝。越过上限会使整个结果失败。没有部分载荷。脱敏关闭时不使用这个 budget。

命中的字符串变成占位符。命中的数字、布尔值或其他非字符串变成 JSON `null`。原本的 null 仍是 null。JSON 值会再按容器计一次，因此它可能比同长度的普通字符串更早耗尽 budget。

Redis 二进制单元按原始字节脱敏。结果重新写入 `base64`，并在脱敏后的字节是合法 UTF-8 时重新写入 `utf8`。`utf8` 和 `base64` 不一致的一对会被拒绝。非法 UTF-8 只保留 `base64`。存成文本的 JSON 对象和数组按 JSON 脱敏，包括转义后的键。

`redis_key` 的 `dump` 是不透明载荷。策略开启时，调用返回 `[redis_masking_failed] result was not returned`，并且没有载荷。策略关闭时，`DUMP` 作为二进制对象返回。`metadata.next_cursor` 是续页 token。规则命中该 token 时，这一页失败，而不是返回原始或改写后的 cursor。两个输出键脱敏成同一个键时也同样失败。

脱敏开启时，包含双引号的工具消息会被替换，Redis 工具集中的固定消息列表除外。`invalid_argument` 然后变成 `Redis rejected an operation argument`。其他带引号的消息变成 `[redis_masking_failed] result was not returned`。发往 Redis 的命令和参数不会被改写。

这不会发现每一个私密值、压缩载荷、未知编码或任意个人数据。

## HTTP

`port` 为 0 时使用 stdio。MCP 消息在 stdout。进程错误在 stderr。

非零端口提供这些路径，并且不检查凭据、cookie 或 `Authorization` 头：

| 路径 | 作用 |
| --- | --- |
| `/mcp` | 无状态 streamable HTTP |
| `/sse` | SSE |
| `/message` | SSE 消息，请求 query 会附加到端点 |
| `/healthz` | 工具注册后，`GET` 返回 `200`，正文是 `healthy`。`HEAD` 返回 `200`、空正文，并且 `Content-Length` 为 7。其他方法返回 `405`，并且没有正文。 |

没有注册工具时，`mcp` 不会打开端口。服务器没有工具时，处理函数的 `GET` 返回 `503`，正文是 `unhealthy`。仓库测试只检查了已有工具的服务器的 `HEAD`。

`sse_base_url` 为空时，SSE 基址使用请求主机。非空值是 SSE 消息端点的公开基址。

服务器用 5 秒读取头，用 30 秒读取请求，空闲时间是 120 秒。写超时是 0，因此打开的 SSE 流不会从读取头的时间起被切断。关闭等待 9 秒。

调试日志记录 `mcp`、`sse`、`message` 或 `other`。它们不记录原始 path 或 query。`/healthz` 不记录日志。

`listen` 默认是回环地址。`0.0.0.0` 接受主机的每个接口。其他机器能够访问的 HTTP 监听前面应放置 TLS、认证和网络策略。本进程不提供这些。

## 版本

模块和 CI 工作流使用 Go 1.25.10。这是 `go.mod` 中的语言版本。工作流不用另一个 Go 版本构建。

`make integration` 使用 [testdata/redis/images.lock](../testdata/redis/images.lock) 固定的 Redis 镜像，即 Redis 8.8.1，启动 standalone、cluster 和 Sentinel。默认 CI 命令不运行 Redis 6 或 Redis 7 矩阵。只有设置了 `REDIS_MATRIX_BINARIES` 时才运行 `TestVerifyStartupRedisVersionMatrix`。工作流不设置它。

启动要求 `PING` 和 ACL 探测命令。源码不声明最低 Redis 版本。有些操作调用较旧服务器没有的命令，包括 `DIGEST`、哈希字段过期，以及 `XNACK` 这类 stream 命令。Redis 拒绝这些调用时走普通工具错误路径。集成镜像是 Redis 8.8.1。

npm 发布作业使用 Node.js 22。包装包的 `package.json` 不设置 `engines`。
