# HotPlex Worker Gateway - Production Dockerfile
#
# Multi-stage build for production deployment
# Best practices:
#   - Build stage with full Go toolchain
#   - Runtime stage with minimal Alpine
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
ARG GO_VERSION=go1.26.0

WORKDIR /build

# Install build dependencies
RUN apk add --no-cache \
    git \
    make \
    ca-certificates \
    tzdata

# Copy go.mod first for better cache
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build with optimizations and version injection
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w \
        -X main.version=${GIT_SHA}" \
    -o /build/bin/hotplex-worker \
    ./cmd/worker

# ─────────────────────────────────────────────────────────────────────────────
# Stage 2: Runtime
# ─────────────────────────────────────────────────────────────────────────────
FROM alpine:3.21

# Build arguments
ARG GIT_SHA
ARG BUILD_TIME

# Labels for metadata
LABEL maintainer="HotPlex Team <support@hotplex.dev>"
LABEL version="1.0.0"
LABEL git.sha="${GIT_SHA}"
LABEL build.time="${BUILD_TIME}"
LABEL description="HotPlex Worker Gateway - AI Coding Agent access layer"

# Install runtime dependencies
# - ca-certificates: TLS/SSL support
# - sqlite-libs: SQLite database
# - curl: health checks
# - tzdata: timezone support
RUN apk add --no-cache \
    ca-certificates \
    sqlite-libs \
    curl \
    tzdata \
    && rm -rf /var/cache/apk/*

# Create non-root user
RUN addgroup -g 1000 hotplex \
    && adduser -D -u 1000 -G hotplex -s /bin/sh -h /home/hotplex hotplex

# Create directories
RUN mkdir -p /etc/hotplex \
    /var/lib/hotplex/data \
    /var/lib/hotplex/tls \
    /var/log/hotplex \
    && chown -R hotplex:hotplex /etc/hotplex /var/lib/hotplex /var/log/hotplex

WORKDIR /home/hotplex

# Copy binary from builder
COPY --from=builder /build/bin/hotplex-worker /usr/local/bin/hotplex-worker

# Copy default config
COPY --chown=hotplex:hotplex configs/ /etc/hotplex/

# Expose ports
# 8888: WebSocket gateway
# 9999: Admin API
EXPOSE 8888 9999

# Environment variables
ENV HOTPLEX_CONFIG=/etc/hotplex/config.yaml
ENV HOTPLEX_DATA_DIR=/var/lib/hotplex/data
ENV HOTPLEX_LOG_DIR=/var/log/hotplex

# Switch to non-root user
USER hotplex:hotplex

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:9999/admin/health || exit 1

# Entry point
ENTRYPOINT ["/usr/local/bin/hotplex-worker"]

# Default command
CMD ["-config", "/etc/hotplex/config.yaml"]
