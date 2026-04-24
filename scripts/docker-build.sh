#!/usr/bin/env bash
#
# HotPlex Worker Gateway - Docker Build Script
#
# Builds a production-ready Docker image with:
#   - Multi-stage build (minimal final image)
#   - Non-root user
#   - Health check
#   - Proper signal handling
#   - Security hardening
#
# Usage:
#   ./scripts/docker-build.sh [tag] [options]
#
# Options:
#   --push           Push image to registry after build
#   --no-cache       Build without cache
#   --platform ARCH  Build for specific platform (e.g., linux/amd64)
#

set -euo pipefail

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

# Parse arguments
TAG="${1:-hotplex:latest}"
PUSH=false
NO_CACHE=""
PLATFORM=""

shift || true
while [[ $# -gt 0 ]]; do
    case $1 in
        --push)
            PUSH=true
            shift
            ;;
        --no-cache)
            NO_CACHE="--no-cache"
            shift
            ;;
        --platform)
            PLATFORM="--platform $2"
            shift 2
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Get version info
GIT_SHA=$(git rev-parse --short=8 HEAD 2>/dev/null || echo "unknown")
BUILD_TIME=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
GO_VERSION=$(go version | awk '{print $3}')

log_info "Building image: $TAG"
log_info "Git SHA: $GIT_SHA"
log_info "Build time: $BUILD_TIME"

# Build Docker image
docker build \
    $NO_CACHE \
    $PLATFORM \
    --build-arg GIT_SHA="$GIT_SHA" \
    --build-arg BUILD_TIME="$BUILD_TIME" \
    --build-arg GO_VERSION="$GO_VERSION" \
    -t "$TAG" \
    -f Dockerfile \
    .

log_info "Image built successfully: $TAG"

# Show image size
IMAGE_SIZE=$(docker images "$TAG" --format "{{.Size}}")
log_info "Image size: $IMAGE_SIZE"

# Push if requested
if [[ "$PUSH" == true ]]; then
    log_info "Pushing image to registry..."
    docker push "$TAG"
    log_info "Image pushed: $TAG"
fi

# Show usage
cat <<EOF

${BLUE}Usage:${NC}

  Run with default config:
    docker run -p 8080:8888 -p 9080:9999 $TAG

  Run with custom config:
    docker run -p 8080:8888 -p 9080:9999 \\
      -v /path/to/config.yaml:/etc/hotplex/config.yaml \\
      -e HOTPLEX_JWT_SECRET=your-secret \\
      $TAG

  Run with TLS:
    docker run -p 8443:8443 -p 9080:9999 \\
      -v /path/to/tls.crt:/etc/hotplex/tls/server.crt \\
      -v /path/to/tls.key:/etc/hotplex/tls/server.key \\
      -e HOTPLEX_JWT_SECRET=your-secret \\
      $TAG

  Health check:
    docker exec <container> wget -q -O- http://localhost:9999/admin/health

${BLUE}Environment Variables:${NC}

  HOTPLEX_JWT_SECRET     JWT secret (required)
  HOTPLEX_ADMIN_TOKEN_1  Admin token 1 (optional)
  HOTPLEX_DB_PATH        Database path (default: /var/lib/hotplex/data/hotplex.db)

${BLUE}Volumes:${NC}

  /etc/hotplex/          Configuration directory
  /var/lib/hotplex/      Data directory (SQLite)
  /var/log/hotplex/      Log directory

${BLUE}Ports:${NC}

  8080  WebSocket gateway
  9080  Admin API

EOF
