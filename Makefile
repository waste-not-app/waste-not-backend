SHELL := /bin/sh
.DEFAULT_GOAL := help

GOLANGCI_LINT_VERSION ?= v2.15.0
GOVULNCHECK_VERSION   ?= v1.10.0
SQLC_VERSION          ?= v1.30.0
SWAG_VERSION          ?= v1.16.6

GOLANGCI_LINT := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
GOVULNCHECK   := go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
SWAG          := go run github.com/swaggo/swag/cmd/swag@$(SWAG_VERSION)

BIN_DIR   := bin
LDFLAGS   := -s -w
GO_BUILD  := CGO_ENABLED=0 go build -trimpath -ldflags='$(LDFLAGS)'
IMAGE     ?= wastenot-api
TAG       ?= local
TEST_DB_CONTAINER := wastenot-test-db
TEST_DB_PORT      ?= 55432
TEST_DATABASE_URL ?= postgres://postgres:test-only@127.0.0.1:$(TEST_DB_PORT)/wastenot_test?sslmode=disable
DEV_COMPOSE       := docker compose -f compose.dev.yaml

.PHONY: help
help: ## Show available targets
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z0-9_-]+:.*## / {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.PHONY: run
run: ## Run the API with variables from .env
	@set -a; [ -f .env ] && . ./.env; set +a; go run ./cmd/api

.PHONY: migrate
migrate: ## Apply database migrations using .env
	@set -a; [ -f .env ] && . ./.env; set +a; go run ./cmd/migrate

.PHONY: guides
guides: ## Import reviewed guides using .env
	@set -a; [ -f .env ] && . ./.env; set +a; go run ./cmd/guides guides/core-guides-v1.json

.PHONY: build
build: ## Build api, migrate and guides binaries into bin/
	$(GO_BUILD) -o $(BIN_DIR)/api ./cmd/api
	$(GO_BUILD) -o $(BIN_DIR)/migrate ./cmd/migrate
	$(GO_BUILD) -o $(BIN_DIR)/guides ./cmd/guides

.PHONY: fmt
fmt: ## Format code (gofmt + goimports)
	$(GOLANGCI_LINT) fmt ./...

.PHONY: fmt-check
fmt-check: ## Fail when code is not formatted
	$(GOLANGCI_LINT) fmt --diff ./...

.PHONY: lint
lint: ## Run golangci-lint (includes gosec, errorlint, staticcheck)
	$(GOLANGCI_LINT) run ./...

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: vuln
vuln: ## Scan dependencies and call graph with govulncheck
	$(GOVULNCHECK) ./...

.PHONY: sec
sec: lint vuln ## Run all security checks

.PHONY: test
test: ## Run unit tests (integration tests skip without TEST_DATABASE_URL)
	go test -count=1 ./...

.PHONY: test-race
test-race: ## Run tests with the race detector against TEST_DATABASE_URL
	@test -n "$(TEST_DATABASE_URL)" || (echo 'Set TEST_DATABASE_URL'; exit 1)
	TEST_DATABASE_URL='$(TEST_DATABASE_URL)' go test -race -count=1 -p 1 ./...

.PHONY: test-integration
test-integration: db-test-up ## Run every test against a disposable PostgreSQL container
	TEST_DATABASE_URL='$(TEST_DATABASE_URL)' go test -count=1 -p 1 ./... ; status=$$?; $(MAKE) --no-print-directory db-test-down; exit $$status

.PHONY: cover
cover: ## Write coverage.out and print the total
	go test -count=1 -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

.PHONY: check
check: fmt-check vet lint test ## Run the local quality gate

.PHONY: ci
ci: fmt-check vet lint vuln test-race build ## Run the CI quality gate

.PHONY: sqlc
sqlc: ## Regenerate type-safe queries
	go run github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION) generate

.PHONY: swag
swag: ## Regenerate Swagger docs
	$(SWAG) init -g cmd/api/main.go -o docs --parseDependency --parseInternal --parseDepth 1

.PHONY: db-test-up
db-test-up: ## Start a disposable PostgreSQL for tests
	@docker rm -f $(TEST_DB_CONTAINER) >/dev/null 2>&1 || true
	docker run -d --rm --name $(TEST_DB_CONTAINER) -e POSTGRES_USER=postgres -e POSTGRES_PASSWORD=test-only \
		-e POSTGRES_DB=wastenot_test -p 127.0.0.1:$(TEST_DB_PORT):5432 postgres:18-bookworm >/dev/null
	@until docker exec $(TEST_DB_CONTAINER) pg_isready -U postgres -d wastenot_test >/dev/null 2>&1; do sleep 1; done

.PHONY: db-test-down
db-test-down: ## Stop the disposable test database
	@docker rm -f $(TEST_DB_CONTAINER) >/dev/null 2>&1 || true

.PHONY: docker-build
docker-build: ## Build the production image
	docker build -t $(IMAGE):$(TAG) .

.PHONY: env
env: ## Create .env from .env.example with fresh local secrets
	@test ! -f .env || (echo '.env already exists'; exit 1)
	@jwt=$$(openssl rand -base64 32); pw=$$(openssl rand -hex 16); \
	awk -v jwt="$$jwt" -v pw="$$pw" '{ \
		if ($$0 ~ /^JWT_SECRET_BASE64=/) print "JWT_SECRET_BASE64=" jwt; \
		else if ($$0 ~ /^POSTGRES_PASSWORD=/) print "POSTGRES_PASSWORD=" pw; \
		else { gsub(/replace-local-db-password/, pw); print } }' .env.example > .env
	@echo 'Created .env with random local secrets'

.PHONY: up
up: ## Start the local stack (db, migrate, guides, api)
	$(DEV_COMPOSE) up -d --build

.PHONY: down
down: ## Stop the local stack
	$(DEV_COMPOSE) down

.PHONY: logs
logs: ## Follow local stack logs
	$(DEV_COMPOSE) logs -f api

.PHONY: clean
clean: ## Remove build and coverage artifacts
	rm -rf $(BIN_DIR) coverage.out
