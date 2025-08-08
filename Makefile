.PHONY: build test clean run-coordinator run-executor install deps fmt vet

# Build configuration
BINARY_DIR=bin
COORDINATOR_BINARY=$(BINARY_DIR)/gofuncs-coordinator
EXECUTOR_BINARY=$(BINARY_DIR)/gofuncs-executor
CLI_BINARY=$(BINARY_DIR)/gofuncs

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOFMT=gofmt

# Build version info
VERSION ?= v0.1.0
BUILD_TIME := $(shell date -u '+%Y-%m-%d_%H:%M:%S')
GIT_COMMIT := $(shell git rev-parse --short HEAD)

LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME) -X main.GitCommit=$(GIT_COMMIT)"

# Default target
all: build

# Create binary directory
$(BINARY_DIR):
	mkdir -p $(BINARY_DIR)

# Build all binaries
build: $(BINARY_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(COORDINATOR_BINARY) ./cmd/coordinator
	$(GOBUILD) $(LDFLAGS) -o $(EXECUTOR_BINARY) ./cmd/executor  
	$(GOBUILD) $(LDFLAGS) -o $(CLI_BINARY) ./cmd/gofuncs

# Build coordinator only
build-coordinator: $(BINARY_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(COORDINATOR_BINARY) ./cmd/coordinator

# Build executor only
build-executor: $(BINARY_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(EXECUTOR_BINARY) ./cmd/executor

# Build CLI only
build-cli: $(BINARY_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(CLI_BINARY) ./cmd/gofuncs

# Install dependencies
deps:
	$(GOMOD) download
	$(GOMOD) tidy

# Run tests
test:
	$(GOTEST) -v -race -coverprofile=coverage.out ./...

# Run tests with coverage report
test-coverage: test
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Benchmark tests
benchmark:
	$(GOTEST) -bench=. -benchmem ./...

# Format code
fmt:
	$(GOFMT) -s -w .

# Vet code
vet:
	$(GOCMD) vet ./...

# Lint code (requires golangci-lint)
lint:
	golangci-lint run

# Clean build artifacts
clean:
	$(GOCLEAN)
	rm -rf $(BINARY_DIR)
	rm -f coverage.out coverage.html

# Run coordinator locally
run-coordinator: build-coordinator
	./$(COORDINATOR_BINARY) -verbose

# Run executor locally
run-executor: build-executor
	./$(EXECUTOR_BINARY) -verbose

# Run coordinator with custom config
run-coordinator-config: build-coordinator
	./$(COORDINATOR_BINARY) -config=config/coordinator.yaml -verbose

# Install binaries to GOPATH/bin
install:
	$(GOCMD) install $(LDFLAGS) ./cmd/coordinator
	$(GOCMD) install $(LDFLAGS) ./cmd/executor
	$(GOCMD) install $(LDFLAGS) ./cmd/gofuncs

# Docker targets
docker-build:
	docker build -t gofuncs-coordinator:$(VERSION) -f deployments/docker/Dockerfile.coordinator .
	docker build -t gofuncs-executor:$(VERSION) -f deployments/docker/Dockerfile.executor .

# Development setup
dev-setup: deps
	@echo "Installing development tools..."
	$(GOGET) github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo "Development environment ready!"

# Generate code (if needed)
generate:
	$(GOCMD) generate ./...

# Security scan
security:
	gosec ./...

# Full CI pipeline
ci: deps fmt vet lint test

# Quick development cycle
dev: fmt vet build test

# Help target
help:
	@echo "Available targets:"
	@echo "  build              - Build all binaries"
	@echo "  build-coordinator  - Build coordinator binary only"
	@echo "  build-executor     - Build executor binary only"
	@echo "  build-cli          - Build CLI binary only"
	@echo "  test               - Run tests"
	@echo "  test-coverage      - Run tests with coverage report"
	@echo "  benchmark          - Run benchmark tests"
	@echo "  fmt                - Format code"
	@echo "  vet                - Vet code"
	@echo "  lint               - Lint code"
	@echo "  clean              - Clean build artifacts"
	@echo "  run-coordinator    - Run coordinator locally"
	@echo "  run-executor       - Run executor locally"
	@echo "  install            - Install binaries to GOPATH/bin"
	@echo "  docker-build       - Build Docker images"
	@echo "  dev-setup          - Set up development environment"
	@echo "  ci                 - Run full CI pipeline"
	@echo "  dev                - Quick development cycle"
	@echo "  help               - Show this help"