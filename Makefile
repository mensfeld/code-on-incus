.PHONY: build install clean test test-coverage test-unit integrations-setup integrations integrations-debug integrations-cli lint lint-python fmt tidy help

# Binary name
BINARY_NAME=coi
BINARY_FULL=claude-on-incus

# Build directory
BUILD_DIR=.

# Installation directory
INSTALL_DIR=/usr/local/bin

# Coverage directory
COVERAGE_DIR=coverage

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOFMT=$(GOCMD) fmt
GOVET=$(GOCMD) vet

# Version injection
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS=-ldflags "-X github.com/mensfeld/code-on-incus/internal/cli.Version=$(VERSION)"

# Build the project
build:
	@echo "Building $(BINARY_NAME) version $(VERSION)..."
	@$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/coi
	@ln -sf $(BINARY_NAME) $(BUILD_DIR)/$(BINARY_FULL)

# Install to system
install: build
	@sudo cp $(BUILD_DIR)/$(BINARY_NAME) $(INSTALL_DIR)/$(BINARY_NAME)
	@sudo ln -sf $(INSTALL_DIR)/$(BINARY_NAME) $(INSTALL_DIR)/$(BINARY_FULL)

# Clean build artifacts
clean:
	@$(GOCLEAN)
	@rm -f $(BUILD_DIR)/$(BINARY_NAME)
	@rm -f $(BUILD_DIR)/$(BINARY_FULL)
	@rm -rf $(COVERAGE_DIR)
	@rm -rf dist
	@bash scripts/cleanup-pycache.sh

# Run all tests (unit tests only)
test:
	@echo "Running unit tests..."
	$(GOTEST) -v -race -short ./...

# Setup integration test dependencies
integrations-setup:
	@echo "Installing integration test dependencies..."
	@pip install -r tests/support/requirements.txt
	@pip install ruff

# Run integration tests (requires Incus)
integrations: build
	@echo "Running integration tests..."
	@bash scripts/cleanup-pycache.sh
	@if groups | grep -q incus-admin; then \
		pytest tests/ -v; \
	else \
		echo "Running with incus-admin group..."; \
		sg incus-admin -c "pytest tests/ -v"; \
	fi

# Run integration tests with output (for debugging)
integrations-debug: build
	@echo "Running integration tests with output..."
	@bash scripts/cleanup-pycache.sh
	@if groups | grep -q incus-admin; then \
		pytest tests/ -v -s; \
	else \
		echo "Running with incus-admin group..."; \
		sg incus-admin -c "pytest tests/ -v -s"; \
	fi

# Run only CLI tests (no Incus required)
integrations-cli:
	@echo "Running CLI integration tests..."
	@pytest tests/cli/ -v

# Lint Python tests
lint-python:
	@echo "Linting Python tests..."
	@ruff check tests/
	@ruff format --check tests/

# Run unit tests only (fast)
test-unit:
	@echo "Running unit tests..."
	$(GOTEST) -v -short -race ./...

# Run tests with coverage (unit tests only)
test-coverage:
	@mkdir -p $(COVERAGE_DIR)
	@echo "Running unit tests with coverage..."
	@$(GOTEST) -v -short -race -coverprofile=$(COVERAGE_DIR)/coverage.out -covermode=atomic ./...
	@$(GOCMD) tool cover -html=$(COVERAGE_DIR)/coverage.out -o $(COVERAGE_DIR)/coverage.html
	@$(GOCMD) tool cover -func=$(COVERAGE_DIR)/coverage.out | grep total | awk '{print "Test Coverage: " $$3}'
	@echo "Report: $(COVERAGE_DIR)/coverage.html"

# Tidy dependencies
tidy:
	@$(GOMOD) tidy

# Format code
fmt:
	@$(GOFMT) ./...

# Check formatting
fmt-check:
	@test -z "$$(gofmt -l .)" || (echo "Files need formatting:" && gofmt -l . && exit 1)

# Run linter
lint:
	@which golangci-lint > /dev/null || (echo "Error: golangci-lint not installed" && exit 1)
	@golangci-lint run --timeout 5m

# Run go vet
vet:
	@$(GOVET) ./...

# Check documentation coverage
doc-coverage:
	@bash scripts/doc-coverage.sh

# Run all checks (CI)
check: fmt-check vet lint test

# Run all checks including doc coverage
check-all: check doc-coverage

# Build for multiple platforms
build-all:
	@echo "Building $(BINARY_NAME) version $(VERSION) for all platforms..."
	@mkdir -p dist
	@GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o dist/$(BINARY_NAME)-linux-amd64 ./cmd/coi
	@GOOS=linux GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o dist/$(BINARY_NAME)-linux-arm64 ./cmd/coi
	@GOOS=darwin GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o dist/$(BINARY_NAME)-darwin-amd64 ./cmd/coi
	@GOOS=darwin GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o dist/$(BINARY_NAME)-darwin-arm64 ./cmd/coi

# Help
help:
	@echo "Available targets:"
	@echo ""
	@echo "Build:"
	@echo "  build         - Build the binary"
	@echo "  build-all     - Build for all platforms"
	@echo "  install       - Install to $(INSTALL_DIR)"
	@echo "  clean         - Remove build artifacts"
	@echo ""
	@echo "Testing (Go):"
	@echo "  test          - Run Go unit tests (fast, no Incus)"
	@echo "  test-unit     - Same as test"
	@echo "  test-coverage - Unit tests with coverage report"
	@echo ""
	@echo "Testing (Integration):"
	@echo "  integrations-setup - Install integration test dependencies"
	@echo "  integrations       - Run integration tests (requires Incus)"
	@echo "  integrations-debug - Run integration tests with output (for debugging)"
	@echo "  integrations-cli   - Run CLI integration tests only (no Incus required)"
	@echo ""
	@echo "Code Quality:"
	@echo "  fmt         - Format Go code"
	@echo "  fmt-check   - Check Go code formatting"
	@echo "  vet         - Run go vet"
	@echo "  lint        - Run golangci-lint"
	@echo "  lint-python - Lint and format check Python tests"
	@echo "  check       - Run all checks (fmt, vet, lint, test)"
	@echo ""
	@echo "Maintenance:"
	@echo "  tidy        - Tidy dependencies"
	@echo "  help        - Show this help"

# Default target
.DEFAULT_GOAL := build
