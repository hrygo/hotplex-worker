# HotPlex Worker Gateway - Production Dockerfile
#
# Multi-stage build for production deployment
# Best practices:
#   - Build stage with full Go toolchain
#   - Runtime stage with minimal Alpine + necessary runtimes (Node, Python)
#   - Non-root user
#   - Health check
#   - Proper signal handling
#   - Security hardening
#

# ─────────────────────────────────────────────────────────────────────────────
# Stage 1: Build
# ─────────────────────────────────────────────────────────────────────────────
FROM golang:1.26-alpine AS builder

# Build arguments for version injection
ARG GIT_SHA
ARG BUILD_TIME

WORKDIR /build

# Install build dependencies
RUN apk add --no-cache \
    git \
    make \
    gcc \
    musl-dev \
    ca-certificates \
    tzdata

# Copy go.mod first for better cache
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy source code
COPY . .

# Build with optimizations and version injection
# Fixed path: ./cmd/hotplex instead of ./cmd/worker
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=1 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w \
    -X 'github.com/hrygo/hotplex/internal/version.GitCommit=${GIT_SHA}' \
    -X 'github.com/hrygo/hotplex/internal/version.BuildDate=${BUILD_TIME}'" \
    -o /build/bin/hotplex \
    ./cmd/hotplex

# ─────────────────────────────────────────────────────────────────────────────
# Stage 2: Runtime
# ─────────────────────────────────────────────────────────────────────────────
FROM alpine:3.21

# Build arguments
ARG GIT_SHA
ARG BUILD_TIME

# Labels for metadata
LABEL maintainer="HotPlex Team <support@hotplex.dev>"
LABEL version="1.7.0"
LABEL git.sha="${GIT_SHA}"
LABEL build.time="${BUILD_TIME}"
LABEL description="HotPlex Worker Gateway - AI Coding Agent access layer"

# Install runtime dependencies
# - ca-certificates: TLS/SSL support
# - curl: health checks
# - tzdata: timezone support
# - git: required by coding agents
# - nodejs & npm: for Claude Code
# - python3: for STT server
# - sqlite: for database backups
RUN apk add --no-cache \
    ca-certificates \
    curl \
    tzdata \
    git \
    nodejs \
    npm \
    python3 \
    py3-pip \
    sqlite \
    && rm -rf /var/cache/apk/*

# Install Claude Code CLI (optional, can also be mounted or installed via env)
# Note: We don't pre-install it to keep the image size down, 
# but we provide the environment.
# RUN npm install -g @anthropic-ai/claude-code

# Create non-root user
RUN addgroup -g 1000 hotplex \
    && adduser -D -u 1000 -G hotplex -s /bin/sh -h /home/hotplex hotplex

# Create directories
RUN mkdir -p /etc/hotplex \
    /var/lib/hotplex/data \
    /var/lib/hotplex/tls \
    /var/log/hotplex \
    /home/hotplex/.claude \
    && chown -R hotplex:hotplex /etc/hotplex /var/lib/hotplex /var/log/hotplex /home/hotplex/.claude

WORKDIR /home/hotplex

# Copy binary from builder
COPY --from=builder /build/bin/hotplex /usr/local/bin/hotplex

# Copy default config and assets
COPY --chown=hotplex:hotplex configs/ /etc/hotplex/
COPY --chown=hotplex:hotplex scripts/ /home/hotplex/scripts/

# Expose ports
# 8888: WebSocket gateway
# 9999: Admin API
EXPOSE 8888 9999

# Environment variables
ENV HOTPLEX_CONFIG=/etc/hotplex/config.yaml
ENV HOTPLEX_DATA_DIR=/var/lib/hotplex/data
ENV HOTPLEX_LOG_DIR=/var/log/hotplex
ENV PATH="/usr/local/bin:/home/hotplex/.npm-global/bin:${PATH}"

# Switch to non-root user
USER hotplex:hotplex

# Health check using the admin API
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD curl -f http://localhost:9999/admin/health/ready || exit 1

# Entry point
ENTRYPOINT ["/usr/local/bin/hotplex"]

# Default command
CMD ["gateway", "start", "--config", "/etc/hotplex/config.yaml"]
