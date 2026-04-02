# HotPlex Worker Gateway Makefile

# ─── Build parameters ────────────────────────────────────────────────────────────
BINARY_NAME   := hotplex-worker
BUILD_DIR     := bin
MAIN_PATH     := ./cmd/worker
GO_VERSION    := $(shell go version | cut -d' ' -f3)
GIT_SHA       := $(shell git rev-parse --short=8 HEAD 2>/dev/null || echo "unknown")
BUILD_TIME    := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS       := -s -w \
	-X main.version=$(GIT_SHA) \
	-X main.buildTime=$(BUILD_TIME) \
	-X main.goVersion=$(GO_VERSION)

# ─── Go parameters ───────────────────────────────────────────────────────────────
GOCMD     := go
GOBUILD   := $(GOCMD) build
GOTEST    := $(GOCMD) test
GOFMT     := $(GOCMD) fmt
GOTIDY    := $(GOCMD) mod tidy
GOMOD     := $(GOCMD) mod

# ─── Directories ─────────────────────────────────────────────────────────────────
COVERAGE_FILE := coverage.out
HTML_FILE     := coverage.html

# ─── Targets ─────────────────────────────────────────────────────────────────────
.PHONY: all build build-pgo build-clean test test-short lint lint-fix fmt tidy \
	clean run run-dev run-verbose coverage coverage-html \
	build-linux build-darwin build-all \
	version help setup

# all should be lint+test+build in CI, but just build locally
all: lint test build

# ─── Build ───────────────────────────────────────────────────────────────────────
build: ## Build hotplex-worker (default)
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) -trimpath -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PATH)

build-pgo: ## Build with PGO optimization
	@echo "Building $(BINARY_NAME) with PGO..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) -trimpath -pgo=auto -ldflags="$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PATH)

# Cross-compile targets
build-linux: ## Build for linux/amd64
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 $(GOBUILD) -trimpath -ldflags="$(LDFLAGS)" \
		-o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 $(MAIN_PATH)

build-darwin: ## Build for darwin/arm64 (Apple Silicon)
	@mkdir -p $(BUILD_DIR)
	GOOS=darwin GOARCH=arm64 $(GOBUILD) -trimpath -ldflags="$(LDFLAGS)" \
		-o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 $(MAIN_PATH)

build-all: build build-pgo build-linux build-darwin ## Build all platforms

build-clean: clean build ## Clean + rebuild

# ─── Run ────────────────────────────────────────────────────────────────────────
run: build ## Build and run hotplex-worker
	./$(BUILD_DIR)/$(BINARY_NAME)

run-dev: build ## Build and run in dev mode
	./$(BUILD_DIR)/$(BINARY_NAME) -dev

run-verbose: build ## Build and run with verbose output
	./$(BUILD_DIR)/$(BINARY_NAME) -v

# ─── Test ────────────────────────────────────────────────────────────────────────
test: ## Run all tests with race detection
	@echo "Running tests..."
	$(GOTEST) -race -timeout 15m ./...

test-short: ## Run short tests (skip integration)
	@echo "Running short tests..."
	$(GOTEST) -short -race -timeout 5m ./...

# ─── Lint ────────────────────────────────────────────────────────────────────────
lint: ## Run golangci-lint
	@echo "Running linter..."
	golangci-lint run ./...

lint-fix: ## Run golangci-lint with auto-fix
	@echo "Running linter with auto-fix..."
	golangci-lint run --fix ./...

# ─── Format ───────────────────────────────────────────────────────────────────────
fmt: ## Format code (go fmt + goimports)
	@echo "Formatting code..."
	$(GOFMT) ./...
	@command -v goimports > /dev/null && goimports -w . || true

tidy: ## Tidy go.mod
	@echo "Tidying modules..."
	$(GOTIDY)

# ─── Coverage ────────────────────────────────────────────────────────────────────
coverage: ## Generate and view coverage report
	@echo "Generating coverage report..."
	$(GOTEST) -coverprofile=$(COVERAGE_FILE) -timeout 15m ./...
	$(GOCMD) tool cover -func=$(COVERAGE_FILE)
	@echo "---"
	@$(GOCMD) tool cover -func=$(COVERAGE_FILE) | tail -n 1

coverage-html: ## Generate HTML coverage report
	$(GOCMD) tool cover -html=$(COVERAGE_FILE) -o $(HTML_FILE)
	@echo "Coverage report: $(HTML_FILE)"

# ─── Clean ───────────────────────────────────────────────────────────────────────
clean: ## Remove build artifacts
	@echo "Cleaning..."
	$(GOCMD) clean
	rm -rf $(BUILD_DIR)
	rm -f $(COVERAGE_FILE) $(HTML_FILE)

# ─── Misc ────────────────────────────────────────────────────────────────────────
version: ## Print version info
	@echo "$(BINARY_NAME) version=$(GIT_SHA) go=$(GO_VERSION) time=$(BUILD_TIME)"

setup: ## Install development tools
	@echo "Installing tools..."
	$(GOMOD) download
	@if ! command -v golangci-lint > /dev/null 2>&1; then \
		echo "Installing golangci-lint v1.64.8..."; \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh \
			| sh -s -- -b $$(go env GOPATH)/bin v1.64.8; \
	fi

help: ## Display this help screen
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'
