BINARY_NAME ?= redis-mcp-server
GOLANGCI_LINT ?= golangci-lint
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || printf '%s' dev)
COMMIT ?= $(shell git rev-parse --verify --quiet HEAD 2>/dev/null || printf '%s' unknown)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X github.com/futuretea/redis-mcp-server/pkg/core/version.Version=$(VERSION) \
	-X github.com/futuretea/redis-mcp-server/pkg/core/version.Commit=$(COMMIT) \
	-X github.com/futuretea/redis-mcp-server/pkg/core/version.Date=$(DATE)

NPM_PACKAGE ?= redis-mcp-server
NPM_ROOT ?= npm
NPM_VERSION ?= $(shell git describe --tags --always 2>/dev/null | sed 's/^v//')
NPM_PUBLISH_FLAGS ?= --access=public
NPM_PACK_DIR ?= dist/npm-packages
NPM ?= npm
NODE ?= node
NPM_UNIX_PLATFORMS := darwin-amd64 darwin-arm64 linux-amd64 linux-arm64
NPM_WINDOWS_PLATFORMS := windows-amd64

.PHONY: build test lint format tidy ci clean docker coverage integration npm-copy-platform npm-verify-binaries npm-prepare-packages npm-create-tarballs npm-publish

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
	adapter_test_binary="bin/redisadapter-integration.test"; \
	e2e_test_binary="bin/redis-mcp-server-e2e.test"; \
	e2e_server_binary="bin/redis-mcp-server-e2e"; \
	cleanup() { rm -f "$$adapter_test_binary" "$$e2e_test_binary" "$$e2e_server_binary"; REDIS_IMAGE="$$REDIS_IMAGE" docker compose -f testdata/redis/compose.yaml down -v; }; \
	trap cleanup EXIT; \
	mkdir -p bin; \
	CGO_ENABLED=0 GOOS=linux go test -c -tags=integration -o "$$adapter_test_binary" ./internal/redisadapter; \
	CGO_ENABLED=0 GOOS=linux go test -c -tags=e2e -o "$$e2e_test_binary" ./internal/e2e; \
	CGO_ENABLED=0 GOOS=linux go build -o "$$e2e_server_binary" ./cmd/redis-mcp-server; \
	REDIS_IMAGE="$$REDIS_IMAGE" docker compose -f testdata/redis/compose.yaml up -d standalone sentinel-master sentinel cluster-1 cluster-2 cluster-3; \
	REDIS_IMAGE="$$REDIS_IMAGE" docker compose -f testdata/redis/compose.yaml run --rm cluster-init; \
	for attempt in $$(seq 1 30); do \
		if REDIS_IMAGE="$$REDIS_IMAGE" docker compose -f testdata/redis/compose.yaml exec -T cluster-1 redis-cli cluster info | grep -q '^cluster_state:ok'; then break; fi; \
		if [ "$$attempt" -eq 30 ]; then exit 1; fi; \
		sleep 1; \
	done; \
	REDIS_IMAGE="$$REDIS_IMAGE" docker compose -f testdata/redis/compose.yaml run --rm --no-deps -e REDIS_MCP_INTEGRATION_DOCKER_NETWORK=1 -v "$$PWD/$$adapter_test_binary:/redisadapter-integration.test:ro" --entrypoint /redisadapter-integration.test cluster-init -test.v; \
	REDIS_IMAGE="$$REDIS_IMAGE" docker compose -f testdata/redis/compose.yaml run --rm --no-deps -e REDIS_MCP_E2E_DOCKER_NETWORK=1 -e REDIS_MCP_E2E_SERVER_BINARY=/redis-mcp-server-e2e -v "$$PWD/$$e2e_test_binary:/redis-mcp-server-e2e.test:ro" -v "$$PWD/$$e2e_server_binary:/redis-mcp-server-e2e:ro" --entrypoint /redis-mcp-server-e2e.test cluster-init -test.v

ci: lint test build integration

DOCKER_TAG ?= dev

docker:
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg GIT_COMMIT=$(COMMIT) \
		--build-arg BUILD_DATE=$(DATE) \
		-t redis-mcp-server:$(DOCKER_TAG) \
		-t redis-mcp-server:$(VERSION) .

npm-copy-platform:
	test -n "$(GOOS)"
	test -n "$(GOARCH)"
	test -n "$(OUTPUT)"
	test -f "$(OUTPUT)"
	mkdir -p "$(NPM_ROOT)/$(NPM_PACKAGE)-$(GOOS)-$(GOARCH)/bin"
	cp "$(OUTPUT)" "$(NPM_ROOT)/$(NPM_PACKAGE)-$(GOOS)-$(GOARCH)/bin/$(BINARY_NAME)$(if $(filter windows,$(GOOS)),.exe,)"

npm-verify-binaries:
	@set -eu; \
		for platform in $(NPM_UNIX_PLATFORMS); do \
			test -x "$(NPM_ROOT)/$(NPM_PACKAGE)-$$platform/bin/$(BINARY_NAME)"; \
		done; \
		for platform in $(NPM_WINDOWS_PLATFORMS); do \
			test -f "$(NPM_ROOT)/$(NPM_PACKAGE)-$$platform/bin/$(BINARY_NAME).exe"; \
		done

npm-prepare-packages: npm-verify-binaries
	test -f README.md
	test -f LICENSE
	cp README.md LICENSE "$(NPM_ROOT)/$(NPM_PACKAGE)/"
	@set -eu; for package_dir in "$(NPM_ROOT)"/$(NPM_PACKAGE)-*; do \
		jq --arg version "$(NPM_VERSION)" '.version = $$version' "$$package_dir/package.json" > "$$package_dir/package.json.tmp"; \
		mv "$$package_dir/package.json.tmp" "$$package_dir/package.json"; \
	done
	jq --arg version "$(NPM_VERSION)" '.version = $$version | .optionalDependencies |= with_entries(.value = $$version)' "$(NPM_ROOT)/$(NPM_PACKAGE)/package.json" > "$(NPM_ROOT)/$(NPM_PACKAGE)/package.json.tmp"
	mv "$(NPM_ROOT)/$(NPM_PACKAGE)/package.json.tmp" "$(NPM_ROOT)/$(NPM_PACKAGE)/package.json"

npm-create-tarballs: npm-prepare-packages
	rm -rf "$(NPM_PACK_DIR)"
	mkdir -p "$(NPM_PACK_DIR)"
	@set -eu; for package_dir in "$(NPM_ROOT)"/$(NPM_PACKAGE)-* "$(NPM_ROOT)/$(NPM_PACKAGE)"; do \
		filename="$$(cd "$$package_dir" && $(NPM) pack --json | jq -er '.[0].filename')"; \
		mv "$$package_dir/$$filename" "$(NPM_PACK_DIR)/$$(basename "$$package_dir").tgz"; \
	done

npm-publish: npm-create-tarballs
	@set -eu; \
		temp_dir="$$(mktemp -d)"; \
		trap 'rm -rf "$$temp_dir"' EXIT; \
		for package_dir in "$(NPM_ROOT)"/$(NPM_PACKAGE)-* "$(NPM_ROOT)/$(NPM_PACKAGE)"; do \
			package_name="$$(jq -er '.name' "$$package_dir/package.json")"; \
			package_version="$$(jq -er '.version' "$$package_dir/package.json")"; \
			package_spec="$$package_name@$$package_version"; \
			archive="$(NPM_PACK_DIR)/$$(basename "$$package_dir").tgz"; \
			local_integrity="$$( $(NODE) -e 'const crypto = require("crypto"); const fs = require("fs"); process.stdout.write("sha512-" + crypto.createHash("sha512").update(fs.readFileSync(process.argv[1])).digest("base64"));' "$$archive")"; \
			if $(NPM) view "$$package_spec" dist.integrity --json > "$$temp_dir/view.json" 2> "$$temp_dir/view.err"; then \
				remote_integrity="$$(jq -er 'if type == "string" and length > 0 then . else error("missing dist.integrity") end' "$$temp_dir/view.json")"; \
				if [ "$$local_integrity" != "$$remote_integrity" ]; then \
					echo "Existing $$package_spec does not match the local release artifact" >&2; \
					exit 1; \
				fi; \
				echo "$$package_spec is already published with matching integrity"; \
			elif grep -Eq 'E404|404 Not Found' "$$temp_dir/view.err"; then \
				NPM_PACKAGE_NAME="$$package_name" NPM_PACKAGE_VERSION="$$package_version" $(NPM) publish "$$archive" $(NPM_PUBLISH_FLAGS); \
			else \
				cat "$$temp_dir/view.err" >&2; \
				exit 1; \
			fi; \
		done

clean:
	rm -rf bin coverage.out coverage.txt
