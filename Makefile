# HotPlex Worker Gateway Makefile

# Build parameters
BINARY_NAME=hotplex-worker
BUILD_DIR=bin
MAIN_PATH=./cmd/worker/main.go

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod

# Environment variables
export GO111MODULE=on

.PHONY: all build test clean run setup coverage lint help

all: test build

help: ## Display this help screen
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

setup: ## Install development tools
	@echo "Installing tools..."
	$(GOMOD) download
	@which golangci-lint > /dev/null || (curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(shell go env GOPATH)/bin v1.64.5)

build: ## Build the hotplex-worker binary
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PATH)

test: ## Run unit tests
	@echo "Running unit tests..."
	$(GOTEST) -v -race -timeout 15m ./...

test-short: ## Run short unit tests
	@echo "Running short unit tests..."
	$(GOTEST) -short -v ./...

coverage: ## Generate and view coverage report
	@echo "Generating coverage report..."
	$(GOTEST) -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -func=coverage.out
	@echo "Total coverage:"
	@$(GOCMD) tool cover -func=coverage.out | tail -n 1

coverage-html: coverage ## Generate HTML coverage report
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage HTML report generated at coverage.html"

lint: ## Run golangci-lint
	@echo "Running linter..."
	golangci-lint run ./...

clean: ## Remove build artifacts
	@echo "Cleaning..."
	$(GOCLEAN)
	rm -rf $(BUILD_DIR)
	rm -f coverage.out
	rm -f coverage.html

run: build ## Build and run hotplex-worker
	./$(BUILD_DIR)/$(BINARY_NAME)

run-dev: build ## Build and run hotplex-worker in dev mode
	./$(BUILD_DIR)/$(BINARY_NAME) -dev
