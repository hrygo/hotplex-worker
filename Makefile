# HotPlex Worker Gateway - Development Makefile
# Not for production service management.
#
#   make help        Show commands
#   make quickstart  First-time setup
#   make dev         Start dev environment
#   make check       Quality check (CI)

# ─────────────────────────────────────────────────────────────────────────────
# Configuration
# ─────────────────────────────────────────────────────────────────────────────

BINARY_NAME  := hotplex
BUILD_DIR    := bin
MAIN_PATH    := ./cmd/hotplex
CONFIG_DIR   := configs
LOG_DIR      := logs

GO_VERSION   := $(shell go version | cut -d' ' -f3)
GOOS         := $(shell go env GOOS)
GOARCH       := $(shell go env GOARCH)
GIT_SHA      := $(shell git rev-parse --short=8 HEAD 2>/dev/null || echo "unknown")
BUILD_TIME   := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS      := -s -w -X main.version=$(GIT_SHA) -X main.buildTime=$(BUILD_TIME)
BUILD_OPTS   := -trimpath

GATEWAY_PID   := $(HOME)/.hotplex/.pids/gateway.pid
GATEWAY_LOG   := $(LOG_DIR)/hotplex.log
WEB_CHAT_PID  := $(HOME)/.hotplex/.pids/hotplex-webchat.pid
WEB_CHAT_PORT := 3000
WEB_CHAT_LOG  := $(CURDIR)/$(LOG_DIR)/webchat.log
WEB_CHAT_DIR  := webchat
GRACE_PERIOD  := 7

# ─────────────────────────────────────────────────────────────────────────────
# Color
# ─────────────────────────────────────────────────────────────────────────────

RESET  := \033[0m
BOLD   := \033[1m
DIM    := \033[2m
RED    := \033[31m
GREEN  := \033[32m
YELLOW := \033[33m
CYAN   := \033[36m

# ─────────────────────────────────────────────────────────────────────────────
# PHONY
# ─────────────────────────────────────────────────────────────────────────────

.PHONY: all help quickstart check-tools build run
.PHONY: dev dev-start dev-stop dev-status dev-logs dev-reset
.PHONY: gateway-start gateway-stop gateway-status gateway-logs
.PHONY: webchat-dev webchat-stop
.PHONY: test test-short lint fmt quality check clean

# ─────────────────────────────────────────────────────────────────────────────
# Default
# ─────────────────────────────────────────────────────────────────────────────

all: help

# ─────────────────────────────────────────────────────────────────────────────
# Setup
# ─────────────────────────────────────────────────────────────────────────────

quickstart: check-tools build test-short
	@echo ""
	@echo "  $(GREEN)✓ Setup complete$(RESET)"
	@echo ""
	@echo "    make dev      Start dev environment"
	@echo "    make run      Run gateway"
	@echo "    make help     Show all commands"
	@echo ""

check-tools:
	@$(call check-tool, go, "Go")
	@$(call check-tool, golangci-lint, "golangci-lint")
	@$(call check-tool, goimports, "goimports")

define check-tool
	@if command -v $(1) > /dev/null 2>&1; then \
		echo "  $(GREEN)✓$(RESET) $(2)"; \
	else \
		echo "  $(YELLOW)⚠$(RESET) $(2) $(DIM)(missing)$(RESET)"; \
	fi
endef

# ─────────────────────────────────────────────────────────────────────────────
# Build
# ─────────────────────────────────────────────────────────────────────────────

build:
	@echo "$(CYAN)Building...$(RESET)"
	@mkdir -p $(BUILD_DIR) $(LOG_DIR)
	@go build $(BUILD_OPTS) -ldflags="$(LDFLAGS)" \
		-o $(BUILD_DIR)/$(BINARY_NAME)-$(GOOS)-$(GOARCH) $(MAIN_PATH)
	@echo "  $(GREEN)✓$(RESET) $(BUILD_DIR)/$(BINARY_NAME)-$(GOOS)-$(GOARCH)"

run: build
	@./$(BUILD_DIR)/$(BINARY_NAME)-$(GOOS)-$(GOARCH) \
		gateway start -c $(CONFIG_DIR)/config-dev.yaml

# ─────────────────────────────────────────────────────────────────────────────
# Test
# ─────────────────────────────────────────────────────────────────────────────

test:
	@echo "$(CYAN)Testing...$(RESET)"
	@GORACE="history_size=5" go test -race -timeout 15m ./...
	@echo "  $(GREEN)✓ Tests passed$(RESET)"

test-short:
	@echo "$(CYAN)Testing...$(RESET)"
	@GORACE="history_size=5" go test -short -race -timeout 5m ./...
	@echo "  $(GREEN)✓ Tests passed$(RESET)"

coverage:
	@echo "$(CYAN)Generating coverage report...$(RESET)"
	@go test -timeout=15m -coverprofile=coverage.out -covermode=atomic \
		$$(go list ./... | grep -v -e 'internal/worker/proc' -e 'internal/worker/pi' -e 'cmd/hotplex')
	@echo ""
	@echo "$(BOLD)Per-package coverage:$(RESET)"
	@go tool cover -func=coverage.out | grep -v "^total:" | sort -t: -k3 -n
	@echo ""
	@TOTAL=$$(go tool cover -func=coverage.out | tail -1 | grep -oP '\d+\.\d+') ; \
		echo "  $(BOLD)Total: $${TOTAL}%$(RESET)"

test-slack-e2e:
	@echo "$(CYAN)Running Slack semi-automated E2E tests...$(RESET)"
	@test -n "$$SLACK_BOT_TOKEN" || (echo "  $(RED)SLACK_BOT_TOKEN required$(RESET)"; exit 1)
	@test -n "$$SLACK_APP_TOKEN" || (echo "  $(RED)SLACK_APP_TOKEN required$(RESET)"; exit 1)
	@go test -v -tags=slack_e2e -timeout 30m ./internal/messaging/slack/...

# ─────────────────────────────────────────────────────────────────────────────
# Quality
# ─────────────────────────────────────────────────────────────────────────────

lint:
	@echo "$(CYAN)Linting...$(RESET)"
	@golangci-lint run ./...

fmt:
	@echo "$(CYAN)Formatting...$(RESET)"
	@go fmt ./...
	@if command -v goimports > /dev/null 2>&1; then goimports -w .; fi

quality: fmt lint test
	@echo ""
	@echo "  $(GREEN)✓ All checks passed$(RESET)"
	@echo ""

check: quality build
	@echo "  $(GREEN)✓ CI check passed$(RESET)"

# ─────────────────────────────────────────────────────────────────────────────
# Dev Environment
# ─────────────────────────────────────────────────────────────────────────────

dev: dev-start
	@echo ""
	@echo "  $(GREEN)✓ Dev environment ready$(RESET)"
	@echo ""
	@echo "    make dev-logs     View logs"
	@echo "    make dev-status  Check status"
	@echo "    make dev-stop    Stop all"
	@echo ""

dev-start: gateway-start
	@$(MAKE) webchat-dev || echo "  $(YELLOW)⚠$(RESET) Webchat skipped (run 'cd webchat && pnpm install' to fix)"

dev-stop: webchat-stop gateway-stop
	@echo "  $(GREEN)✓ Dev environment stopped$(RESET)"

dev-status:
	@./scripts/dev.sh status all

dev-logs:
	@./scripts/dev.sh logs all

dev-reset: dev-stop dev-start

# ─────────────────────────────────────────────────────────────────────────────
# Gateway
# ─────────────────────────────────────────────────────────────────────────────

gateway-start: build
	@./scripts/dev.sh start gateway

gateway-stop:
	@./scripts/dev.sh stop gateway

gateway-status:
	@./scripts/dev.sh status gateway

gateway-logs:
	@./scripts/dev.sh logs gateway

# ─────────────────────────────────────────────────────────────────────────────
# Webchat
# ─────────────────────────────────────────────────────────────────────────────

webchat-dev:
	@./scripts/dev.sh start webchat

webchat-stop:
	@./scripts/dev.sh stop webchat

# ─────────────────────────────────────────────────────────────────────────────
# Clean
# ─────────────────────────────────────────────────────────────────────────────

clean:
	@go clean
	@rm -rf $(BUILD_DIR)
	@rm -f coverage.out
	@echo "  $(GREEN)✓$(RESET) Cleaned"

# ─────────────────────────────────────────────────────────────────────────────
# Help
# ─────────────────────────────────────────────────────────────────────────────

help:
	@echo ""
	@echo "  $(CYAN)━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━$(RESET)"
	@echo "  $(CYAN)  ⚡ HotPlex Worker$(RESET)  $(GIT_SHA)  $(GOOS)/$(GOARCH)"
	@echo "  $(CYAN)━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━$(RESET)"
	@echo ""
	@echo "  $(BOLD)⚡ Start"
	@printf "    $(CYAN)make %-15s$(RESET)  %s\n" "dev"         "Start all services (gateway + webchat)"
	@printf "    $(CYAN)make %-15s$(RESET)  %s\n" "dev-start"   "Start individually"
	@echo ""
	@echo "  $(BOLD)⏹  Stop"
	@printf "    $(CYAN)make %-15s$(RESET)  %s\n" "dev-stop"      "Stop all services"
	@printf "    $(CYAN)make %-15s$(RESET)  %s\n" "gateway-stop"   "Stop gateway"
	@printf "    $(CYAN)make %-15s$(RESET)  %s\n" "webchat-stop"  "Stop webchat"
	@echo ""
	@echo "  $(BOLD)🔧 Build"
	@printf "    $(CYAN)make %-15s$(RESET)  %s\n" "build"   "Build binary"
	@printf "    $(CYAN)make %-15s$(RESET)  %s\n" "run"     "Build and run (foreground)"
	@echo ""
	@echo "  $(BOLD)🧪 Test & Quality"
	@printf "    $(CYAN)make %-15s$(RESET)  %s\n" "test"         "All tests (race, 15m)"
	@printf "    $(CYAN)make %-15s$(RESET)  %s\n" "test-short"   "Short tests (5m)"
	@printf "    $(CYAN)make %-15s$(RESET)  %s\n" "lint"         "Run linter"
	@printf "    $(CYAN)make %-15s$(RESET)  %s\n" "fmt"          "Format code"
	@printf "    $(CYAN)make %-15s$(RESET)  %s\n" "quality"       "fmt + lint + test"
	@printf "    $(CYAN)make %-15s$(RESET)  %s\n" "check"         "quality + build (CI)"
	@echo ""
	@echo "  $(BOLD)📊 Status & Logs"
	@printf "    $(CYAN)make %-15s$(RESET)  %s\n" "dev-status"     "All services"
	@printf "    $(CYAN)make %-15s$(RESET)  %s\n" "gateway-status"  "Gateway"
	@printf "    $(CYAN)make %-15s$(RESET)  %s\n" "dev-logs"      "View all logs"
	@printf "    $(CYAN)make %-15s$(RESET)  %s\n" "gateway-logs"   "Gateway logs"
	@echo ""
	@echo "  $(BOLD)🔄 Workflow"
	@printf "    $(CYAN)make %-15s$(RESET)  %s\n" "dev-reset"   "Restart all services"
	@printf "    $(CYAN)make %-15s$(RESET)  %s\n" "quickstart"  "First-time setup"
	@echo ""
	@echo "  $(BOLD)🧹 Other"
	@printf "    $(CYAN)make %-15s$(RESET)  %s\n" "clean"        "Clean artifacts"
	@printf "    $(CYAN)make %-15s$(RESET)  %s\n" "check-tools"  "Check dev tools"
	@echo ""
	@echo "  $(DIM)Try:  make dev | make test | make check"
	@echo ""

# Catch-all
%:
	@echo ""
	@echo "  $(RED)Unknown: make $@$(RESET)"
	@echo "    make help  Show commands"
	@echo ""
	@exit 1
