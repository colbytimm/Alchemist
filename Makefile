.PHONY: all build test lint fmt clean help coverage-html security security-deps gosec govulncheck gitleaks lint-security all-security

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
	@go test -v ./... -coverprofile=coverage.out

coverage-html:
	@echo "Generating coverage HTML report..."
	@go test -coverprofile=coverage.out.tmp ./... -coverpkg=github.com/colbytimm/alchemist/cmd,github.com/colbytimm/alchemist/data,github.com/colbytimm/alchemist/cosmos,github.com/colbytimm/alchemist/services,github.com/colbytimm/alchemist/util
	@# Filter out mock and interface files from coverage report
	@cat coverage.out.tmp | grep -v "mock" | grep -v "_mock.go" | grep -v "mocks.go" | grep -v "_interface.go" | grep -v "interface_" > coverage.out
	@rm coverage.out.tmp
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"
	@echo "Total coverage: $$(go tool cover -func=coverage.out | grep total: | awk '{print $$3}')"

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

# Install security dependencies
security-deps:
	@echo "Installing security tools..."
	@go install github.com/securego/gosec/v2/cmd/gosec@latest
	@go install golang.org/x/vuln/cmd/govulncheck@latest
	@go install github.com/zricethezav/gitleaks/v8@latest

# Run gosec security scanner
gosec:
	@echo "Running gosec..."
	@go run github.com/securego/gosec/v2/cmd/gosec@latest -no-fail -fmt=text -out=gosec-results.txt ./...
	@echo "gosec results saved to gosec-results.txt"

# Check for vulnerabilities in dependencies
vuln-check:
	@echo "Running govulncheck..."
	@go run golang.org/x/vuln/cmd/govulncheck@latest ./... || true
	@echo "govulncheck results noted. See output above for details."

# Check for leaked secrets
gitleaks:
	@echo "Running gitleaks..."
	@go run github.com/zricethezav/gitleaks/v8@latest detect --report-path=gitleaks-report.json || true
	@echo "gitleaks results saved to gitleaks-report.json"

# Run security linters as part of golangci-lint
lint-security:
	@echo "Running security linters with golangci-lint..."
	@go run github.com/golangci/golangci-lint/cmd/golangci-lint@latest run --timeout=5m -c .golangci-security.yml ./...

# Run all security checks
security: gosec vuln-check gitleaks lint-security
	@echo "All security checks completed"

# Complete quality gate including security
quality-gate: build test lint gosec vuln-check gitleaks
	@echo "All quality checks and security scans completed"

clean:
	@echo "Cleaning up..."
	@rm -f ${BINARY_NAME}
	@rm -f gosec-results.txt gitleaks-report.json coverage.out coverage.html
	@go clean

install-tools: security-deps
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
	@echo "  make security     - Run all security checks"
	@echo "  make security-deps - Install security tools"
	@echo "  make gosec        - Run gosec security scanner"
	@echo "  make govulncheck  - Check dependencies for vulnerabilities"
	@echo "  make gitleaks     - Check for leaked secrets"
	@echo "  make lint-security - Run security-focused linters"
	@echo "  make all-security - Run all quality and security checks"
	@echo "  make help         - Show this help message"
