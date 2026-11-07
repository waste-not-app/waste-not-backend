.PHONY: run build migrate sqlc swag test test-integration vet vuln check fmt
run:
	@set -a; [ -f .env ] && . ./.env; set +a; \
	if command -v go >/dev/null 2>&1; then go run ./cmd/api; else ./bin/api; fi
build:
	CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o bin/api ./cmd/api
	CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o bin/migrate ./cmd/migrate
	CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o bin/guides ./cmd/guides
migrate:
	@set -a; [ -f .env ] && . ./.env; set +a; \
	if command -v go >/dev/null 2>&1; then go run ./cmd/migrate; else ./bin/migrate; fi
sqlc:
	sqlc generate
swag:
	swag init -g cmd/api/main.go -o docs --parseDependency --parseInternal --parseDepth 1
test:
	go test ./...
test-integration:
	@test -n "$$TEST_DATABASE_URL" || (echo 'Set TEST_DATABASE_URL to a dedicated PostgreSQL test database'; exit 1)
	go test -race -count=1 ./...
vet:
	go vet ./...
vuln:
	go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
check: vet test-integration
	@test -z "$$(gofmt -l cmd internal contracts/embed.go)" || (echo 'Run make fmt'; exit 1)
fmt:
	gofmt -w cmd internal contracts/embed.go
