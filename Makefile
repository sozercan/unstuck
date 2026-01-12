.PHONY: all build test test-unit test-integration lint clean install

# Binary name
BINARY := unstuck
# Build directory
BUILD_DIR := bin

# Go parameters
GOCMD := go
GOBUILD := $(GOCMD) build
GOTEST := $(GOCMD) test
GOMOD := $(GOCMD) mod
GOVET := $(GOCMD) vet

# Version information
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

# Linker flags
LDFLAGS := -s -w \
	-X main.version=$(VERSION) \
	-X main.commit=$(COMMIT) \
	-X main.date=$(DATE)

all: lint test build

## build: Build the binary
build:
	@echo "Building $(BINARY)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 $(GOBUILD) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) ./cmd/unstuck

## install: Install the binary to GOPATH/bin
install:
	@echo "Installing $(BINARY)..."
	CGO_ENABLED=0 $(GOCMD) install -ldflags "$(LDFLAGS)" ./cmd/unstuck

## test: Run all tests
test: test-unit

## test-unit: Run unit tests
test-unit:
	@echo "Running unit tests..."
	$(GOTEST) -v -race -coverprofile=coverage.out -covermode=atomic ./pkg/...

## test-integration: Run integration tests (requires kind cluster)
test-integration:
	@echo "Running integration tests..."
	@if ! kubectl cluster-info > /dev/null 2>&1; then \
		echo "Creating kind cluster..."; \
		kind create cluster --name unstuck-test; \
	fi
	$(GOTEST) -v -tags=integration -timeout=15m ./test/e2e/...

## lint: Run linters
lint:
	@echo "Running linters..."
	golangci-lint run --timeout=5m

## fmt: Format code
fmt:
	@echo "Formatting code..."
	$(GOCMD) fmt ./...

## vet: Run go vet
vet:
	@echo "Running go vet..."
	$(GOVET) ./...

## mod-tidy: Tidy go modules
mod-tidy:
	@echo "Tidying modules..."
	$(GOMOD) tidy

## mod-download: Download go modules
mod-download:
	@echo "Downloading modules..."
	$(GOMOD) download

## clean: Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
	@rm -f coverage.out

## test-cluster: Create a kind cluster for testing
test-cluster:
	@echo "Creating kind cluster..."
	kind create cluster --name unstuck-test --image kindest/node:v1.31.0

## test-cluster-delete: Delete the test kind cluster
test-cluster-delete:
	@echo "Deleting kind cluster..."
	kind delete cluster --name unstuck-test

## help: Show this help
help:
	@echo "Available targets:"
	@grep -E '^## ' Makefile | sed 's/## /  /'
