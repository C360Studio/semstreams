# SemStreams Production Dockerfile
#
# Multi-stage build for minimal, secure production images
# Supports multi-arch: linux/amd64, linux/arm64
#
# Build from repository root:
#   docker build -t semstreams:latest .
#
# Build with buildx for multi-arch:
#   docker buildx build --platform linux/amd64,linux/arm64 \
#     -t semstreams:latest .

# Build arguments
ARG GO_VERSION=1.25
ARG VERSION=dev
ARG COMMIT_SHA=unknown
ARG BUILD_DATE=unknown

# ============================================================================
# Builder Stage
# ============================================================================
FROM golang:${GO_VERSION}-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /build

# Copy go module files
COPY go.mod go.sum ./

# Download dependencies (cached layer)
RUN go mod download

# Copy source code
COPY . .

# Build arguments for version info
ARG VERSION
ARG COMMIT_SHA
ARG BUILD_DATE
ARG TARGETOS
ARG TARGETARCH

# Build binary with version information
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -ldflags="-w -s \
      -X main.version=${VERSION} \
      -X main.commit=${COMMIT_SHA} \
      -X main.buildDate=${BUILD_DATE}" \
    -o /build/semstreams-bin \
    ./cmd/semstreams

# ============================================================================
# Production Stage
# ============================================================================
FROM alpine:latest AS production

# Install runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    wget

# Create non-root user and group
RUN addgroup -S -g 1000 semstreams && \
    adduser -S -u 1000 -G semstreams -h /app semstreams

# Set working directory
WORKDIR /app

# Copy binary from builder
COPY --from=builder --chown=semstreams:semstreams /build/semstreams-bin /app/semstreams

# Copy example configs (optional, users should mount their own)
COPY --from=builder --chown=semstreams:semstreams /build/configs /app/configs

# Create config directory for mounted configs
RUN mkdir -p /etc/semstreams && \
    chown semstreams:semstreams /etc/semstreams

# Create data directory
RUN mkdir -p /data && \
    chown semstreams:semstreams /data

# Switch to non-root user
USER semstreams

# Expose ports
# HTTP API, Metrics, and UDP input
EXPOSE 8080
EXPOSE 9090
EXPOSE 14550/udp

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Set entrypoint
ENTRYPOINT ["/app/semstreams"]

# Default command: use mounted config or fall back to example
# Users should mount their config at /etc/semstreams/config.json
CMD ["--config", "/etc/semstreams/config.json"]

# Labels
ARG VERSION
ARG COMMIT_SHA
ARG BUILD_DATE
LABEL org.opencontainers.image.title="SemStreams" \
      org.opencontainers.image.description="Semantic stream processing framework for edge deployments" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT_SHA}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.source="https://github.com/c360/semstreams" \
      org.opencontainers.image.vendor="C360" \
      org.opencontainers.image.licenses="MIT"
