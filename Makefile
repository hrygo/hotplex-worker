# HotPlex Worker Gateway Makefile

# ─── Build parameters ────────────────────────────────────────────────────────────
BINARY_NAME   := hotplex-worker
BUILD_DIR     := bin
MAIN_PATH     := ./cmd/worker
GO_VERSION    := $(shell go version | cut -d' ' -f3)
GOOS          := $(shell go env GOOS)
GOARCH        := $(shell go env GOARCH)
GIT_SHA       := $(shell git rev-parse --short=8 HEAD 2>/dev/null || echo "unknown")
BUILD_TIME    := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS       := -s -w \
	-X main.version=$(GIT_SHA) \
	-X main.buildTime=$(BUILD_TIME) \
	-X main.goVersion=$(GO_VERSION)

# ─── Cross-compile matrix ────────────────────────────────────────────────────────
PLATFORMS     := linux/amd64 darwin/arm64

# ─── Go parameters ───────────────────────────────────────────────────────────────
GOCMD     := go
GOBUILD   := $(GOCMD) build
GOTEST    := $(GOCMD) test
GOFMT     := $(GOCMD) fmt
GOTIDY    := $(GOCMD) mod tidy

# ─── Output files ────────────────────────────────────────────────────────────────
COVERAGE_FILE := coverage.out
HTML_FILE     := coverage.html

# ─── Targets ─────────────────────────────────────────────────────────────────────
.PHONY: all build build-pgo build-all build-clean test test-short lint lint-fix fmt tidy \
	clean run run-dev run-verbose coverage coverage-html version help setup

all: lint test build

.DEFAULT_GOAL := help

# ─── Build ───────────────────────────────────────────────────────────────────────
# Default: auto-detect OS/ARCH, output as hotplex-worker-<os>-<arch>
build: ## Build for current platform
	@echo "Building $(BINARY_NAME) ($(GOOS)/$(GOARCH))..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) -trimpath -ldflags="$(LDFLAGS)" \
		-o $(BUILD_DIR)/$(BINARY_NAME)-$(GOOS)-$(GOARCH) $(MAIN_PATH)

build-pgo: ## Build for current platform with PGO
	@echo "Building $(BINARY_NAME) with PGO ($(GOOS)/$(GOARCH))..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) -trimpath -pgo=auto -ldflags="$(LDFLAGS)" \
		-o $(BUILD_DIR)/$(BINARY_NAME)-$(GOOS)-$(GOARCH) $(MAIN_PATH)

# Cross-compile all platforms from PLATFORMS matrix
build-all: ## Build for all platforms (linux/amd64, darwin/arm64)
	@echo "Cross-compiling for all platforms..."
	@mkdir -p $(BUILD_DIR)
	@$(foreach platform,$(PLATFORMS), \
		$(eval _os=$(word 1,$(subst /, ,$(platform)))) \
		$(eval _arch=$(word 2,$(subst /, ,$(platform)))) \
		echo "  $(_os)/$(_arch)..."; \
		GOOS=$(_os) GOARCH=$(_arch) $(GOBUILD) -trimpath -ldflags="$(LDFLAGS)" \
			-o $(BUILD_DIR)/$(BINARY_NAME)-$(platform) $(MAIN_PATH);)

build-clean: clean build ## Clean + rebuild

# ─── Run ────────────────────────────────────────────────────────────────────────
run: build ## Build and run hotplex-worker
	./$(BUILD_DIR)/$(BINARY_NAME)-$(GOOS)-$(GOARCH)

run-dev: build ## Build and run in dev mode
	./$(BUILD_DIR)/$(BINARY_NAME)-$(GOOS)-$(GOARCH) -dev

run-verbose: build ## Build and run with verbose output
	./$(BUILD_DIR)/$(BINARY_NAME)-$(GOOS)-$(GOARCH) -v

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
	@echo "$(BINARY_NAME) version=$(GIT_SHA) go=$(GO_VERSION) platform=$(GOOS)/$(GOARCH) time=$(BUILD_TIME)"

setup: ## Install development tools
	@echo "Installing tools..."
	$(GOTIDY)
	@if ! command -v golangci-lint > /dev/null 2>&1; then \
		echo "Installing golangci-lint v1.64.8..."; \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh \
			| sh -s -- -b $$(go env GOPATH)/bin v1.64.8; \
	fi

help: ## Display this help screen
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'
