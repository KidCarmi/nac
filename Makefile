# NAC platform developer task runner.
#
# Run `make help` for the list of targets.

SHELL := /bin/bash
GO    ?= go

VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
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

MIN_COVERAGE ?= 60
.PHONY: cover-gate
cover-gate: ## Enforce the minimum coverage threshold (MIN_COVERAGE)
	$(GO) test -coverprofile=coverage.out ./...
	./scripts/check-coverage.sh $(MIN_COVERAGE) coverage.out

FUZZTIME ?= 30s
.PHONY: fuzz
fuzz: ## Run all fuzz targets briefly (FUZZTIME)
	$(GO) test -run=NONE -fuzz=FuzzNormalizeMAC -fuzztime=$(FUZZTIME) ./pkg/validation/
	$(GO) test -run=NONE -fuzz=FuzzTokenParse -fuzztime=$(FUZZTIME) ./internal/auth/
	$(GO) test -run=NONE -fuzz=FuzzVerifyPassword -fuzztime=$(FUZZTIME) ./internal/auth/
	$(GO) test -run=NONE -fuzz=FuzzEvaluateBody -fuzztime=$(FUZZTIME) ./internal/server/

.PHONY: bench
bench: ## Run benchmarks
	$(GO) test -run=NONE -bench=. -benchmem ./internal/access/...

.PHONY: vuln
vuln: ## Run govulncheck (installs it if missing)
	@command -v govulncheck >/dev/null 2>&1 || $(GO) install golang.org/x/vuln/cmd/govulncheck@latest
	govulncheck ./...

.PHONY: validate-migrations
validate-migrations: ## Validate migrations against TEST_DSN (clean/idempotent/down-up)
	NAC_DB_DSN="$(TEST_DSN)" ./scripts/validate-migrations.sh

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
