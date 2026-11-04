.PHONY: run build migrate test test-integration vet vuln check fmt
run:
	go run ./cmd/api
build:
	CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o bin/api ./cmd/api
	CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o bin/migrate ./cmd/migrate
	CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o bin/guides ./cmd/guides
migrate:
	go run ./cmd/migrate
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
