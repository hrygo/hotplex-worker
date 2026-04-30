# HotPlex Gateway — Dev Makefile (not for production)
#
#   make help       Show commands
#   make dev        Start dev environment
#   make check      Quality gate (CI)

# ─── Config ────────────────────────────────────────────────────────────────────

BINARY   := hotplex
BUILDDIR := bin
MAIN     := ./cmd/hotplex
CFGDIR   := configs
LOGDIR   := logs

GOOS     := $(shell go env GOOS)
GOARCH   := $(shell go env GOARCH)
LDFLAGS  := -s -w -X main.version=v1.2.0 -X main.buildTime=$(shell date '+%Y-%m-%dT%H:%M:%S%z')

# ─── PHONY ─────────────────────────────────────────────────────────────────────

.PHONY: all help quickstart build run \
        dev dev-stop dev-status dev-logs dev-reset \
        gateway-start gateway-stop gateway-status gateway-logs \
        webchat-dev webchat-stop webchat-embed webchat-rebuild \
        test test-short coverage lint fmt quality check clean

all: help

# ─── Setup ─────────────────────────────────────────────────────────────────────

quickstart: build test-short
	@echo "\n  Setup complete — make dev | make run | make help\n"

# ─── Build ─────────────────────────────────────────────────────────────────────

build: webchat-embed
	@mkdir -p $(BUILDDIR) $(LOGDIR)
	@go build -trimpath -ldflags="$(LDFLAGS)" -o $(BUILDDIR)/$(BINARY)-$(GOOS)-$(GOARCH) $(MAIN)

run: build
	@./$(BUILDDIR)/$(BINARY)-$(GOOS)-$(GOARCH) gateway start -c $(CFGDIR)/config-dev.yaml

webchat-embed:
	@if [ ! -d internal/webchat/out/_next ]; then \
		cd webchat && npm run build && \
		rm -rf ../internal/webchat/out.tmp && cp -r out ../internal/webchat/out.tmp && \
		rm -rf ../internal/webchat/out && mv ../internal/webchat/out.tmp ../internal/webchat/out; \
	fi

webchat-rebuild:
	@cd webchat && npm run build && \
		rm -rf ../internal/webchat/out.tmp && cp -r out ../internal/webchat/out.tmp && \
		rm -rf ../internal/webchat/out && mv ../internal/webchat/out.tmp ../internal/webchat/out

# ─── Test ──────────────────────────────────────────────────────────────────────

test:
	@GORACE="history_size=5" go test -race -timeout 15m ./...

test-short:
	@GORACE="history_size=5" go test -short -race -timeout 5m ./...

coverage:
	@go test -timeout=15m -coverprofile=coverage.out -covermode=atomic \
		$$(go list ./... | grep -v -e 'internal/worker/proc' -e 'internal/worker/pi' -e 'cmd/hotplex')
	@go tool cover -func=coverage.out

# ─── Quality ───────────────────────────────────────────────────────────────────

fmt:
	@go fmt ./...
	@command -v goimports > /dev/null 2>&1 && goimports -w . || true

lint:
	@golangci-lint run ./...

quality: fmt lint test

check: quality build

# ─── Dev ───────────────────────────────────────────────────────────────────────

dev: gateway-start
	@$(MAKE) webchat-dev || echo "  Webchat skipped — cd webchat && pnpm install"

dev-stop: webchat-stop gateway-stop

dev-status:
	@./scripts/dev.sh status all

dev-logs:
	@./scripts/dev.sh logs all

dev-reset: dev-stop dev

gateway-start: build
	@./scripts/dev.sh start gateway

gateway-stop:
	@./scripts/dev.sh stop gateway

gateway-status:
	@./scripts/dev.sh status gateway

gateway-logs:
	@./scripts/dev.sh logs gateway

webchat-dev:
	@./scripts/dev.sh start webchat

webchat-stop:
	@./scripts/dev.sh stop webchat

# ─── Clean ─────────────────────────────────────────────────────────────────────

clean:
	@go clean
	@rm -rf $(BUILDDIR) coverage.out

# ─── Help ──────────────────────────────────────────────────────────────────────

help:
	@echo ""
	@echo "  HotPlex Gateway  $$(git rev-parse --short=8 HEAD 2>/dev/null)  $(GOOS)/$(GOARCH)"
	@echo ""
	@echo "  Dev       make dev          Start gateway + webchat"
	@echo "            make dev-stop     Stop all"
	@echo "            make dev-logs     View logs"
	@echo "            make dev-reset    Restart all"
	@echo ""
	@echo "  Build     make build        Build binary"
	@echo "            make run          Build and run"
	@echo "            make clean        Clean artifacts"
	@echo ""
	@echo "  Test      make test         Full tests (race, 15m)"
	@echo "            make test-short   Quick tests (5m)"
	@echo "            make coverage     Coverage report"
	@echo "            make lint         Linter"
	@echo "            make fmt          Format code"
	@echo "            make check        CI gate (fmt+lint+test+build)"
	@echo ""
