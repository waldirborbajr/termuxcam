default:
    just --list

# === Run & Dev ===
dev:
    air

run:
    go run .

build:
    go build -o bin/app .

build-release:
    go build -ldflags="-s -w" -o bin/app .

# === Quality ===
fmt:
    gofumpt -l -w .

lint:
    golangci-lint run --fast

lint-full:
    golangci-lint run

vet:
    go vet ./...

staticcheck:
    staticcheck ./...

check: fmt lint vet staticcheck

# === Test ===
test:
    richgo test ./... -v -race

test-short:
    richgo test ./... -short

coverage:
    go test ./... -race -coverprofile=coverage.out
    go tool cover -html=coverage.out

# === Dependencies ===
tidy:
    go mod tidy

update:
    go get -u ./...
    go mod tidy

# === Tools ===
install-tools:
    go install github.com/air-verse/air@latest
    go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
    go install mvdan.cc/gofumpt@latest

# === Clean ===
clean:
    go clean -cache -modcache
    rm -rf bin/

# === Full CI Check ===
ci: check test
    @echo "✅ All checks passed!"
