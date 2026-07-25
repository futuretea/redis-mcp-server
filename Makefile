BINARY_NAME ?= redis-mcp-server
GOLANGCI_LINT ?= golangci-lint
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || printf '%s' dev)
COMMIT ?= $(shell git rev-parse --verify --quiet HEAD 2>/dev/null || printf '%s' unknown)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X github.com/futuretea/redis-mcp-server/pkg/core/version.Version=$(VERSION) \
	-X github.com/futuretea/redis-mcp-server/pkg/core/version.Commit=$(COMMIT) \
	-X github.com/futuretea/redis-mcp-server/pkg/core/version.Date=$(DATE)

.PHONY: build test lint format tidy ci clean docker coverage integration

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY_NAME) ./cmd/redis-mcp-server

test:
	go test ./...

lint:
	go vet ./...
	test -z "$$(gofmt -l cmd internal pkg)"
	@if command -v "$(GOLANGCI_LINT)" >/dev/null 2>&1; then \
		$(GOLANGCI_LINT) run ./...; \
	else \
		printf '%s\n' "$(GOLANGCI_LINT) not installed, skipping"; \
	fi

format:
	gofmt -w $$(find cmd internal pkg -name '*.go')

tidy:
	go mod tidy

coverage:
	go test ./pkg/... -coverprofile=coverage.out
	go tool cover -func=coverage.out | tail -1

integration:
	@set -eu; \
	set -a; . testdata/redis/images.lock; set +a; \
	test_binary="bin/redisadapter-integration.test"; \
	cleanup() { rm -f "$$test_binary"; REDIS_IMAGE="$$REDIS_IMAGE" docker compose -f testdata/redis/compose.yaml down -v; }; \
	trap cleanup EXIT; \
	mkdir -p bin; \
	CGO_ENABLED=0 GOOS=linux go test -c -tags=integration -o "$$test_binary" ./internal/redisadapter; \
	REDIS_IMAGE="$$REDIS_IMAGE" docker compose -f testdata/redis/compose.yaml up -d standalone sentinel-master sentinel cluster-1 cluster-2 cluster-3; \
	REDIS_IMAGE="$$REDIS_IMAGE" docker compose -f testdata/redis/compose.yaml run --rm cluster-init; \
	for attempt in $$(seq 1 30); do \
		if REDIS_IMAGE="$$REDIS_IMAGE" docker compose -f testdata/redis/compose.yaml exec -T cluster-1 redis-cli cluster info | grep -q '^cluster_state:ok'; then break; fi; \
		if [ "$$attempt" -eq 30 ]; then exit 1; fi; \
		sleep 1; \
	done; \
	REDIS_IMAGE="$$REDIS_IMAGE" docker compose -f testdata/redis/compose.yaml run --rm --no-deps -e REDIS_MCP_INTEGRATION_DOCKER_NETWORK=1 -v "$$PWD/$$test_binary:/redisadapter-integration.test:ro" --entrypoint /redisadapter-integration.test cluster-init -test.v

ci: lint test build integration

DOCKER_TAG ?= dev

docker:
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg GIT_COMMIT=$(COMMIT) \
		--build-arg BUILD_DATE=$(DATE) \
		-t redis-mcp-server:$(DOCKER_TAG) \
		-t redis-mcp-server:$(VERSION) .

clean:
	rm -rf bin coverage.out coverage.txt
