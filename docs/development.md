# Development and release

[中文](development.zh.md)

This page is the contributor workflow for redis-mcp-server. Install and client setup are in the [English guide](../README.md). Behavior and limits are in the [reference](reference.md).

## Requirements

`go.mod` and `.tool-versions` select Go 1.25.10. The Build workflow uses that version.

`make integration` and the CI test job need Docker. They start Redis from the image in [testdata/redis/images.lock](../testdata/redis/images.lock). The compose file is [testdata/redis/compose.yaml](../testdata/redis/compose.yaml).

The license is [MIT](../LICENSE). Issues are tracked at [github.com/futuretea/redis-mcp-server/issues](https://github.com/futuretea/redis-mcp-server/issues).

Do not commit `MCP_REDIS_PASSWORD`, tokens, or a filled-in client config. The password is an environment variable.

## Local commands

Run these from the repository root.

```bash
make test
make lint
make build
make integration
```

`make test` runs `go test ./...`. Files built with the `integration` or `e2e` tag are not part of that command. `TestVerifyStartupAgainstLocalRedis` runs only when `redis-server` is on `PATH`. Otherwise it skips. `TestVerifyStartupRedisVersionMatrix` runs only when `REDIS_MATRIX_BINARIES` is set.

`make lint` runs `go vet ./...` and checks `gofmt` on `cmd`, `internal`, and `pkg`. It runs `golangci-lint` when that binary is on `PATH`. If the binary is missing, the target prints that it is skipping and still exits 0. CI does not use that skip. The lint job runs golangci-lint v2.12.2 with a five-minute timeout. The local config is [.golangci.yml](../.golangci.yml).

`make build` writes `bin/redis-mcp-server`. The version string comes from `git describe`, the commit, and the UTC build time. A local binary can therefore print a different version from the published 0.2.0 npm binary.

`make integration` builds Linux test binaries, starts the compose stack, waits until cluster node 1 reports `cluster_state:ok`, and runs the adapter integration tests and the e2e tests inside the compose network. Docker, image pull, network, compose, or test failures fail the target. The target removes the compose volumes and the test binaries on exit.

`make ci` runs `lint`, `test`, `build`, and `integration`.

`make format` rewrites Go files with `gofmt`. `make tidy` runs `go mod tidy`. `make coverage` covers `./pkg/...` only and prints the last line of `go tool cover -func`. `make clean` removes `bin/` and coverage files.

`make docker` builds the local image `redis-mcp-server:dev` and a second tag from `git describe`. It does not push.

The npm Makefile targets pack and publish the wrapper and the five platform packages. Publishing uses `NPM_TOKEN` through the release workflow. Do not publish from a checkout unless you are cutting the release.

## What CI runs

[Build](../.github/workflows/build.yaml) runs on pushes to `main` and on pull requests. It ignores changes that are only `.gitignore`, `LICENSE`, or `*.md`. A pull request that changes Go, YAML, or workflow files still runs the workflow.

The workflow has four jobs:

- Lint, on `ubuntu-latest`, runs `go vet`, the `gofmt` check, and golangci-lint v2.12.2.
- Test, on `ubuntu-latest`, runs `make ci`, which includes the Redis 8.8.1 integration stack. The job then uploads `./coverage.txt`. `make ci` does not run the `coverage` target, and that target writes `coverage.out` rather than `coverage.txt`. The upload does not fail the job.
- Build, on `ubuntu-latest` and `macos-latest`, runs `make build`, `go mod verify`, `version`, and `tools list --config config.example.yaml`.
- Docker Build, on `ubuntu-latest`, builds a local `redis-mcp-server:ci` image and does not push it.

The build matrix does not include Windows. The npm release workflow builds Windows amd64 when a tag is pushed.

## Release artifacts

Pushing a stable tag with a leading `v` and three numeric parts, such as `v0.2.0`, triggers [release-npm](../.github/workflows/release-npm.yaml) and [release-docker](../.github/workflows/release-docker.yaml). The workflow rejects prerelease tags. `v0.2.0` is the current stable tag. The commands that install it are in the [guide](../README.md).

The npm workflow builds five native binaries and publishes them as optional dependencies of `@futuretea/redis-mcp-server`. The npm version is the tag without the leading `v`. The publish job uses Node.js 22. The wrapper tarball for 0.2.0 contains `package.json`, `bin/index.js`, `LICENSE`, and `README.md`. It does not contain `README.zh.md`. Each platform tarball contains `package.json` and the binary at `bin/redis-mcp-server`, or `bin/redis-mcp-server.exe` on Windows. The platforms are darwin/amd64, darwin/arm64, linux/amd64, linux/arm64, and windows/amd64.

The Docker workflow builds the `release` target for `linux/amd64` and `linux/arm64` and pushes `ghcr.io/futuretea/redis-mcp-server` with the git tag and `latest`. The image entrypoint is `/usr/local/bin/redis-mcp-server mcp` and the user is `mcp`. A later stable tag moves `latest`.

The same tag creates a GitHub release with generated notes. The release workflow does not upload binary files. The [v0.2.0 release](https://github.com/futuretea/redis-mcp-server/releases/tag/v0.2.0) has no attached program assets. GitHub still offers its source archive for the tag. That archive is source, not a built `redis-mcp-server` binary. Install the program from npm or from GHCR.

The published linux/amd64 npm binary from 0.2.0 prints:

```text
redis-mcp-server version=0.2.0 commit=b519d1d9c7430c96abd07eea736d01c3e5f69676 date=2026-11-15T00:38:32-08:00
```

That commit is the `v0.2.0` tag. The checked GHCR tags `v0.2.0` and `latest` pointed at one image index when this page was written. The runnable platforms in that index are `linux/amd64` and `linux/arm64`.
