.PHONY: all build test lint fmt clean help coverage-html

BINARY_NAME=alchemist
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_DATE=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS=-ldflags "-X github.com/colbytimm/alchemist/app.Version=${VERSION} -X github.com/colbytimm/alchemist/app.BuildDate=${BUILD_DATE}"

all: lint test build

build:
	@echo "Building ${BINARY_NAME}..."
	@go build ${LDFLAGS} -o ${BINARY_NAME} .

test:
	@echo "Running tests..."
	@go test -v ./...

coverage-html:
	@echo "Generating coverage HTML report..."
	@go test -coverprofile=coverage.out ./... -coverpkg=./...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

lint:
	@echo "Running linters..."
	@go run github.com/golangci/golangci-lint/cmd/golangci-lint@latest run ./...

fmt:
	@echo "Formatting code..."
	@go run golang.org/x/tools/cmd/goimports@latest -w $(shell find . -type f -name "*.go" -not -path "./vendor/*")
	@echo "Ensuring files end with newline and cleaning whitespace..."
	@find . -type f -name "*.go" -not -path "./vendor/*" -exec gofmt -s -w {} \;
	@# This ensures files end with exactly one newline
	@find . -type f -name "*.go" -not -path "./vendor/*" -exec sh -c 'tail -c1 {} | read -r _ || echo "" >> {}' \;

vet:
	@echo "Running go vet..."
	@go vet ./...

clean:
	@echo "Cleaning up..."
	@rm -f ${BINARY_NAME}
	@go clean

install-tools:
	@echo "Installing development tools..."
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@go install golang.org/x/tools/cmd/goimports@latest
	@go install honnef.co/go/tools/cmd/staticcheck@latest
	@echo "Setting up Git pre-commit hook..."
	@chmod +x .git/hooks/pre-commit
	@echo "Development tools installed successfully!"

help:
	@echo "Available commands:"
	@echo "  make build        - Build the binary"
	@echo "  make test         - Run tests"
	@echo "  make coverage-html - Generate coverage HTML report"
	@echo "  make lint         - Run linters"
	@echo "  make fmt          - Format code"
	@echo "  make vet          - Run go vet"
	@echo "  make clean        - Clean up build artifacts"
	@echo "  make install-tools - Install development tools"
	@echo "  make all          - Run lint, test, and build"
	@echo "  make help         - Show this help message"
