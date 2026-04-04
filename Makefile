# HotPlex Worker Gateway Makefile
#
# Organized by functional groups:
# 1. Build & Compilation
# 2. Test & Quality
# 3. Run & Development
# 4. Process Lifecycle
# 5. Utilities & Maintenance

# ─────────────────────────────────────────────────────────────────────────────
# Build Parameters
# ─────────────────────────────────────────────────────────────────────────────

BINARY_NAME   := hotplex-worker
BUILD_DIR     := bin
MAIN_PATH     := ./cmd/worker
GO_VERSION    := $(shell go version | cut -d' ' -f3)
GOOS          := $(shell go env GOOS)
GOARCH        := $(shell go env GOARCH)
GIT_SHA       := $(shell git rev-parse --short=8 HEAD 2>/dev/null || echo "unknown")
BUILD_TIME    := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS       := -s -w \
	-X main.version=$(GIT_SHA)

# Cross-compile platforms
PLATFORMS     := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

# ─────────────────────────────────────────────────────────────────────────────
# Go Tools
# ─────────────────────────────────────────────────────────────────────────────

GOCMD     := go
GOBUILD   := $(GOCMD) build
GOTEST    := $(GOCMD) test
GOFMT     := $(GOCMD) fmt
GOTIDY    := $(GOCMD) mod tidy
GOVET     := $(GOCMD) vet

# ─────────────────────────────────────────────────────────────────────────────
# Output Files
# ─────────────────────────────────────────────────────────────────────────────

COVERAGE_FILE := coverage.out
HTML_FILE     := coverage.html
PID_FILE      := /tmp/hotplex-worker.pid
LOG_FILE      := logs/hotplex-worker.log
GRACE_PERIOD  := 7  # seconds to wait for graceful shutdown

# ─────────────────────────────────────────────────────────────────────────────
# PHONY Targets (Grouped by functionality)
# ─────────────────────────────────────────────────────────────────────────────

.PHONY: all help

# Build & Compilation
.PHONY: build build-pgo build-all build-clean build-docker

# Test & Quality
.PHONY: test test-short test-integration test-race coverage coverage-html lint lint-fix fmt vet tidy

# Run & Development
.PHONY: run run-dev run-prod run-verbose run-config

# Process Lifecycle
.PHONY: start stop restart status reload graceful-shutdown force-kill logs tail

# Utilities & Maintenance
.PHONY: clean version setup install uninstall check-deps

# ─────────────────────────────────────────────────────────────────────────────
# Default Target
# ─────────────────────────────────────────────────────────────────────────────

.DEFAULT_GOAL := help

all: lint test build

# ─────────────────────────────────────────────────────────────────────────────
# 1. BUILD & COMPILATION
# ─────────────────────────────────────────────────────────────────────────────

build: ## Build for current platform
	@echo "🔨 Building $(BINARY_NAME) ($(GOOS)/$(GOARCH))..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) -trimpath -ldflags="$(LDFLAGS)" \
		-o $(BUILD_DIR)/$(BINARY_NAME)-$(GOOS)-$(GOARCH) $(MAIN_PATH)
	@echo "✅ Built: $(BUILD_DIR)/$(BINARY_NAME)-$(GOOS)-$(GOARCH)"

build-pgo: ## Build with Profile-Guided Optimization (PGO)
	@echo "🔨 Building $(BINARY_NAME) with PGO ($(GOOS)/$(GOARCH))..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) -trimpath -pgo=auto -ldflags="$(LDFLAGS)" \
		-o $(BUILD_DIR)/$(BINARY_NAME)-$(GOOS)-$(GOARCH) $(MAIN_PATH)
	@echo "✅ Built with PGO: $(BUILD_DIR)/$(BINARY_NAME)-$(GOOS)-$(GOARCH)"

build-all: ## Build for all platforms (linux/amd64, linux/arm64, darwin/amd64, darwin/arm64)
	@echo "🔨 Cross-compiling for all platforms..."
	@mkdir -p $(BUILD_DIR)
	@$(foreach platform,$(PLATFORMS), \
		$(eval _os=$(word 1,$(subst /, ,$(platform)))) \
		$(eval _arch=$(word 2,$(subst /, ,$(platform)))) \
		echo "  📦 $(_os)/$(_arch)..."; \
		GOOS=$(_os) GOARCH=$(_arch) $(GOBUILD) -trimpath -ldflags="$(LDFLAGS)" \
			-o $(BUILD_DIR)/$(BINARY_NAME)-$(_os)-$(_arch) $(MAIN_PATH);)
	@echo "✅ All platforms built in $(BUILD_DIR)/"

build-clean: clean build ## Clean rebuild

build-docker: ## Build Docker image
	@echo "🐳 Building Docker image..."
	./scripts/docker-build.sh
	@echo "✅ Docker image built: hotplex-worker:latest"

# ─────────────────────────────────────────────────────────────────────────────
# 2. TEST & QUALITY
# ─────────────────────────────────────────────────────────────────────────────

test: ## Run all tests with race detection
	@echo "🧪 Running all tests..."
	$(GOTEST) -race -timeout 15m ./...
	@echo "✅ Tests passed"

test-short: ## Run short tests (skip slow integration tests)
	@echo "🧪 Running short tests..."
	$(GOTEST) -short -race -timeout 5m ./...
	@echo "✅ Short tests passed"

test-integration: ## Run integration tests (requires Docker)
	@echo "🧪 Running integration tests..."
	$(GOTEST) -run Integration -timeout 20m ./...
	@echo "✅ Integration tests passed"

test-race: test ## Alias for 'test' (race detection enabled by default)

coverage: ## Generate coverage report
	@echo "📊 Generating coverage report..."
	$(GOTEST) -coverprofile=$(COVERAGE_FILE) -timeout 15m ./...
	$(GOCMD) tool cover -func=$(COVERAGE_FILE)
	@echo "---"
	@$(GOCMD) tool cover -func=$(COVERAGE_FILE) | tail -n 1

coverage-html: ## Generate HTML coverage report
	@echo "📊 Generating HTML coverage report..."
	$(GOTEST) -coverprofile=$(COVERAGE_FILE) -timeout 15m ./...
	$(GOCMD) tool cover -html=$(COVERAGE_FILE) -o $(HTML_FILE)
	@echo "✅ Coverage report: $(HTML_FILE)"

lint: ## Run golangci-lint
	@echo "🔍 Running linter..."
	golangci-lint run ./...
	@echo "✅ Linting passed"

lint-fix: ## Run golangci-lint with auto-fix
	@echo "🔧 Running linter with auto-fix..."
	golangci-lint run --fix ./...
	@echo "✅ Auto-fix applied"

fmt: ## Format code (go fmt + goimports)
	@echo "✨ Formatting code..."
	$(GOFMT) ./...
	@command -v goimports > /dev/null && goimports -w . || true
	@echo "✅ Code formatted"

vet: ## Run go vet
	@echo "🔍 Running go vet..."
	$(GOVET) ./...
	@echo "✅ Vet passed"

tidy: ## Tidy go.mod
	@echo "📦 Tidying modules..."
	$(GOTIDY)
	@echo "✅ Modules tidied"

# ─────────────────────────────────────────────────────────────────────────────
# 3. RUN & DEVELOPMENT
# ─────────────────────────────────────────────────────────────────────────────

run: build ## Build and run with defaults
	@echo "🚀 Starting $(BINARY_NAME)..."
	./$(BUILD_DIR)/$(BINARY_NAME)-$(GOOS)-$(GOARCH)

run-dev: build ## Run in development mode (relaxed security)
	@echo "🚀 Starting $(BINARY_NAME) in dev mode..."
	./$(BUILD_DIR)/$(BINARY_NAME)-$(GOOS)-$(GOARCH) -dev

run-prod: build ## Run with production config
	@echo "🚀 Starting $(BINARY_NAME) in production mode..."
	./$(BUILD_DIR)/$(BINARY_NAME)-$(GOOS)-$(GOARCH) -config configs/config-prod.yaml

run-verbose: build ## Run with verbose logging
	@echo "🚀 Starting $(BINARY_NAME) with verbose logging..."
	./$(BUILD_DIR)/$(BINARY_NAME)-$(GOOS)-$(GOARCH) -v

run-config: build ## Run with custom config (make run-config CONFIG=/path/to/config.yaml)
	@echo "🚀 Starting $(BINARY_NAME) with custom config..."
	./$(BUILD_DIR)/$(BINARY_NAME)-$(GOOS)-$(GOARCH) -config $(or CONFIG,configs/config.yaml)

# ─────────────────────────────────────────────────────────────────────────────
# 4. PROCESS LIFECYCLE
# ─────────────────────────────────────────────────────────────────────────────

start: build ## Start as background process (writes PID to /tmp)
	@echo "🟢 Starting $(BINARY_NAME) as background process..."
	@mkdir -p logs
	./$(BUILD_DIR)/$(BINARY_NAME)-$(GOOS)-$(GOARCH) -config $(or $(CONFIG),configs/config.yaml) > $(LOG_FILE) 2>&1 & echo $$! > $(PID_FILE)
	@echo "✅ Started (PID: $$(cat $(PID_FILE)))"
	@echo "📋 Logs: $(LOG_FILE)"
	@echo "📊 Status: make status"

stop: ## Stop background process gracefully (SIGTERM)
	@if [ -f $(PID_FILE) ]; then \
		PID=$$(cat $(PID_FILE)); \
		if kill -0 $$PID 2>/dev/null; then \
			echo "🟡 Sending SIGTERM to process $$PID..."; \
			kill -TERM $$PID; \
			for i in $$(seq 1 $(GRACE_PERIOD)); do \
				sleep 1; \
				if ! kill -0 $$PID 2>/dev/null; then \
					echo "✅ Process stopped gracefully after $$i seconds"; \
					rm -f $(PID_FILE); \
					exit 0; \
				fi; \
			done; \
			echo "⚠️  Process still running after $(GRACE_PERIOD) seconds, use 'make force-kill' to force stop"; \
		else \
			echo "✅ Process not running"; \
			rm -f $(PID_FILE); \
		fi \
	else \
		echo "❌ PID file not found: $(PID_FILE)"; \
		echo "💡 Is the process running? Use 'make status' to check"; \
	fi

restart: stop start ## Restart service (graceful stop + start)
	@echo "🔄 Service restarted"

status: ## Check if process is running
	@if [ -f $(PID_FILE) ]; then \
		PID=$$(cat $(PID_FILE)); \
		if kill -0 $$PID 2>/dev/null; then \
			echo "🟢 Process running (PID: $$PID)"; \
			echo "📊 Memory: $$(ps -o rss= -p $$PID | tail -1 | awk '{print $$1/1024 " MB"}')"; \
			echo "⏱️  CPU: $$(ps -o %cpu= -p $$PID | tail -1)%"; \
			echo "📁  Logs: $(LOG_FILE)"; \
		else \
			echo "🔴 Process not running (stale PID: $$PID)"; \
			rm -f $(PID_FILE); \
		fi \
	else \
		echo "⚪ No PID file found"; \
		echo "💡 Start with: make start"; \
	fi

reload: ## Reload configuration (SIGHUP - hot reload config)
	@if [ -f $(PID_FILE) ]; then \
		PID=$$(cat $(PID_FILE)); \
		if kill -0 $$PID 2>/dev/null; then \
			echo "🔄 Sending SIGHUP to reload config..."; \
			kill -HUP $$PID; \
			echo "✅ Config reload signal sent"; \
			echo "📋 Check logs: make tail"; \
		else \
			echo "❌ Process not running"; \
		fi \
	else \
		echo "❌ PID file not found"; \
	fi

graceful-shutdown: stop ## Alias for 'stop' (graceful shutdown with SIGTERM)

force-kill: ## Force kill process (SIGKILL - data loss possible!)
	@if [ -f $(PID_FILE) ]; then \
		PID=$$(cat $(PID_FILE)); \
		echo "⚠️  Force killing process $$PID (SIGKILL)..."; \
		kill -9 $$PID 2>/dev/null || true; \
		rm -f $(PID_FILE); \
		echo "💀 Process force killed"; \
		echo "⚠️  Warning: Data loss possible!"; \
	else \
		echo "❌ PID file not found"; \
	fi

logs: ## Show all logs
	@if [ -f $(LOG_FILE) ]; then \
		cat $(LOG_FILE); \
	else \
		echo "❌ Log file not found: $(LOG_FILE)"; \
	fi

tail: ## Tail logs (follow live output)
	@if [ -f $(LOG_FILE) ]; then \
		echo "📋 Tailing logs (Ctrl+C to stop)..."; \
		tail -f $(LOG_FILE); \
	else \
		echo "❌ Log file not found: $(LOG_FILE)"; \
		echo "💡 Start with: make start"; \
	fi

# ─────────────────────────────────────────────────────────────────────────────
# 5. UTILITIES & MAINTENANCE
# ─────────────────────────────────────────────────────────────────────────────

clean: ## Remove build artifacts and temporary files
	@echo "🧹 Cleaning..."
	$(GOCMD) clean
	rm -rf $(BUILD_DIR)
	rm -f worker gateway
	rm -f *.db*
	rm -f $(COVERAGE_FILE) $(HTML_FILE)
	rm -f $(PID_FILE)
	@echo "✅ Cleaned"

version: ## Print version info
	@echo "$(BINARY_NAME) version=$(GIT_SHA) go=$(GO_VERSION) platform=$(GOOS)/$(GOARCH) time=$(BUILD_TIME)"

setup: ## Install development tools (golangci-lint, goimports)
	@echo "📦 Setting up development tools..."
	$(GOTIDY)
	@if ! command -v golangci-lint > /dev/null 2>&1; then \
		echo "Installing golangci-lint v1.64.8..."; \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh \
			| sh -s -- -b $$(go env GOPATH)/bin v1.64.8; \
	fi
	@if ! command -v goimports > /dev/null 2>&1; then \
		echo "Installing goimports..."; \
		$(GOCMD) install golang.org/x/tools/cmd/goimports@latest; \
	fi
	@echo "✅ Development tools installed"

install: build ## Install system-wide (requires sudo)
	@echo "📦 Installing $(BINARY_NAME)..."
	@if [ $$USER != "root" ]; then \
		echo "❌ This command requires sudo"; \
		echo "💡 Run: sudo make install"; \
		exit 1; \
	fi
	./scripts/install.sh --non-interactive
	@echo "✅ Installed to /usr/local/bin/$(BINARY_NAME)"

uninstall: ## Uninstall system-wide (requires sudo)
	@echo "🗑️  Uninstalling $(BINARY_NAME)..."
	@if [ $$USER != "root" ]; then \
		echo "❌ This command requires sudo"; \
		echo "💡 Run: sudo make uninstall"; \
		exit 1; \
	fi
	./scripts/uninstall.sh --non-interactive
	@echo "✅ Uninstalled"

check-deps: ## Check dependencies and Go version
	@echo "🔍 Checking dependencies..."
	@echo "Go version: $$(go version)"
	@echo "Go path: $$(go env GOPATH)"
	@echo "Platform: $(GOOS)/$(GOARCH)"
	@$(GOCMD) mod verify
	@echo "✅ Dependencies verified"

# ─────────────────────────────────────────────────────────────────────────────
# HELP
# ─────────────────────────────────────────────────────────────────────────────

help: ## Display this help screen
	@echo "HotPlex Worker Gateway - Makefile Commands"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Groups:"
	@echo ""
	@echo "  BUILD & COMPILATION:"
	@grep -E '^build[^-]|^build-'  $(MAKEFILE_LIST) | grep '##' | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "    \033[36m%-20s\033[0m %s\n", $$1, $$2}'
	@echo ""
	@echo "  TEST & QUALITY:"
	@grep -E '^test|^coverage|^lint|^fmt|^vet|^tidy'  $(MAKEFILE_LIST) | grep '##' | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "    \033[36m%-20s\033[0m %s\n", $$1, $$2}'
	@echo ""
	@echo "  RUN & DEVELOPMENT:"
	@grep -E '^run'  $(MAKEFILE_LIST) | grep '##' | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "    \033[36m%-20s\033[0m %s\n", $$1, $$2}'
	@echo ""
	@echo "  PROCESS LIFECYCLE:"
	@grep -E '^start|^stop|^restart|^status|^reload|^graceful|^force|^logs|^tail'  $(MAKEFILE_LIST) | grep '##' | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "    \033[36m%-20s\033[0m %s\n", $$1, $$2}'
	@echo ""
	@echo "  UTILITIES & MAINTENANCE:"
	@grep -E '^clean|^version|^setup|^install|^uninstall|^check'  $(MAKEFILE_LIST) | grep '##' | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "    \033[36m%-20s\033[0m %s\n", $$1, $$2}'
	@echo ""
	@echo "Examples:"
	@echo "  make build          # Build for current platform"
	@echo "  make test           # Run tests with race detection"
	@echo "  make start          # Start as background process"
	@echo "  make tail           # Follow logs"
	@echo "  make stop           # Graceful shutdown"
	@echo "  make help           # Show this help"
