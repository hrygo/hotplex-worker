# ════════════════════════════════════════════════════════════════════════════
# HotPlex Worker Gateway - Premium Makefile
# ════════════════════════════════════════════════════════════════════════════
#
# 🚀 Quick Start:
#   make              Show interactive help
#   make quickstart   First-time setup (5 min)
#   make dev          Start development environment
#   make check        Full quality check (lint + test + build)
#
# 💡 Pro Tips:
#   make <tab>        Tab completion for targets
#   make help-<query> Search commands (e.g., make help-test)
#   make watch        Auto-rebuild on file changes
#
# ════════════════════════════════════════════════════════════════════════════

# ─────────────────────────────────────────────────────────────────────────────
# 🎯 Core Configuration
# ─────────────────────────────────────────────────────────────────────────────

BINARY_NAME    := hotplex-worker
BUILD_DIR      := bin
MAIN_PATH      := ./cmd/worker
CONFIG_DIR     := configs
LOG_DIR        := logs

# Version info
GO_VERSION     := $(shell go version | cut -d' ' -f3)
GOOS           := $(shell go env GOOS)
GOARCH         := $(shell go env GOARCH)
GIT_SHA        := $(shell git rev-parse --short=8 HEAD 2>/dev/null || echo "unknown")
GIT_BRANCH     := $(shell git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "unknown")
BUILD_TIME     := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

# Build configuration
PLATFORMS      := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64
LDFLAGS        := -s -w -X main.version=$(GIT_SHA) -X main.buildTime=$(BUILD_TIME)
BUILD_OPTS     := -trimpath

# ─────────────────────────────────────────────────────────────────────────────
# 🔧 Go Tools
# ─────────────────────────────────────────────────────────────────────────────

GO            := go
GOBUILD       := $(GO) build
GOTEST        := $(GO) test
GOFMT         := $(GO) fmt
GOTIDY        := $(GO) mod tidy
GOVET         := $(GO) vet
GOMOD         := $(GO) mod
GOINSTALL     := $(GO) install

# ─────────────────────────────────────────────────────────────────────────────
# 📁 File Paths
# ─────────────────────────────────────────────────────────────────────────────

COVERAGE_FILE  := coverage.out
COVERAGE_HTML  := coverage.html
TESTDB_PATTERN := *.db*

WORKER_PID     := $(HOME)/.hotplex/.pid/hotplex-worker.pid
WORKER_LOG     := $(LOG_DIR)/hotplex-worker.log
WORKER_SOCK    := /tmp/hotplex-worker.sock

WEB_CHAT_DIR   := webchat
WEB_CHAT_PID   := $(HOME)/.hotplex/.pid/hotplex-webchat.pid
WEB_CHAT_PORT  := 3000
WEB_CHAT_LOG   := $(LOG_DIR)/webchat.log

# ─────────────────────────────────────────────────────────────────────────────
# 🎨 Premium Color Palette & Symbols
# ─────────────────────────────────────────────────────────────────────────────

# ANSI Colors
RESET          := \033[0m
BOLD           := \033[1m
DIM            := \033[2m
UNDERLINE      := \033[4m

# Text Colors
BLACK          := \033[30m
RED            := \033[31m
GREEN          := \033[32m
YELLOW         := \033[33m
BLUE           := \033[34m
MAGENTA        := \033[35m
CYAN           := \033[36m
WHITE          := \033[37m

# Background Colors
BG_RED         := \033[41m
BG_GREEN       := \033[42m
BG_YELLOW      := \033[43m
BG_BLUE        := \033[44m
BG_CYAN        := \033[46m

# Premium Symbols (Nerd Font compatible)
# Fallback to ASCII if symbols don't render
OK             := ✅
ERR            := ❌
WARN           := ⚠️
INFO           := ℹ️
IDEA           := 💡
ROCKET         := 🚀
STOP           := 🛑
BUILD          := 🔨
TEST           := 🧪
CLEAN          := 🧹
LOGS           := 📋
PACKAGE        := 📦
TOOL           := 🔧
EAR            := 👂
CHART          := 📊
GEAR           := ⚙️
MAG            := 🔍
SPEED          := ⚡
TARGET         := 🎯
STAR           := ⭐
SPARKLE        := ✨
ART            := 🎨
LOCK           := 🔒
KEY            := 🔑
BOOK           := 📖
MENU           := ☰
PLAY           := ▶️
PAUSE          := ⏸️
FAST           := ⏩
SLOW           := ⏪
ARROW_RIGHT    := →
ARROW_DOWN     := ↓
CHECK          := ✓
CROSS          := ✗
BULLET         := •
DOT            := ‣

# ════════════════════════════════════════════════════════════════════════════
# 🎯 PHONY Targets Declaration
# ════════════════════════════════════════════════════════════════════════════

# Main targets
.PHONY: all help help-full check quickstart

# Development
.PHONY: dev dev-start dev-stop dev-status dev-logs dev-reset watch

# Build
.PHONY: build build-pgo build-all build-clean build-docker build-optimize

# Test & Quality
.PHONY: test test-short test-integration test-e2e test-verbose test-race
.PHONY: coverage coverage-html coverage-func benchmark
.PHONY: lint lint-fix lint-verbose fmt fmt-check vet tidy quality

# Run modes
.PHONY: run run-dev run-prod run-verbose run-config run-debug

# Lifecycle
.PHONY: start stop restart status reload force-kill logs tail
.PHONY: worker-start worker-stop worker-status worker-logs worker-tail worker-restart

# Web chat
.PHONY: webchat-install webchat-build webchat-dev webchat-start
.PHONY: webchat-stop webchat-status webchat-logs webchat-tail webchat-clean

# Utilities
.PHONY: clean clean-all clean-logs clean-cache reset
.PHONY: version info env health check-deps check-tools
.PHONY: setup install uninstall update upgrade
.PHONY: snapshot restore config-edit config-validate

# Help system
.PHONY: help-dev help-test help-build help-run help-web help-advanced

# ════════════════════════════════════════════════════════════════════════════
# 🚀 Default Target
# ════════════════════════════════════════════════════════════════════════════

all: help

# ════════════════════════════════════════════════════════════════════════════
# 🎓 Quick Start & Setup
# ════════════════════════════════════════════════════════════════════════════

quickstart: ## 🚀 First-time setup (install tools + build + verify)
	@echo "$(CYAN)$(BOLD)╔═══════════════════════════════════════════════════════════╗$(RESET)"
	@echo "$(CYAN)$(BOLD)║         🚀 HotPlex Worker - Quick Start (5 min)          ║$(RESET)"
	@echo "$(CYAN)$(BOLD)╚═══════════════════════════════════════════════════════════╝$(RESET)"
	@echo ""
	@$(MAKE) --no-print-directory check-tools || $(MAKE) --no-print-directory setup
	@echo ""
	@$(MAKE) --no-print-directory build
	@echo ""
	@$(MAKE) --no-print-directory test-short
	@echo ""
	@echo "$(GREEN)$(BOLD)$(SPARKLE) Setup Complete!$(RESET)"
	@echo ""
	@echo "$(BOLD)Next Steps:$(RESET)"
	@echo "  $(CYAN)make dev$(RESET)       Start development environment"
	@echo "  $(CYAN)make run$(RESET)       Run gateway directly"
	@echo "  $(CYAN)make help$(RESET)      Explore all commands"
	@echo ""

setup: ## 🔧 Install development tools (golangci-lint, goimports, etc.)
	@echo "$(CYAN)$(GEAR) Setting up development tools...$(RESET)"
	@$(GOTIDY)
	@echo ""
	@$(call install-tool, golangci-lint, "curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin v1.64.8")
	@$(call install-tool, goimports, "$(GOINSTALL) golang.org/x/tools/cmd/goimports@latest")
	@$(call install-tool, gopls, "$(GOINSTALL) golang.org/x/tools/gopls@latest")
	@echo ""
	@echo "$(GREEN)$(OK) All tools installed$(RESET)"

# Helper function to install tools
define install-tool
	@if ! command -v $(1) > /dev/null 2>&1; then \
		echo "$(YELLOW)$(PACKAGE) Installing $(1)...$(RESET)"; \
		$(2); \
		echo "$(GREEN)$(OK) $(1) installed$(RESET)"; \
	else \
		echo "$(DIM)$(OK) $(1) already installed$(RESET)"; \
	fi
endef

# ════════════════════════════════════════════════════════════════════════════
# 🔍 Environment & Health Checks
# ════════════════════════════════════════════════════════════════════════════

check-tools: ## 🔍 Verify all required tools are installed
	@echo "$(CYAN)$(MAG) Checking development tools...$(RESET)"
	@$(call check-tool, go, "Go 1.26+")
	@$(call check-tool, golangci-lint, "Linter")
	@$(call check-tool, goimports, "Import formatter")
	@$(call check-tool, gopls, "Language server")
	@echo "$(GREEN)$(OK) All tools available$(RESET)"

define check-tool
	@if command -v $(1) > /dev/null 2>&1; then \
		echo "  $(GREEN)$(CHECK)$(RESET) $(2)"; \
	else \
		echo "  $(RED)$(CROSS)$(RESET) $(2) $(DIM)(missing)$(RESET)"; \
		exit 1; \
	fi
endef

check-deps: ## 🔍 Verify Go module dependencies
	@echo "$(CYAN)$(PACKAGE) Checking dependencies...$(RESET)"
	@$(GOMOD) verify
	@echo "$(GREEN)$(OK) Dependencies verified$(RESET)"

health: ## 🏥 Run comprehensive health check
	@echo "$(CYAN)$(BOLD)═══════════════════════════════════════════$(RESET)"
	@echo "$(CYAN)$(BOLD)         🏥 System Health Check             $(RESET)"
	@echo "$(CYAN)$(BOLD)═══════════════════════════════════════════$(RESET)"
	@echo ""
	@echo "$(BOLD)Environment:$(RESET)"
	@echo "  $(BULLET) Go:         $(GREEN)$(GO_VERSION)$(RESET)"
	@echo "  $(BULLET) Platform:   $(GREEN)$(GOOS)/$(GOARCH)$(RESET)"
	@echo "  $(BULLET) Git:        $(GREEN)$(GIT_BRANCH)@$(GIT_SHA)$(RESET)"
	@echo "  $(BULLET) Built:      $(GREEN)$(BUILD_TIME)$(RESET)"
	@echo ""
	@echo "$(BOLD)Services:$(RESET)"
	@-$(MAKE) --no-print-directory worker-status 2>/dev/null || echo "  $(DIM)$(BULLET) Gateway: not running$(RESET)"
	@-$(MAKE) --no-print-directory webchat-status 2>/dev/null || echo "  $(DIM)$(BULLET) Web-chat: not running$(RESET)"
	@echo ""
	@echo "$(BOLD)Resources:$(RESET)"
	@echo "  $(BULLET) Disk:       $(shell df -h . | tail -1 | awk '{print $$4}') available"
	@echo "  $(BULLET) Memory:     $(shell vm_stat | head -5 | grep 'free' | awk '{print $$2}' | sed 's/\.//') pages free"
	@echo ""
	@$(MAKE) --no-print-directory check-deps

info: ## ℹ️ Show detailed system information
	@echo "$(CYAN)$(BOLD)═══════════════════════════════════════════$(RESET)"
	@echo "$(CYAN)$(BOLD)         ℹ️  System Information              $(RESET)"
	@echo "$(CYAN)$(BOLD)═══════════════════════════════════════════$(RESET)"
	@echo ""
	@echo "$(BOLD)Project:$(RESET)"
	@echo "  $(BULLET) Name:       $(BINARY_NAME)"
	@echo "  $(BULLET) Version:    $(GIT_SHA)"
	@echo "  $(BULLET) Branch:     $(GIT_BRANCH)"
	@echo "  $(BULLET) Built:      $(BUILD_TIME)"
	@echo ""
	@echo "$(BOLD)Go Environment:$(RESET)"
	@$(GO) env GOVERSION GOOS GOARCH GOROOT GOPATH GOMODCACHE
	@echo ""
	@echo "$(BOLD)Build Configuration:$(RESET)"
	@echo "  $(BULLET) Output:     $(BUILD_DIR)/$(BINARY_NAME)-$(GOOS)-$(GOARCH)"
	@echo "  $(BULLET) LDFLAGS:    $(LDFLAGS)"
	@echo ""

env: ## 🌍 Show environment variables
	@echo "$(CYAN)$(BOLD)Environment Variables:$(RESET)"
	@$(GO) env

# ════════════════════════════════════════════════════════════════════════════
# 🛠️  Build System
# ════════════════════════════════════════════════════════════════════════════

build: ## 🔨 Build gateway binary (optimized)
	@echo "$(CYAN)$(BUILD) Building $(BINARY_NAME) ($(GOOS)/$(GOARCH))...$(RESET)"
	@mkdir -p $(BUILD_DIR) $(CURDIR)/$(LOG_DIR)
	@$(GOBUILD) $(BUILD_OPTS) -ldflags="$(LDFLAGS)" \
		-o $(BUILD_DIR)/$(BINARY_NAME)-$(GOOS)-$(GOARCH) $(MAIN_PATH)
	@echo "$(GREEN)$(OK) Built: $(BUILD_DIR)/$(BINARY_NAME)-$(GOOS)-$(GOARCH)$(RESET)"
	@ls -lh $(BUILD_DIR)/$(BINARY_NAME)-$(GOOS)-$(GOARCH) | awk '{print "  $(DIM)Size: " $$5 "$(RESET)"}'

build-pgo: ## ⚡ Build with Profile-Guided Optimization
	@echo "$(CYAN)$(SPEED) Building with PGO optimization...$(RESET)"
	@mkdir -p $(BUILD_DIR)
	@$(GOBUILD) $(BUILD_OPTS) -pgo=auto -ldflags="$(LDFLAGS)" \
		-o $(BUILD_DIR)/$(BINARY_NAME)-$(GOOS)-$(GOARCH) $(MAIN_PATH)
	@echo "$(GREEN)$(OK) PGO-optimized binary built$(RESET)"

build-optimize: ## 🎯 Build with maximum optimization (slower compile)
	@echo "$(CYAN)$(TARGET) Building with maximum optimization...$(RESET)"
	@mkdir -p $(BUILD_DIR)
	@$(GOBUILD) $(BUILD_OPTS) -ldflags="$(LDFLAGS)" -gcflags="-l=4" \
		-o $(BUILD_DIR)/$(BINARY_NAME)-$(GOOS)-$(GOARCH) $(MAIN_PATH)
	@echo "$(GREEN)$(OK) Maximum optimization applied$(RESET)"

build-all: ## 📦 Cross-compile for all platforms (parallel)
	@echo "$(CYAN)$(PACKAGE) Cross-compiling for all platforms...$(RESET)"
	@mkdir -p $(BUILD_DIR)
	@$(foreach platform,$(PLATFORMS),\
		$(eval _os=$(word 1,$(subst /, ,$(platform))))\
		$(eval _arch=$(word 2,$(subst /, ,$(platform))))\
		$(call build-platform,$(_os),$(_arch)))
	@echo "$(GREEN)$(OK) All platforms built$(RESET)"

define build-platform
	@echo "  $(PACKAGE) $(1)/$(2)...$(RESET)"
	@GOOS=$(1) GOARCH=$(2) $(GOBUILD) $(BUILD_OPTS) -ldflags="$(LDFLAGS)" \
		-o $(BUILD_DIR)/$(BINARY_NAME)-$(1)-$(2) $(MAIN_PATH)

endef

build-clean: clean build ## 🧹 Clean rebuild

build-docker: ## 🐳 Build Docker image
	@echo "$(CYAN)$(PACKAGE) Building Docker image...$(RESET)"
	@if [ ! -f scripts/docker-build.sh ]; then \
		echo "$(RED)$(ERR) scripts/docker-build.sh not found$(RESET)"; \
		exit 1; \
	fi
	@./scripts/docker-build.sh
	@echo "$(GREEN)$(OK) Docker image built$(RESET)"

# ════════════════════════════════════════════════════════════════════════════
# 🧪 Testing & Quality Assurance
# ════════════════════════════════════════════════════════════════════════════

test: ## 🧪 Run all tests with race detection
	@echo "$(CYAN)$(TEST) Running test suite...$(RESET)"
	@$(GOTEST) -race -timeout 15m ./...
	@echo "$(GREEN)$(OK) All tests passed$(RESET)"

test-short: ## ⚡ Run short tests (skip integration)
	@echo "$(CYAN)$(TEST) Running short tests...$(RESET)"
	@$(GOTEST) -short -race -timeout 5m ./...
	@echo "$(GREEN)$(OK) Short tests passed$(RESET)"

test-integration: ## 🔗 Run integration tests
	@echo "$(CYAN)$(TEST) Running integration tests...$(RESET)"
	@$(GOTEST) -run Integration -timeout 20m ./...
	@echo "$(GREEN)$(OK) Integration tests passed$(RESET)"

test-e2e: ## 🧪 Run end-to-end tests (client SDK → gateway → worker)
	@echo "$(CYAN)$(TEST) Running E2E tests...$(RESET)"
	@$(GOTEST) -race -v -timeout 2m ./e2e/...
	@echo "$(GREEN)$(OK) E2E tests passed$(RESET)"

test-verbose: ## 🔍 Run tests with verbose output
	@echo "$(CYAN)$(TEST) Running tests (verbose)...$(RESET)"
	@$(GOTEST) -race -v -timeout 15m ./...

test-race: test ## 🏃 Run tests with race detector (alias for test)

benchmark: ## 📊 Run benchmarks
	@echo "$(CYAN)$(CHART) Running benchmarks...$(RESET)"
	@$(GOTEST) -bench=. -benchmem -timeout 10m ./...

coverage: ## 📈 Generate coverage report
	@echo "$(CYAN)$(CHART) Generating coverage report...$(RESET)"
	@$(GOTEST) -coverprofile=$(COVERAGE_FILE) -timeout 15m ./...
	@$(GO) tool cover -func=$(COVERAGE_FILE) | tail -n 1 | awk '{print "$(GREEN)$(OK) Total coverage: " $$NF "$(RESET)"}'

coverage-html: ## 📄 Generate HTML coverage report
	@echo "$(CYAN)$(CHART) Generating HTML coverage...$(RESET)"
	@$(GOTEST) -coverprofile=$(COVERAGE_FILE) -timeout 15m ./...
	@$(GO) tool cover -html=$(COVERAGE_FILE) -o $(COVERAGE_HTML)
	@echo "$(GREEN)$(OK) Report: $(COVERAGE_HTML)$(RESET)"
	@open $(COVERAGE_HTML) 2>/dev/null || echo "$(DIM)Open $(COVERAGE_HTML) in your browser$(RESET)"

coverage-func: ## 📊 Show coverage by function
	@echo "$(CYAN)$(CHART) Coverage by function:$(RESET)"
	@$(GO) tool cover -func=$(COVERAGE_FILE)

lint: ## 🔧 Run linter
	@echo "$(CYAN)$(TOOL) Running golangci-lint...$(RESET)"
	@golangci-lint run ./...
	@echo "$(GREEN)$(OK) Linting passed$(RESET)"

lint-fix: ## 🔧 Run linter with auto-fix
	@echo "$(CYAN)$(TOOL) Running linter with auto-fix...$(RESET)"
	@golangci-lint run --fix ./...
	@echo "$(GREEN)$(OK) Auto-fix applied$(RESET)"

lint-verbose: ## 🔧 Run linter with verbose output
	@echo "$(CYAN)$(TOOL) Running linter (verbose)...$(RESET)"
	@golangci-lint run -v ./...

fmt: ## ✨ Format code
	@echo "$(CYAN)$(ART) Formatting code...$(RESET)"
	@$(GOFMT) ./...
	@if command -v goimports > /dev/null 2>&1; then \
		goimports -w .; \
	fi
	@echo "$(GREEN)$(OK) Code formatted$(RESET)"

fmt-check: ## ✨ Check code formatting (CI)
	@echo "$(CYAN)$(ART) Checking code format...$(RESET)"
	@test -z "$$(gofmt -l .)" || (echo "$(RED)$(ERR) Unformatted files:$(RESET"; gofmt -l .; exit 1)
	@echo "$(GREEN)$(OK) Formatting correct$(RESET)"

vet: ## 🔍 Run go vet
	@echo "$(CYAN)$(MAG) Running go vet...$(RESET)"
	@$(GOVET) ./...
	@echo "$(GREEN)$(OK) Vet passed$(RESET)"

tidy: ## 📦 Tidy go.mod
	@echo "$(CYAN)$(PACKAGE) Tidying modules...$(RESET)"
	@$(GOTIDY)
	@echo "$(GREEN)$(OK) Modules tidied$(RESET)"

quality: fmt vet lint test ## 🎯 Run all quality checks (fmt + vet + lint + test)
	@echo "$(GREEN)$(BOLD)$(SPARKLE) All quality checks passed!$(RESET)"

check: quality build ## 🎯 Full quality check + build (CI workflow)

# ════════════════════════════════════════════════════════════════════════════
# 🚀 Run Modes
# ════════════════════════════════════════════════════════════════════════════

run: build ## ▶️  Build and run gateway
	@echo "$(CYAN)$(ROCKET) Starting $(BINARY_NAME)...$(RESET)"
	@./$(BUILD_DIR)/$(BINARY_NAME)-$(GOOS)-$(GOARCH)

run-dev: build ## 🛠️  Run in development mode
	@echo "$(CYAN)$(ROCKET) Starting in dev mode...$(RESET)"
	@./$(BUILD_DIR)/$(BINARY_NAME)-$(GOOS)-$(GOARCH) -dev

run-prod: build ## 🏭 Run with production config
	@echo "$(CYAN)$(ROCKET) Starting production mode...$(RESET)"
	@./$(BUILD_DIR)/$(BINARY_NAME)-$(GOOS)-$(GOARCH) -config $(CONFIG_DIR)/config-prod.yaml

run-verbose: build ## 🔊 Run with verbose logging
	@echo "$(CYAN)$(ROCKET) Starting with verbose logging...$(RESET)"
	@./$(BUILD_DIR)/$(BINARY_NAME)-$(GOOS)-$(GOARCH) -v

run-config: build ## ⚙️  Run with custom config (CONFIG=/path/to/config.yaml)
	@echo "$(CYAN)$(ROCKET) Starting with custom config...$(RESET)"
	@./$(BUILD_DIR)/$(BINARY_NAME)-$(GOOS)-$(GOARCH) -config $(or $(CONFIG),$(CONFIG_DIR)/config.yaml)

run-debug: build ## 🐛 Run with debug flags
	@echo "$(CYAN)$(ROCKET) Starting in debug mode...$(RESET)"
	@./$(BUILD_DIR)/$(BINARY_NAME)-$(GOOS)-$(GOARCH) -v -dev

watch: ## 👀 Watch for file changes and auto-rebuild (requires reflex)
	@echo "$(CYAN)$(EAR) Watching for changes...$(RESET)"
	@if ! command -v reflex > /dev/null 2>&1; then \
		echo "$(YELLOW)$(WARN) reflex not installed$(RESET)"; \
		echo "$(DIM)Install: go install github.com/cespare/reflex@latest$(RESET)"; \
		exit 1; \
	fi
	@reflex -r '\.go$$' -s -- sh -c "$(MAKE) build && echo '$(GREEN)$(OK) Rebuilt$(RESET)'"

# ════════════════════════════════════════════════════════════════════════════
# 🔄 Gateway Lifecycle (Background)
# ════════════════════════════════════════════════════════════════════════════

GRACE_PERIOD := 7

worker-start: build ## 🚀 Start gateway in background
	@mkdir -p $(CURDIR)/$(LOG_DIR)
	@if [ -f $(WORKER_PID) ] && kill -0 $$(cat $(WORKER_PID)) 2>/dev/null; then \
		echo "$(YELLOW)$(WARN) Gateway already running (PID: $$(cat $(WORKER_PID)))$(RESET)"; \
	else \
		echo "$(CYAN)$(ROCKET) Starting gateway...$(RESET)"; \
		if [ -f $(CURDIR)/.env ]; then \
			set -a; . $(CURDIR)/.env; set +a; \
		fi; \
		./$(BUILD_DIR)/$(BINARY_NAME)-$(GOOS)-$(GOARCH) \
			-config $(or $(CONFIG),$(CONFIG_DIR)/config.yaml) \
			> $(CURDIR)/$(WORKER_LOG) 2>&1 & \
		echo $$! > $(WORKER_PID); \
		sleep 1; \
		if kill -0 $$(cat $(WORKER_PID)) 2>/dev/null; then \
			echo "$(GREEN)$(OK) Started (PID: $$(cat $(WORKER_PID)))$(RESET)"; \
			echo "$(DIM)Logs: $(WORKER_LOG)$(RESET)"; \
		else \
			echo "$(RED)$(ERR) Failed to start$(RESET)"; \
			cat $(CURDIR)/$(WORKER_LOG); \
			exit 1; \
		fi; \
	fi

worker-stop: ## 🛑 Stop gateway gracefully
	@if [ -f $(WORKER_PID) ]; then \
		PID=$$(cat $(WORKER_PID)); \
		if kill -0 $$PID 2>/dev/null; then \
			echo "$(CYAN)$(STOP) Stopping gateway (PID: $$PID)...$(RESET)"; \
			kill -TERM $$PID; \
			for i in $$(seq 1 $(GRACE_PERIOD)); do \
				sleep 1; \
				if ! kill -0 $$PID 2>/dev/null; then \
					echo "$(GREEN)$(OK) Stopped after $$i seconds$(RESET)"; \
					rm -f $(WORKER_PID); \
					exit 0; \
				fi; \
			done; \
			if kill -0 $$PID 2>/dev/null; then \
				echo "$(YELLOW)$(WARN) Force killing...$(RESET)"; \
				kill -9 $$PID; \
				rm -f $(WORKER_PID); \
			fi; \
		else \
			echo "$(DIM)$(INFO) Already stopped (stale PID)$(RESET)"; \
			rm -f $(WORKER_PID); \
		fi; \
	else \
		echo "$(DIM)$(INFO) Not running$(RESET)"; \
	fi

worker-status: ## 📊 Check gateway status
	@if [ -f $(WORKER_PID) ]; then \
		PID=$$(cat $(WORKER_PID)); \
		if kill -0 $$PID 2>/dev/null; then \
			echo "$(GREEN)🟢 Gateway running$(RESET) (PID: $$PID)"; \
			echo "  $(BULLET) Memory: $$(ps -o rss= -p $$PID 2>/dev/null | awk '{printf "%.1f MB", $$1/1024}')"; \
			echo "  $(BULLET) CPU:    $$(ps -o %cpu= -p $$PID 2>/dev/null | awk '{print $$1}')%"; \
			echo "  $(BULLET) Uptime: $$(ps -o etime= -p $$PID 2>/dev/null)"; \
			echo "  $(BULLET) Logs:   $(WORKER_LOG)"; \
		else \
			echo "$(RED)🔴 Gateway not running$(RESET) (stale PID)"; \
			rm -f $(WORKER_PID); \
		fi; \
	else \
		echo "$(DIM)⚪ Gateway not running$(RESET)"; \
	fi

worker-logs: ## 📋 Show gateway logs
	@if [ -f $(WORKER_LOG) ]; then \
		cat $(WORKER_LOG); \
	else \
		echo "$(DIM)$(INFO) No log file: $(WORKER_LOG)$(RESET)"; \
	fi

worker-tail: ## 👁️  Tail gateway logs
	@if [ -f $(WORKER_LOG) ]; then \
		echo "$(CYAN)$(LOGS) Tailing gateway logs...$(RESET)"; \
		tail -f $(WORKER_LOG); \
	else \
		echo "$(DIM)$(INFO) No log file: $(WORKER_LOG)$(RESET)"; \
	fi

worker-restart: worker-stop worker-start ## 🔄 Restart gateway

# Gateway aliases
start: worker-start ## 🚀 Start gateway (alias)
stop: worker-stop   ## 🛑 Stop gateway (alias)
status: worker-status ## 📊 Check status (alias)
logs: worker-logs   ## 📋 Show logs (alias)
tail: worker-tail   ## 👁️  Tail logs (alias)
restart: worker-restart ## 🔄 Restart gateway (alias)

force-kill: ## 💥 Force kill gateway
	@if [ -f $(WORKER_PID) ]; then \
		PID=$$(cat $(WORKER_PID)); \
		echo "$(YELLOW)$(WARN) Force killing (PID: $$PID)...$(RESET)"; \
		kill -9 $$PID 2>/dev/null || true; \
		rm -f $(WORKER_PID); \
		echo "$(GREEN)$(OK) Force killed$(RESET)"; \
	else \
		echo "$(DIM)$(INFO) Not running$(RESET)"; \
	fi

reload: ## 🔃 Reload config (SIGHUP)
	@if [ -f $(WORKER_PID) ] && kill -0 $$(cat $(WORKER_PID)) 2>/dev/null; then \
		PID=$$(cat $(WORKER_PID)); \
		echo "$(CYAN)$(ROCKET) Reloading config...$(RESET)"; \
		kill -HUP $$PID; \
		echo "$(GREEN)$(OK) Reload signal sent$(RESET)"; \
	else \
		echo "$(RED)$(ERR) Not running$(RESET)"; \
	fi

# ════════════════════════════════════════════════════════════════════════════
# 💬 Web Chat
# ════════════════════════════════════════════════════════════════════════════

webchat-install: ## 📦 Install webchat dependencies
	@echo "$(CYAN)$(PACKAGE) Installing webchat dependencies...$(RESET)"
	@cd $(WEB_CHAT_DIR) && pnpm install
	@echo "$(GREEN)$(OK) Dependencies installed$(RESET)"

webchat-build: webchat-install ## 🔨 Build webchat
	@mkdir -p $(CURDIR)/$(LOG_DIR)
	@echo "$(CYAN)$(BUILD) Building webchat...$(RESET)"
	@cd $(WEB_CHAT_DIR) && pnpm build
	@echo "$(GREEN)$(OK) Built$(RESET)"

webchat-dev: webchat-install ## 🛠️  Start webchat dev server (background)
	@mkdir -p $(CURDIR)/$(LOG_DIR)
	@if [ -f $(WEB_CHAT_PID) ] && kill -0 $$(cat $(WEB_CHAT_PID)) 2>/dev/null; then \
		echo "$(YELLOW)$(WARN) Web-chat already running (PID: $$(cat $(WEB_CHAT_PID)))$(RESET)"; \
	else \
		echo "$(CYAN)$(ROCKET) Starting webchat dev...$(RESET)"; \
		cd $(WEB_CHAT_DIR) && pnpm dev > $(CURDIR)/$(WEB_CHAT_LOG) 2>&1 & \
		echo $$! > $(WEB_CHAT_PID); \
		sleep 2; \
		if kill -0 $$(cat $(WEB_CHAT_PID)) 2>/dev/null; then \
			echo "$(GREEN)$(OK) Started (PID: $$(cat $(WEB_CHAT_PID)))$(RESET)"; \
		else \
			echo "$(RED)$(ERR) Failed to start$(RESET)"; \
			cat $(CURDIR)/$(WEB_CHAT_LOG); \
			exit 1; \
		fi; \
	fi

webchat-start: webchat-build ## 🚀 Start webchat production (background)
	@mkdir -p $(CURDIR)/$(LOG_DIR)
	@if [ -f $(WEB_CHAT_PID) ] && kill -0 $$(cat $(WEB_CHAT_PID)) 2>/dev/null; then \
		echo "$(YELLOW)$(WARN) Web-chat already running$(RESET)"; \
	else \
		echo "$(CYAN)$(ROCKET) Starting webchat production...$(RESET)"; \
		cd $(WEB_CHAT_DIR) && pnpm start > $(CURDIR)/$(WEB_CHAT_LOG) 2>&1 & \
		echo $$! > $(WEB_CHAT_PID); \
		sleep 2; \
		if kill -0 $$(cat $(WEB_CHAT_PID)) 2>/dev/null; then \
			echo "$(GREEN)$(OK) Started (PID: $$(cat $(WEB_CHAT_PID)))$(RESET)"; \
		else \
			echo "$(RED)$(ERR) Failed to start$(RESET)"; \
			cat $(CURDIR)/$(WEB_CHAT_LOG); \
			exit 1; \
		fi; \
	fi

webchat-stop: ## Stop webchat
	@STOPPED=true; \
	if [ -f $(WEB_CHAT_PID) ]; then \
		PID=$$(cat $(WEB_CHAT_PID)); \
		if kill -0 $$PID 2>/dev/null; then \
			echo "$(CYAN)$(STOP) Stopping webchat (PID: $$PID)...$(RESET)"; \
			kill -TERM $$PID 2>/dev/null; \
			sleep 1; \
			kill -0 $$PID 2>/dev/null && kill -9 $$PID 2>/dev/null || true; \
			echo "$(GREEN)$(OK) Stopped$(RESET)"; \
			STOPPED=false; \
		fi; \
		rm -f $(WEB_CHAT_PID); \
	fi; \
	if $$STOPPED; then \
		PORT_PIDS=$$(lsof -ti:$(WEB_CHAT_PORT) 2>/dev/null || true); \
		if [ -n "$$PORT_PIDS" ]; then \
			echo "$(YELLOW)$(WARN) PID file stale, found processes on port $(WEB_CHAT_PORT)$(RESET)"; \
			for PID in $$PORT_PIDS; do \
				echo "$(CYAN)$(STOP) Stopping webchat (PID: $$PID)...$(RESET)"; \
				kill -TERM $$PID 2>/dev/null || true; \
			done; \
			sleep 1; \
			for PID in $$PORT_PIDS; do \
				kill -0 $$PID 2>/dev/null && kill -9 $$PID 2>/dev/null || true; \
			done; \
			echo "$(GREEN)$(OK) Stopped$(RESET)"; \
			STOPPED=false; \
		fi; \
	fi; \
	if $$STOPPED; then \
		echo "$(DIM)$(INFO) Not running$(RESET)"; \
	fi

webchat-status: ## 📊 Check webchat status
	@if [ -f $(WEB_CHAT_PID) ]; then \
		PID=$$(cat $(WEB_CHAT_PID)); \
		if kill -0 $$PID 2>/dev/null; then \
			echo "$(GREEN)🟢 Web-chat running$(RESET) (PID: $$PID)"; \
		else \
			echo "$(RED)🔴 Web-chat not running$(RESET) (stale PID)"; \
			rm -f $(WEB_CHAT_PID); \
		fi; \
	else \
		echo "$(DIM)⚪ Web-chat not running$(RESET)"; \
	fi

webchat-logs: ## 📋 Show webchat logs
	@if [ -f $(WEB_CHAT_LOG) ]; then \
		cat $(WEB_CHAT_LOG); \
	else \
		echo "$(DIM)$(INFO) No log file: $(WEB_CHAT_LOG)$(RESET)"; \
	fi

webchat-tail: ## 👁️  Tail webchat logs
	@if [ -f $(WEB_CHAT_LOG) ]; then \
		echo "$(CYAN)$(LOGS) Tailing webchat logs...$(RESET)"; \
		tail -f $(WEB_CHAT_LOG); \
	else \
		echo "$(DIM)$(INFO) No log file: $(WEB_CHAT_LOG)$(RESET)"; \
	fi

webchat-clean: ## 🧹 Clean webchat build artifacts
	@echo "$(CYAN)$(CLEAN) Cleaning webchat...$(RESET)"
	@cd $(WEB_CHAT_DIR) && rm -rf .next node_modules
	@echo "$(GREEN)$(OK) Cleaned$(RESET)"

# ════════════════════════════════════════════════════════════════════════════
# 🔧 Development Workflow
# ════════════════════════════════════════════════════════════════════════════

dev: dev-start ## 🚀 Start development environment (alias)

dev-start: worker-start webchat-dev ## 🚀 Start full dev environment
	@echo ""
	@echo "$(GREEN)$(BOLD)$(SPARKLE) Development Environment Ready!$(RESET)"
	@echo ""
	@echo "$(BOLD)Services:$(RESET)"
	@echo "  $(CYAN)$(BULLET) Gateway:   http://localhost:8888$(RESET)"
	@echo "  $(CYAN)$(BULLET) Web-chat:  http://localhost:3000$(RESET)"
	@echo "  $(CYAN)$(BULLET) Admin:     http://localhost:9999$(RESET)"
	@echo ""
	@echo "$(BOLD)Commands:$(RESET)"
	@echo "  $(DIM)make dev-logs$(RESET)    View all logs"
	@echo "  $(DIM)make dev-status$(RESET)  Check status"
	@echo "  $(DIM)make dev-stop$(RESET)    Stop all services"
	@echo ""

dev-stop: worker-stop webchat-stop ## 🛑 Stop development environment
	@echo "$(GREEN)$(OK) Development environment stopped$(RESET)"

dev-status: worker-status webchat-status ## 📊 Check development status

dev-logs: worker-logs webchat-logs ## 📋 Show all development logs

dev-reset: dev-stop clean dev-start ## 🔄 Reset development environment

# ════════════════════════════════════════════════════════════════════════════
# 🧹 Cleanup & Maintenance
# ════════════════════════════════════════════════════════════════════════════

clean: ## 🧹 Clean build artifacts
	@echo "$(CYAN)$(CLEAN) Cleaning build artifacts...$(RESET)"
	@$(GO) clean
	@rm -rf $(BUILD_DIR)
	@rm -f $(COVERAGE_FILE) $(COVERAGE_HTML)
	@rm -f $(TESTDB_PATTERN)
	@echo "$(GREEN)$(OK) Cleaned$(RESET)"

clean-logs: ## 🧹 Clean log files
	@echo "$(CYAN)$(CLEAN) Cleaning logs...$(RESET)"
	@rm -rf $(LOG_DIR)
	@echo "$(GREEN)$(OK) Logs cleaned$(RESET)"

clean-cache: ## 🧹 Clean Go build cache
	@echo "$(CYAN)$(CLEAN) Cleaning Go cache...$(RESET)"
	@$(GO) clean -cache -testcache -modcache
	@echo "$(GREEN)$(OK) Cache cleaned$(RESET)"

clean-all: clean clean-logs clean-cache ## 🧹 Clean everything
	@echo "$(GREEN)$(OK) All cleaned$(RESET)"

reset: dev-stop clean-all ## 🔄 Factory reset (stop + clean all)
	@echo "$(GREEN)$(OK) Factory reset complete$(RESET)"

# ════════════════════════════════════════════════════════════════════════════
# 📦 Installation & Deployment
# ════════════════════════════════════════════════════════════════════════════

install: build ## 📦 Install system-wide (requires sudo)
	@echo "$(CYAN)$(PACKAGE) Installing system-wide...$(RESET)"
	@if [ $$USER != root ]; then \
		echo "$(RED)$(ERR) Requires sudo privileges$(RESET)"; \
		echo "$(DIM)Run: sudo make install$(RESET)"; \
		exit 1; \
	fi
	@if [ ! -f scripts/install.sh ]; then \
		echo "$(RED)$(ERR) scripts/install.sh not found$(RESET)"; \
		exit 1; \
	fi
	@./scripts/install.sh --non-interactive
	@echo "$(GREEN)$(OK) Installed to /usr/local/bin/$(BINARY_NAME)$(RESET)"

uninstall: ## 🗑️  Uninstall system-wide (requires sudo)
	@echo "$(CYAN)$(CLEAN) Uninstalling...$(RESET)"
	@if [ $$USER != root ]; then \
		echo "$(RED)$(ERR) Requires sudo privileges$(RESET)"; \
		echo "$(DIM)Run: sudo make uninstall$(RESET)"; \
		exit 1; \
	fi
	@if [ ! -f scripts/uninstall.sh ]; then \
		echo "$(RED)$(ERR) scripts/uninstall.sh not found$(RESET)"; \
		exit 1; \
	fi
	@./scripts/uninstall.sh --non-interactive
	@echo "$(GREEN)$(OK) Uninstalled$(RESET)"

update: ## 📥 Update dependencies
	@echo "$(CYAN)$(PACKAGE) Updating dependencies...$(RESET)"
	@$(GOTIDY)
	@$(GO) list -u -m -json all | grep -E '"Path"|"Version"|"Update"' || true
	@echo "$(GREEN)$(OK) Dependencies updated$(RESET)"

upgrade: ## ⬆️  Upgrade all dependencies to latest
	@echo "$(CYAN)$(PACKAGE) Upgrading dependencies...$(RESET)"
	@$(GO) get -u ./...
	@$(GOTIDY)
	@echo "$(GREEN)$(OK) Dependencies upgraded$(RESET)"

# ════════════════════════════════════════════════════════════════════════════
# 💾 Snapshot & Restore
# ════════════════════════════════════════════════════════════════════════════

SNAPSHOT_DIR := .snapshots

snapshot: ## 💾 Create development snapshot
	@mkdir -p $(SNAPSHOT_DIR)
	@echo "$(CYAN)$(PACKAGE) Creating snapshot...$(RESET)"
	@SNAPSHOT_NAME="$(BINARY_NAME)-$(GIT_SHA)-$$(date +%Y%m%d-%H%M%S).tar.gz"; \
	tar -czf $(SNAPSHOT_DIR)/$$SNAPSHOT_NAME \
		--exclude='$(SNAPSHOT_DIR)' \
		--exclude='$(BUILD_DIR)' \
		--exclude='$(LOG_DIR)' \
		--exclude='node_modules' \
		--exclude='.git' \
		--exclude='*.db*' \
		. 2>/dev/null; \
	echo "$(GREEN)$(OK) Snapshot: $(SNAPSHOT_DIR)/$$SNAPSHOT_NAME$(RESET)"

restore: ## 💾 Restore from snapshot (SNAPSHOT=filename)
	@if [ -z "$(SNAPSHOT)" ]; then \
		echo "$(RED)$(ERR) No snapshot specified$(RESET)"; \
		echo "$(DIM)Usage: make restore SNAPSHOT=snapshot.tar.gz$(RESET)"; \
		exit 1; \
	fi
	@if [ ! -f "$(SNAPSHOT_DIR)/$(SNAPSHOT)" ]; then \
		echo "$(RED)$(ERR) Snapshot not found: $(SNAPSHOT)$(RESET)"; \
		exit 1; \
	fi
	@echo "$(YELLOW)$(WARN) This will overwrite current files!$(RESET)"
	@read -p "Continue? [y/N] " -n 1 -r; \
	echo; \
	if [[ $$REPLY =~ ^[Yy]$$ ]]; then \
		echo "$(CYAN)$(PACKAGE) Restoring snapshot...$(RESET)"; \
		tar -xzf $(SNAPSHOT_DIR)/$(SNAPSHOT); \
		echo "$(GREEN)$(OK) Restored from $(SNAPSHOT)$(RESET)"; \
	fi

# ════════════════════════════════════════════════════════════════════════════
# ⚙️  Configuration
# ════════════════════════════════════════════════════════════════════════════

config-edit: ## ✏️  Edit configuration file
	@if [ -f "$(CONFIG_DIR)/config.yaml" ]; then \
		$${EDITOR:-vim} $(CONFIG_DIR)/config.yaml; \
	else \
		echo "$(RED)$(ERR) Config file not found: $(CONFIG_DIR)/config.yaml$(RESET)"; \
	fi

config-validate: ## ✅ Validate configuration
	@echo "$(CYAN)$(TOOL) Validating configuration...$(RESET)"
	@if [ -f "$(CONFIG_DIR)/config.yaml" ]; then \
		echo "$(GREEN)$(OK) Config file exists$(RESET)"; \
		echo "$(DIM)Path: $(CONFIG_DIR)/config.yaml$(RESET)"; \
	else \
		echo "$(YELLOW)$(WARN) Config file not found$(RESET)"; \
	fi

# ════════════════════════════════════════════════════════════════════════════
# ℹ️  Version & Info
# ════════════════════════════════════════════════════════════════════════════

version: ## 🔢 Show version info
	@echo "$(CYAN)$(BOLD)$(BINARY_NAME)$(RESET) $(GIT_SHA)"
	@echo "$(DIM)Go: $(GO_VERSION) | Platform: $(GOOS)/$(GOARCH) | Built: $(BUILD_TIME)$(RESET)"

# ════════════════════════════════════════════════════════════════════════════
# 📚 Premium Help System
# ════════════════════════════════════════════════════════════════════════════

help: ## 📖 Show interactive help (this screen)
	@echo ""
	@echo "$(CYAN)$(BOLD)╔════════════════════════════════════════════════════════════════╗$(RESET)"
	@echo "$(CYAN)$(BOLD)║$(RESET)        $(BOLD)$(WHITE)🚀 HotPlex Worker Gateway - Command Center$(RESET)        $(CYAN)$(BOLD)║$(RESET)"
	@echo "$(CYAN)$(BOLD)╚════════════════════════════════════════════════════════════════╝$(RESET)"
	@echo ""
	@echo "$(DIM)Usage:$(RESET) $(BOLD)make [target]$(RESET)"
	@echo ""
	@echo "$(BOLD)$(CYAN)🚀 Quick Start$(RESET)"
	@echo "  $(GREEN)quickstart$(RESET)      First-time setup (install tools + build + test)"
	@echo "  $(GREEN)dev$(RESET)             Start development environment"
	@echo "  $(GREEN)check$(RESET)           Full quality check (lint + test + build)"
	@echo ""
	@echo "$(BOLD)$(CYAN)🔧 Development$(RESET)"
	@echo "  $(CYAN)dev-start$(RESET)       Start gateway + webchat"
	@echo "  $(CYAN)dev-stop$(RESET)        Stop all services"
	@echo "  $(CYAN)dev-status$(RESET)      Check service status"
	@echo "  $(CYAN)dev-logs$(RESET)        View all logs"
	@echo "  $(CYAN)watch$(RESET)           Auto-rebuild on changes"
	@echo ""
	@echo "$(BOLD)$(CYAN)🔨 Build & Run$(RESET)"
	@echo "  $(CYAN)build$(RESET)           Build binary ($(GOOS)/$(GOARCH))"
	@echo "  $(CYAN)build-pgo$(RESET)       Build with PGO optimization"
	@echo "  $(CYAN)build-all$(RESET)       Cross-compile for all platforms"
	@echo "  $(CYAN)run$(RESET)             Build and run (foreground)"
	@echo "  $(CYAN)run-dev$(RESET)         Run in dev mode"
	@echo ""
	@echo "$(BOLD)$(CYAN)🧪 Quality Assurance$(RESET)"
	@echo "  $(CYAN)test$(RESET)            Run all tests (race detection)"
	@echo "  $(CYAN)test-short$(RESET)      Run short tests"
	@echo "  $(CYAN)coverage$(RESET)        Generate coverage report"
	@echo "  $(CYAN)coverage-html$(RESET)   Generate HTML coverage"
	@echo "  $(CYAN)lint$(RESET)            Run linter"
	@echo "  $(CYAN)quality$(RESET)         All quality checks"
	@echo ""
	@echo "$(BOLD)$(CYAN)🔄 Lifecycle Management$(RESET)"
	@echo "  $(CYAN)start$(RESET)           Start gateway (background)"
	@echo "  $(CYAN)stop$(RESET)            Stop gateway gracefully"
	@echo "  $(CYAN)restart$(RESET)         Restart gateway"
	@echo "  $(CYAN)status$(RESET)          Check status"
	@echo "  $(CYAN)logs$(RESET)            Show logs"
	@echo "  $(CYAN)tail$(RESET)            Tail logs (live)"
	@echo ""
	@echo "$(BOLD)$(CYAN)💬 Web Chat$(RESET)"
	@echo "  $(CYAN)webchat-dev$(RESET)    Start webchat dev server"
	@echo "  $(CYAN)webchat-build$(RESET)   Build webchat for production"
	@echo "  $(CYAN)webchat-start$(RESET)   Start webchat (production)"
	@echo "  $(CYAN)webchat-stop$(RESET)    Stop webchat"
	@echo "  $(CYAN)webchat-status$(RESET)   Check webchat status"
	@echo ""
	@echo "$(BOLD)$(CYAN)🧹 Maintenance$(RESET)"
	@echo "  $(CYAN)clean$(RESET)           Clean build artifacts"
	@echo "  $(CYAN)clean-all$(RESET)       Clean everything"
	@echo "  $(CYAN)health$(RESET)          System health check"
	@echo "  $(CYAN)setup$(RESET)           Install dev tools"
	@echo ""
	@echo "$(BOLD)$(CYAN)📚 Help & Info$(RESET)"
	@echo "  $(CYAN)help-full$(RESET)       Show all $(shell grep -c '^[a-z].*:.*## ' $(MAKEFILE_LIST)) commands"
	@echo "  $(CYAN)help-test$(RESET)       Test & coverage commands"
	@echo "  $(CYAN)help-build$(RESET)      Build commands"
	@echo "  $(CYAN)help-dev$(RESET)        Development workflow"
	@echo "  $(CYAN)version$(RESET)         Show version info"
	@echo "  $(CYAN)info$(RESET)            Detailed system info"
	@echo ""
	@echo "$(DIM)$(IDEA) Pro Tips:$(RESET)"
	@echo "  $(DIM)• Press <tab> twice for command completion$(RESET)"
	@echo "  $(DIM)• Run 'make help-<query>' to search commands$(RESET)"
	@echo "  $(DIM)• Most targets have verbose variants (-verbose suffix)$(RESET)"
	@echo ""

help-full: ## 📖 Show all available commands
	@echo ""
	@echo "$(CYAN)$(BOLD)═══════════════════════════════════════════════════════════════$(RESET)"
	@echo "$(CYAN)$(BOLD)               📚 Complete Command Reference$(RESET)              "
	@echo "$(CYAN)$(BOLD)═══════════════════════════════════════════════════════════════$(RESET)"
	@echo ""
	@grep -E '^[a-z].*:.*## ' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*## "}; {printf "  $(CYAN)%-20s$(RESET) %s\n", $$1, $$2}'
	@echo ""

help-dev: ## 📖 Development workflow commands
	@echo "$(CYAN)$(BOLD)🔧 Development Workflow Commands$(RESET)"
	@echo ""
	@echo "$(BOLD)Quick Start:$(RESET)"
	@echo "  $(CYAN)quickstart$(RESET)        First-time setup"
	@echo "  $(CYAN)dev$(RESET)               Start dev environment"
	@echo "  $(CYAN)dev-start$(RESET)         Start all services"
	@echo "  $(CYAN)dev-stop$(RESET)          Stop all services"
	@echo "  $(CYAN)dev-status$(RESET)        Check status"
	@echo "  $(CYAN)dev-logs$(RESET)          View logs"
	@echo "  $(CYAN)dev-reset$(RESET)         Reset environment"
	@echo ""
	@echo "$(BOLD)Hot Reload:$(RESET)"
	@echo "  $(CYAN)watch$(RESET)             Auto-rebuild on changes"
	@echo ""
	@echo "$(BOLD)Gateway:$(RESET)"
	@echo "  $(CYAN)worker-start$(RESET)      Start gateway"
	@echo "  $(CYAN)worker-stop$(RESET)       Stop gateway"
	@echo "  $(CYAN)worker-status$(RESET)     Check status"
	@echo "  $(CYAN)worker-logs$(RESET)       View logs"
	@echo ""
	@echo "$(BOLD)Web Chat:$(RESET)"
	@echo "  $(CYAN)webchat-dev$(RESET)      Start dev server"
	@echo "  $(CYAN)webchat-build$(RESET)    Production build"
	@echo ""

help-test: ## 📖 Testing & quality commands
	@echo "$(CYAN)$(BOLD)🧪 Testing & Quality Commands$(RESET)"
	@echo ""
	@echo "$(BOLD)Testing:$(RESET)"
	@echo "  $(CYAN)test$(RESET)              All tests (race detection)"
	@echo "  $(CYAN)test-short$(RESET)        Quick tests"
	@echo "  $(CYAN)test-integration$(RESET)  Integration tests"
	@echo "  $(CYAN)test-e2e$(RESET)          E2E tests (client→gateway→worker)"
	@echo "  $(CYAN)test-verbose$(RESET)      Verbose output"
	@echo "  $(CYAN)benchmark$(RESET)         Run benchmarks"
	@echo ""
	@echo "$(BOLD)Coverage:$(RESET)"
	@echo "  $(CYAN)coverage$(RESET)          Generate report"
	@echo "  $(CYAN)coverage-html$(RESET)     HTML report"
	@echo "  $(CYAN)coverage-func$(RESET)     Coverage by function"
	@echo ""
	@echo "$(BOLD)Quality:$(RESET)"
	@echo "  $(CYAN)lint$(RESET)              Run linter"
	@echo "  $(CYAN)lint-fix$(RESET)          Auto-fix issues"
	@echo "  $(CYAN)fmt$(RESET)               Format code"
	@echo "  $(CYAN)vet$(RESET)               Run go vet"
	@echo "  $(CYAN)quality$(RESET)           All checks"
	@echo "  $(CYAN)check$(RESET)             Full CI workflow"
	@echo ""

help-build: ## 📖 Build commands
	@echo "$(CYAN)$(BOLD)🔨 Build Commands$(RESET)"
	@echo ""
	@echo "$(BOLD)Standard:$(RESET)"
	@echo "  $(CYAN)build$(RESET)             Build binary"
	@echo "  $(CYAN)build-clean$(RESET)       Clean rebuild"
	@echo ""
	@echo "$(BOLD)Optimized:$(RESET)"
	@echo "  $(CYAN)build-pgo$(RESET)         PGO optimization"
	@echo "  $(CYAN)build-optimize$(RESET)    Maximum optimization"
	@echo ""
	@echo "$(BOLD)Cross-Platform:$(RESET)"
	@echo "  $(CYAN)build-all$(RESET)         All platforms"
	@echo "  $(CYAN)build-docker$(RESET)      Docker image"
	@echo ""
	@echo "$(BOLD)Run:$(RESET)"
	@echo "  $(CYAN)run$(RESET)               Build + run"
	@echo "  $(CYAN)run-dev$(RESET)           Development mode"
	@echo "  $(CYAN)run-prod$(RESET)          Production mode"
	@echo "  $(CYAN)run-verbose$(RESET)       Verbose logging"
	@echo ""

help-run: ## 📖 Runtime commands
	@echo "$(CYAN)$(BOLD)🚀 Runtime Commands$(RESET)"
	@echo ""
	@echo "$(BOLD)Foreground:$(RESET)"
	@echo "  $(CYAN)run$(RESET)               Standard run"
	@echo "  $(CYAN)run-dev$(RESET)           Dev mode"
	@echo "  $(CYAN)run-prod$(RESET)          Production"
	@echo "  $(CYAN)run-config$(RESET)        Custom config"
	@echo ""
	@echo "$(BOLD)Background:$(RESET)"
	@echo "  $(CYAN)start$(RESET)             Start daemon"
	@echo "  $(CYAN)stop$(RESET)              Stop daemon"
	@echo "  $(CYAN)restart$(RESET)           Restart daemon"
	@echo "  $(CYAN)status$(RESET)            Check status"
	@echo "  $(CYAN)reload$(RESET)            Reload config"
	@echo "  $(CYAN)force-kill$(RESET)        Force stop"
	@echo ""
	@echo "$(BOLD)Logs:$(RESET)"
	@echo "  $(CYAN)logs$(RESET)              Show logs"
	@echo "  $(CYAN)tail$(RESET)              Tail logs"
	@echo ""

help-web: ## 📖 Web chat commands
	@echo "$(CYAN)$(BOLD)💬 Web Chat Commands$(RESET)"
	@echo ""
	@echo "$(BOLD)Development:$(RESET)"
	@echo "  $(CYAN)webchat-dev$(RESET)      Start dev server"
	@echo "  $(CYAN)webchat-install$(RESET)  Install deps"
	@echo ""
	@echo "$(BOLD)Production:$(RESET)"
	@echo "  $(CYAN)webchat-build$(RESET)    Build"
	@echo "  $(CYAN)webchat-start$(RESET)    Start production"
	@echo ""
	@echo "$(BOLD)Lifecycle:$(RESET)"
	@echo "  $(CYAN)webchat-stop$(RESET)     Stop"
	@echo "  $(CYAN)webchat-status$(RESET)   Status"
	@echo "  $(CYAN)webchat-logs$(RESET)     Logs"
	@echo "  $(CYAN)webchat-tail$(RESET)     Tail logs"
	@echo "  $(CYAN)webchat-clean$(RESET)    Clean"
	@echo ""

help-advanced: ## 📖 Advanced commands
	@echo "$(CYAN)$(BOLD)⚙️  Advanced Commands$(RESET)"
	@echo ""
	@echo "$(BOLD)System:$(RESET)"
	@echo "  $(CYAN)health$(RESET)            Health check"
	@echo "  $(CYAN)info$(RESET)              System info"
	@echo "  $(CYAN)env$(RESET)               Environment vars"
	@echo ""
	@echo "$(BOLD)Maintenance:$(RESET)"
	@echo "  $(CYAN)clean-all$(RESET)         Full clean"
	@echo "  $(CYAN)clean-cache$(RESET)       Clean Go cache"
	@echo "  $(CYAN)reset$(RESET)             Factory reset"
	@echo ""
	@echo "$(BOLD)Snapshot:$(RESET)"
	@echo "  $(CYAN)snapshot$(RESET)          Create snapshot"
	@echo "  $(CYAN)restore$(RESET)           Restore snapshot"
	@echo ""
	@echo "$(BOLD)Installation:$(RESET)"
	@echo "  $(CYAN)install$(RESET)           System-wide install"
	@echo "  $(CYAN)uninstall$(RESET)         Uninstall"
	@echo "  $(CYAN)update$(RESET)            Update deps"
	@echo "  $(CYAN)upgrade$(RESET)           Upgrade deps"
	@echo ""
	@echo "$(BOLD)Configuration:$(RESET)"
	@echo "  $(CYAN)config-edit$(RESET)       Edit config"
	@echo "  $(CYAN)config-validate$(RESET)   Validate config"
	@echo ""

# ─────────────────────────────────────────────────────────────────────────────
# 🎯 Special Targets
# ─────────────────────────────────────────────────────────────────────────────

# Catch-all for unknown targets
%:
	@echo "$(RED)$(ERR) Unknown target: $@$(RESET)"
	@echo "$(DIM)Run 'make help' to see available commands$(RESET)"
	@exit 1

# ─────────────────────────────────────────────────────────────────────────────
# 🎨 End of Makefile
# ─────────────────────────────────────────────────────────────────────────────
