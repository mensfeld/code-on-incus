.PHONY: build install clean test test-coverage test-unit test-python-setup test-python test-python-debug test-python-cli lint lint-python fmt tidy help

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

# Build the project
build:
	@echo "Building $(BINARY_NAME)..."
	@$(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/coi
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

# Setup Python test dependencies
test-python-setup:
	@echo "Installing Python test dependencies..."
	@pip install -r tests/support/requirements.txt
	@pip install ruff

# Run Python integration tests (requires Incus)
test-python: build
	@echo "Running Python integration tests..."
	@if groups | grep -q incus-admin; then \
		pytest tests/ -v; \
	else \
		echo "Running with incus-admin group..."; \
		sg incus-admin -c "pytest tests/ -v"; \
	fi

# Run Python tests with output (for debugging)
test-python-debug: build
	@echo "Running Python tests with output..."
	@if groups | grep -q incus-admin; then \
		pytest tests/ -v -s; \
	else \
		echo "Running with incus-admin group..."; \
		sg incus-admin -c "pytest tests/ -v -s"; \
	fi

# Run only Python CLI tests (no Incus required)
test-python-cli:
	@echo "Running Python CLI tests..."
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
	@mkdir -p dist
	@GOOS=linux GOARCH=amd64 $(GOBUILD) -o dist/$(BINARY_NAME)-linux-amd64 ./cmd/coi
	@GOOS=linux GOARCH=arm64 $(GOBUILD) -o dist/$(BINARY_NAME)-linux-arm64 ./cmd/coi
	@GOOS=darwin GOARCH=amd64 $(GOBUILD) -o dist/$(BINARY_NAME)-darwin-amd64 ./cmd/coi
	@GOOS=darwin GOARCH=arm64 $(GOBUILD) -o dist/$(BINARY_NAME)-darwin-arm64 ./cmd/coi

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
	@echo "Testing (Python):"
	@echo "  test-python-setup - Install Python test dependencies"
	@echo "  test-python       - Run Python integration tests (requires Incus)"
	@echo "  test-python-debug - Run Python tests with output (for debugging)"
	@echo "  test-python-cli   - Run Python CLI tests only (no Incus required)"
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
