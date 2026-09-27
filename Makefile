BINARY_NAME := alchemist
MODULE := github.com/colbytimm/alchemist
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -ldflags "-X $(MODULE)/app.Version=$(VERSION) -X $(MODULE)/app.BuildDate=$(BUILD_DATE)"

# Pinned tool versions (no @latest — reproducible builds).
GOLANGCI_LINT := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.4.0
GOSEC := go run github.com/securego/gosec/v2/cmd/gosec@v2.22.8
GOVULNCHECK := go run golang.org/x/vuln/cmd/govulncheck@v1.1.4
GITLEAKS := go run github.com/zricethezav/gitleaks/v8@v8.30.1
GORELEASER := go run github.com/goreleaser/goreleaser/v2@v2.17.1
ACTIONLINT := go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
# goreleaser runs syft by name, so it is installed into TOOLS_BIN rather than run with go run.
SYFT_VERSION := v1.52.0
CSPELL := npx --yes cspell@10.1.1

# --dot reaches .claude/ and .golangci.yml; --gitignore skips build artifacts.
CSPELL_FLAGS := lint --no-progress --dot --gitignore "**"

TOOLS_BIN := $(CURDIR)/bin/tools

COVER_PKGS := ./app/...,./cmd/...,./internal/...
EMULATOR_URL ?= http://localhost:8081

.PHONY: all build test bench bench-gate bench-baseline test-coverage emulator-up emulator-wait emulator-seed emulator-down test-integration coverage-html fmt fmt-check vet lint spell security gosec govulncheck gitleaks actionlint syft release-check release-snapshot release clean help

## all: fmt-check, lint, spell, test, build
all: fmt-check lint spell test build

## build: compile the binary into bin/ with version ldflags
build:
	go build $(LDFLAGS) -o bin/$(BINARY_NAME) .

## test: run unit tests
test:
	go test ./...

# The typing benchmarks that cmd/benchmark-gate holds to its budget. Five runs
# each, of which the gate takes the median.
BENCH_FLAGS := -run XXX -bench 'Typing|ViewWithoutEdit|Diagnose' -benchtime 50x -benchmem -count 5
BENCH_PKGS := ./internal/tui/test/ ./internal/query/test/

## bench: run the typing benchmarks into bench.txt
bench:
	go test $(BENCH_FLAGS) $(BENCH_PKGS) > bench.txt
	@cat bench.txt

## bench-gate: fail when bench.txt breaks the budget or the baseline
bench-gate: bench
	go run ./cmd/benchmark-gate -baseline testdata/bench-baseline.txt bench.txt

## bench-baseline: refresh testdata/bench-baseline.txt, to commit on its own
bench-baseline:
	go test $(BENCH_FLAGS) $(BENCH_PKGS) > testdata/bench-baseline.txt

## emulator-up: start the Cosmos DB emulator container
emulator-up:
	docker compose -f test/integration/docker-compose.yml up -d --wait

## emulator-wait: block until the emulator answers on EMULATOR_URL (it boots slowly)
emulator-wait:
	@for attempt in $$(seq 1 60); do \
		if curl -ks --max-time 5 -o /dev/null $(EMULATOR_URL); then exit 0; fi; \
		sleep 5; \
	done; \
	echo "emulator did not answer on $(EMULATOR_URL)"; exit 1

## emulator-seed: replace the sales, telemetry and hr databases in the emulator with sample data
emulator-seed:
	go run ./test/seed

## emulator-down: stop the Cosmos DB emulator container
emulator-down:
	docker compose -f test/integration/docker-compose.yml down -v

## test-integration: run integration tests against the emulator
test-integration:
	go test -tags integration -count=1 -timeout 10m ./internal/adapter/cosmos/test/... ./test/integration/...

## test-coverage: run unit tests, writing coverage.out
test-coverage:
	go test -coverprofile=coverage.out -coverpkg=$(COVER_PKGS) ./...

## coverage-html: generate an HTML coverage report
coverage-html: test-coverage
	go tool cover -html=coverage.out -o coverage.html
	@go tool cover -func=coverage.out | tail -1

## fmt: format all Go files
fmt:
	gofmt -s -w .
	go run golang.org/x/tools/cmd/goimports@v0.36.0 -local $(MODULE) -w .

## fmt-check: fail if any file is not gofmt-clean
fmt-check:
	@out=$$(gofmt -s -l .); if [ -n "$$out" ]; then echo "gofmt needed on:"; echo "$$out"; exit 1; fi

## vet: run go vet
vet:
	go vet ./...

## lint: run golangci-lint
lint:
	$(GOLANGCI_LINT) run ./...

## spell: spell-check every file (skipped with a warning if npx is not installed)
spell:
	@if command -v npx >/dev/null 2>&1; then \
		$(CSPELL) $(CSPELL_FLAGS); \
	else \
		echo "warning: npx not installed (brew install node); skipping"; \
	fi

## security: run all security checks
security: gosec govulncheck gitleaks

## gosec: static security analysis
gosec:
	$(GOSEC) -quiet ./...

## govulncheck: known-vulnerability scan
govulncheck:
	$(GOVULNCHECK) ./...

## gitleaks: secret scan
gitleaks:
	$(GITLEAKS) detect --source . --no-banner

## actionlint: lint the GitHub Actions workflows
actionlint:
	$(ACTIONLINT)

## release-check: validate .goreleaser.yml
release-check:
	$(GORELEASER) check

## syft: install the pinned syft, which writes the SBOMs, into bin/tools
syft:
	GOBIN=$(TOOLS_BIN) go install github.com/anchore/syft/cmd/syft@$(SYFT_VERSION)

## release-snapshot: build every release artifact into dist/ without publishing (the MSI needs wixl)
release-snapshot: syft
	PATH="$(TOOLS_BIN):$$PATH" $(GORELEASER) release --snapshot --clean

## release: publish a GitHub Release for the current tag (needs GITHUB_TOKEN, wixl and osslsigncode)
release: syft
	PATH="$(TOOLS_BIN):$$PATH" $(GORELEASER) release --clean

## clean: remove build and coverage artifacts
clean:
	rm -rf bin/ coverage.out coverage.html bench.txt

## help: list targets
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## /  /'
