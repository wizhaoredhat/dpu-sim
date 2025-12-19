.PHONY: build test clean install lint fmt vet help

# Binary names
BINARIES = dpu-sim vmctl

# Build directory
BUILD_DIR = bin

# Go parameters
GOCMD = go
GOBUILD = $(GOCMD) build
GOTEST = $(GOCMD) test
GOCLEAN = $(GOCMD) clean
GOINSTALL = $(GOCMD) install
GOFMT = $(GOCMD) fmt
GOVET = $(GOCMD) vet

# Build all binaries
build: ## Build all binaries
	@echo "Building binaries..."
	@mkdir -p $(BUILD_DIR)
	@for bin in $(BINARIES); do \
		echo "  Building $$bin..."; \
		$(GOBUILD) -o $(BUILD_DIR)/$$bin ./cmd/$$bin; \
	done
	@echo "Build complete! Binaries are in $(BUILD_DIR)/"

# Run tests
test: ## Run tests
	@echo "Running tests..."
	$(GOTEST) -v -race -coverprofile=coverage.out ./...
	@echo "Tests complete!"

# Run tests with coverage report
test-coverage: test ## Run tests and show coverage
	$(GOCMD) tool cover -html=coverage.out

# Run integration tests (requires libvirt/docker)
test-integration: ## Run integration tests
	$(GOTEST) -v -tags=integration ./...

# Clean build artifacts
clean: ## Clean build artifacts
	@echo "Cleaning..."
	$(GOCLEAN)
	rm -rf $(BUILD_DIR)
	rm -f coverage.out
	@echo "Clean complete!"

# Install binaries to $GOPATH/bin
install: ## Install binaries to $GOPATH/bin
	@echo "Installing binaries..."
	@for bin in $(BINARIES); do \
		echo "  Installing $$bin..."; \
		$(GOINSTALL) ./cmd/$$bin; \
	done
	@echo "Install complete!"

# Format code
fmt: ## Format code
	@echo "Formatting code..."
	$(GOFMT) ./...
	@echo "Format complete!"

# Run go vet
vet: ## Run go vet
	@echo "Running go vet..."
	$(GOVET) ./...
	@echo "Vet complete!"

# Run linter (requires golangci-lint)
lint: ## Run golangci-lint
	@echo "Running linter..."
	@if command -v golangci-lint > /dev/null; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed. Install from: https://golangci-lint.run/usage/install/"; \
		exit 1; \
	fi

# Run all checks
check: fmt vet test ## Run all checks (fmt, vet, test)

# Cross-compile for multiple platforms
build-all: ## Cross-compile for Linux (amd64, arm64) and macOS
	@echo "Cross-compiling binaries..."
	@mkdir -p $(BUILD_DIR)
	@for bin in $(BINARIES); do \
		echo "  Building $$bin for linux/amd64..."; \
		GOOS=linux GOARCH=amd64 $(GOBUILD) -o $(BUILD_DIR)/$$bin-linux-amd64 ./cmd/$$bin; \
		echo "  Building $$bin for linux/arm64..."; \
		GOOS=linux GOARCH=arm64 $(GOBUILD) -o $(BUILD_DIR)/$$bin-linux-arm64 ./cmd/$$bin; \
		echo "  Building $$bin for darwin/amd64..."; \
		GOOS=darwin GOARCH=amd64 $(GOBUILD) -o $(BUILD_DIR)/$$bin-darwin-amd64 ./cmd/$$bin; \
		echo "  Building $$bin for darwin/arm64..."; \
		GOOS=darwin GOARCH=arm64 $(GOBUILD) -o $(BUILD_DIR)/$$bin-darwin-arm64 ./cmd/$$bin; \
	done
	@echo "Cross-compile complete!"

# Download dependencies
deps: ## Download dependencies
	@echo "Downloading dependencies..."
	$(GOCMD) mod download
	$(GOCMD) mod tidy
	@echo "Dependencies downloaded!"

# Display help
help: ## Display this help message
	@echo "DPU Simulator - Makefile commands:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

# Default target
.DEFAULT_GOAL := help
