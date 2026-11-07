# NAC platform developer task runner.
#
# Run `make help` for the list of targets.

SHELL := /bin/bash
GO    ?= go

VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
# Commit time, not wall-clock time: rebuilding the same commit yields a
# byte-identical nacd (with the pinned toolchain below), so a lab can verify
# the binary it runs against the published SHA-256.
BUILD_DATE ?= $(shell TZ=UTC0 git log -1 --format=%cd --date=format-local:%Y-%m-%dT%H:%M:%SZ 2>/dev/null || echo unknown)
# Toolchain used for release bundles (fetched via proxy.golang.org if needed).
BUNDLE_TOOLCHAIN ?= go1.25.0
LDFLAGS    := -s -w \
	-X github.com/kidcarmi/nac/pkg/version.Version=$(VERSION) \
	-X github.com/kidcarmi/nac/pkg/version.Commit=$(COMMIT) \
	-X github.com/kidcarmi/nac/pkg/version.BuildDate=$(BUILD_DATE)

COMPOSE := docker compose -f deployments/compose/docker-compose.yml
TEST_DSN ?= postgres://nac:nac@localhost:5432/nac?sslmode=disable

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the nacd binary into ./bin
	@mkdir -p bin
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/nacd ./cmd/nacd

.PHONY: pilot-bundle
pilot-bundle: ## Build the pilot release bundle (linux/amd64 nacd + adapter + installer + docs) into dist/
	@set -e; stage=$$(mktemp -d); dir=$$stage/nac-pilot-$(VERSION); mkdir -p $$dir/bin dist; \
	GOTOOLCHAIN=$(BUNDLE_TOOLCHAIN) CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -buildvcs=false -ldflags "$(LDFLAGS)" -o $$dir/bin/nacd ./cmd/nacd; \
	cp -r deployments docs scripts README.md LICENSE $$dir/; \
	{ echo "version: $(VERSION)"; echo "commit: $$(git rev-parse HEAD)"; echo "build_date: $(BUILD_DATE)"; \
	  echo "toolchain: $(BUNDLE_TOOLCHAIN) linux/amd64 CGO_ENABLED=0 -trimpath"; \
	  echo "nacd_sha256: $$(sha256sum $$dir/bin/nacd | cut -d' ' -f1)"; } > $$dir/BUNDLE-MANIFEST.txt; \
	tar -C $$stage -czf dist/nac-pilot-$(VERSION).tar.gz nac-pilot-$(VERSION); \
	cd dist && sha256sum nac-pilot-$(VERSION).tar.gz > nac-pilot-$(VERSION).tar.gz.sha256 && cat nac-pilot-$(VERSION).tar.gz.sha256

.PHONY: test-e2e-radius
test-e2e-radius: build ## EAP-TLS end to end through FreeRADIUS 3.2.5 (root; see scripts/e2e-freeradius.sh)
	scripts/e2e-freeradius.sh

.PHONY: run
run: ## Run nacd locally (expects Postgres/Redis reachable)
	$(GO) run ./cmd/nacd

.PHONY: test
test: ## Run unit tests with race detector and coverage
	$(GO) test -race -coverprofile=coverage.out ./...

.PHONY: test-integration
test-integration: ## Run integration tests against TEST_DSN
	NAC_TEST_DB_DSN="$(TEST_DSN)" $(GO) test -tags=integration ./test/integration/...

.PHONY: cover
cover: test ## Show total test coverage
	$(GO) tool cover -func=coverage.out | tail -1

.PHONY: lint
lint: ## Run golangci-lint
	golangci-lint run ./...

.PHONY: fmt
fmt: ## Format code
	$(GO) fmt ./...

.PHONY: vet
vet: ## Run go vet
	$(GO) vet ./...

.PHONY: tidy
tidy: ## Tidy go.mod/go.sum
	$(GO) mod tidy

.PHONY: migrate
migrate: ## Apply database migrations and exit
	$(GO) run ./cmd/nacd -migrate

.PHONY: docker-build
docker-build: ## Build the container image
	docker build -f deployments/docker/Dockerfile \
		--build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) --build-arg BUILD_DATE=$(BUILD_DATE) \
		-t nac/nacd:$(VERSION) .

.PHONY: up
up: ## Start the local compose stack
	$(COMPOSE) up --build -d

.PHONY: down
down: ## Stop the local compose stack
	$(COMPOSE) down -v

.PHONY: ci
ci: tidy fmt vet lint test ## Run the full local CI pipeline
	@echo "CI pipeline passed"

.PHONY: check
check: ## Verify go.mod is tidy and code is formatted (used by CI)
	@$(GO) mod tidy && git diff --exit-code go.mod go.sum
	@test -z "$$(gofmt -l . )" || (echo "gofmt needed:"; gofmt -l .; exit 1)
