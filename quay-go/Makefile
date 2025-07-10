# Quay Workers Makefile

# Build variables
VERSION ?= $(shell git describe --tags --always --dirty)
COMMIT ?= $(shell git rev-parse HEAD)
BUILD_DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)"

# Go build settings
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
CGO_ENABLED ?= 0

# Binary names
BINARY_NAME := quay-worker
BINARY_DIR := bin

# Default target
.PHONY: all
all: build

# Build the binary
.PHONY: build
build:
	@echo "Building $(BINARY_NAME) for $(GOOS)/$(GOARCH)..."
	@mkdir -p $(BINARY_DIR)
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(LDFLAGS) -o $(BINARY_DIR)/$(BINARY_NAME) ./cmd/worker

# Build for multiple platforms
.PHONY: build-all
build-all:
	@echo "Building for multiple platforms..."
	@make build GOOS=linux GOARCH=amd64
	@make build GOOS=linux GOARCH=arm64
	@make build GOOS=darwin GOARCH=amd64
	@make build GOOS=darwin GOARCH=arm64

# Run tests
.PHONY: test
test:
	@echo "Running tests..."
	go test -v -race -cover ./...

# Run tests with coverage
.PHONY: test-coverage
test-coverage:
	@echo "Running tests with coverage..."
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Format code
.PHONY: fmt
fmt:
	@echo "Formatting code..."
	go fmt ./...
	goimports -w .

# Lint code
.PHONY: lint
lint:
	@echo "Linting code..."
	golangci-lint run ./...

# Vet code
.PHONY: vet
vet:
	@echo "Vetting code..."
	go vet ./...

# Tidy dependencies
.PHONY: tidy
tidy:
	@echo "Tidying dependencies..."
	go mod tidy

# Download dependencies
.PHONY: deps
deps:
	@echo "Downloading dependencies..."
	go mod download

# Clean build artifacts
.PHONY: clean
clean:
	@echo "Cleaning build artifacts..."
	rm -rf $(BINARY_DIR)
	rm -f coverage.out coverage.html

# Install development tools
.PHONY: install-tools
install-tools:
	@echo "Installing development tools..."
	go install golang.org/x/tools/cmd/goimports@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Run namespace GC worker
.PHONY: run-namespace-gc
run-namespace-gc: build
	@echo "Running namespace GC worker..."
	./$(BINARY_DIR)/$(BINARY_NAME) --worker namespace-gc --config config.yaml

# Run garbage collection worker
.PHONY: run-garbage-collection
run-garbage-collection: build
	@echo "Running garbage collection worker..."
	./$(BINARY_DIR)/$(BINARY_NAME) --worker garbage-collection --config config.yaml

# Show version
.PHONY: version
version: build
	@echo "Showing version..."
	./$(BINARY_DIR)/$(BINARY_NAME) version

# List available workers
.PHONY: list-workers
list-workers: build
	@echo "Listing available workers..."
	./$(BINARY_DIR)/$(BINARY_NAME) list

# Development setup
.PHONY: dev-setup
dev-setup: install-tools deps tidy fmt vet lint test

# Docker build
.PHONY: docker-build
docker-build:
	@echo "Building Docker image..."
	docker build -t quay-worker:$(VERSION) .

# Docker compose up (for local development)
.PHONY: dev-up
dev-up:
	@echo "Starting development environment..."
	docker-compose up -d postgres redis

# Docker compose down
.PHONY: dev-down
dev-down:
	@echo "Stopping development environment..."
	docker-compose down

# Help
.PHONY: help
help:
	@echo "Available targets:"
	@echo "  build              Build the binary"
	@echo "  build-all          Build for multiple platforms"
	@echo "  test               Run tests"
	@echo "  test-coverage      Run tests with coverage"
	@echo "  fmt                Format code"
	@echo "  lint               Lint code"
	@echo "  vet                Vet code"
	@echo "  tidy               Tidy dependencies"
	@echo "  deps               Download dependencies"
	@echo "  clean              Clean build artifacts"
	@echo "  install-tools      Install development tools"
	@echo "  run-namespace-gc   Run namespace GC worker"
	@echo "  run-garbage-collection Run garbage collection worker"
	@echo "  version            Show version"
	@echo "  list-workers       List available workers"
	@echo "  dev-setup          Setup development environment"
	@echo "  docker-build       Build Docker image"
	@echo "  dev-up             Start development environment"
	@echo "  dev-down           Stop development environment"
	@echo "  help               Show this help message"