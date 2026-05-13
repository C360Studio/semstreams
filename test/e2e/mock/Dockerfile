# Mock OpenAI Server for E2E Testing
#
# Lightweight mock server that provides deterministic OpenAI-compatible responses
# for testing agentic components without external LLM dependencies.
#
# Build from repository root:
#   docker build -t mock-openai:latest -f test/e2e/mock/Dockerfile .

ARG GO_VERSION=1.26

# Build stage
FROM golang:${GO_VERSION}-alpine AS builder

RUN apk add --no-cache ca-certificates

WORKDIR /build

# Copy go module files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build mock server binary
ARG TARGETOS
ARG TARGETARCH

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -ldflags="-w -s" \
    -o /build/mock-openai \
    ./test/e2e/mock/cmd

# Production stage
FROM alpine:latest

RUN apk add --no-cache ca-certificates curl

# Create non-root user
RUN addgroup -S -g 1000 mockllm && \
    adduser -S -u 1000 -G mockllm mockllm

WORKDIR /app

COPY --from=builder --chown=mockllm:mockllm /build/mock-openai /app/mock-openai

USER mockllm

EXPOSE 8080

HEALTHCHECK --interval=5s --timeout=3s --start-period=2s --retries=3 \
    CMD curl -f http://localhost:8080/health || exit 1

ENTRYPOINT ["/app/mock-openai"]
CMD ["--port", "8080"]
