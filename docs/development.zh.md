# 开发和发布

[English](development.md)

本页是 redis-mcp-server 的贡献流程。安装和客户端设置在[中文指南](../README.zh.md)。行为和限制在[参考](reference.zh.md)。

## 要求

`go.mod` 和 `.tool-versions` 选择 Go 1.25.10。Build 工作流使用该版本。

`make integration` 和 CI 测试作业需要 Docker。它们从 [testdata/redis/images.lock](../testdata/redis/images.lock) 中的镜像启动 Redis。compose 文件是 [testdata/redis/compose.yaml](../testdata/redis/compose.yaml)。

许可证是 [MIT](../LICENSE)。问题跟踪在 [github.com/futuretea/redis-mcp-server/issues](https://github.com/futuretea/redis-mcp-server/issues)。

不要提交 `MCP_REDIS_PASSWORD`、令牌或填好的客户端配置。密码是环境变量。

## 本地命令

在仓库根目录运行这些命令。

```bash
make test
make lint
make build
make integration
```

`make test` 运行 `go test ./...`。带 `integration` 或 `e2e` 构建标签的文件不属于该命令。只有 `PATH` 上有 `redis-server` 时才运行 `TestVerifyStartupAgainstLocalRedis`。否则它跳过。只有设置了 `REDIS_MATRIX_BINARIES` 时才运行 `TestVerifyStartupRedisVersionMatrix`。

`make lint` 运行 `go vet ./...`，并检查 `cmd`、`internal` 和 `pkg` 的 `gofmt`。`PATH` 上有 `golangci-lint` 时它会运行该程序。二进制缺失时，目标打印正在跳过，并且仍以 0 退出。CI 不使用这个跳过。lint 作业运行 golangci-lint v2.12.2，超时五分钟。本地配置是 [.golangci.yml](../.golangci.yml)。

`make build` 写出 `bin/redis-mcp-server`。版本字符串来自 `git describe`、提交和 UTC 构建时间。因此本地二进制可以打印和已发布 0.2.0 npm 二进制不同的版本。

`make integration` 构建 Linux 测试二进制，启动 compose 栈，等到 cluster 节点 1 报告 `cluster_state:ok`，然后在 compose 网络内运行 adapter 集成测试和 e2e 测试。Docker、镜像拉取、网络、compose 或测试失败都会使目标失败。目标在退出时移除 compose 卷和测试二进制。

`make ci` 运行 `lint`、`test`、`build` 和 `integration`。

`make format` 用 `gofmt` 重写 Go 文件。`make tidy` 运行 `go mod tidy`。`make coverage` 只覆盖 `./pkg/...`，并打印 `go tool cover -func` 的最后一行。`make clean` 移除 `bin/` 和覆盖率文件。

`make docker` 构建本地镜像 `redis-mcp-server:dev`，以及来自 `git describe` 的第二个标签。它不推送。

npm Makefile 目标打包并发布包装包和五个平台包。发布通过 release 工作流使用 `NPM_TOKEN`。除非你正在做发布，否则不要从检出目录发布。

## CI 运行什么

[Build](../.github/workflows/build.yaml) 在推送到 `main` 和 pull request 时运行。它忽略只包含 `.gitignore`、`LICENSE` 或 `*.md` 的变更。改变 Go、YAML 或工作流文件的 pull request 仍会运行该工作流。

工作流有四个作业：

- Lint，在 `ubuntu-latest` 上运行 `go vet`、`gofmt` 检查和 golangci-lint v2.12.2。
- Test，在 `ubuntu-latest` 上运行 `make ci`，其中包括 Redis 8.8.1 集成栈。作业随后上传 `./coverage.txt`。`make ci` 不运行 `coverage` 目标，该目标写出的是 `coverage.out` 而不是 `coverage.txt`。上传失败不会使作业失败。
- Build，在 `ubuntu-latest` 和 `macos-latest` 上运行 `make build`、`go mod verify`、`version` 和 `tools list --config config.example.yaml`。
- Docker Build，在 `ubuntu-latest` 上构建本地 `redis-mcp-server:ci` 镜像，并且不推送。

构建矩阵不包含 Windows。推送标签时，npm 发布工作流构建 Windows amd64。

## 发布产物

推送带前导 `v` 和三段数字的稳定标签，例如 `v0.2.0`，会触发 [release-npm](../.github/workflows/release-npm.yaml) 和 [release-docker](../.github/workflows/release-docker.yaml)。工作流拒绝预发布标签。`v0.2.0` 是当前稳定标签。安装命令在[指南](../README.zh.md)。

npm 工作流构建五个原生二进制，并把它们发布为 `@futuretea/redis-mcp-server` 的可选依赖。npm 版本是去掉前导 `v` 的标签。发布作业使用 Node.js 22。0.2.0 的包装压缩包包含 `package.json`、`bin/index.js`、`LICENSE` 和 `README.md`。它不包含 `README.zh.md`。每个平台压缩包包含 `package.json` 和位于 `bin/redis-mcp-server` 的二进制，Windows 上是 `bin/redis-mcp-server.exe`。平台是 darwin/amd64、darwin/arm64、linux/amd64、linux/arm64 和 windows/amd64。

Docker 工作流为 `linux/amd64` 和 `linux/arm64` 构建 `release` 目标，并推送 `ghcr.io/futuretea/redis-mcp-server`，标签是 git 标签和 `latest`。镜像入口点是 `/usr/local/bin/redis-mcp-server mcp`，用户是 `mcp`。之后的稳定标签会移动 `latest`。

同一标签创建带生成说明的 GitHub Release。发布工作流不上传二进制文件。[v0.2.0 release](https://github.com/futuretea/redis-mcp-server/releases/tag/v0.2.0) 没有附带程序文件。GitHub 仍为该标签提供源码归档。该归档是源码，不是构建好的 `redis-mcp-server` 二进制。从 npm 或 GHCR 安装程序。

0.2.0 已发布的 linux/amd64 npm 二进制打印：

```text
redis-mcp-server version=0.2.0 commit=b519d1d9c7430c96abd07eea736d01c3e5f69676 date=2026-11-15T00:38:32-08:00
```

该提交是 `v0.2.0` 标签。撰写本页时，已核对的 GHCR 标签 `v0.2.0` 和 `latest` 指向同一个镜像索引。该索引中的可运行平台是 `linux/amd64` 和 `linux/arm64`。
