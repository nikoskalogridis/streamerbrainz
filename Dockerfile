# Multi-architecture Dockerfile for StreamerBrainz
# Supports: linux/amd64, linux/arm64 (Raspberry Pi 4+)
#
# Build examples:
#   docker build -t streamerbrainz:latest .
#   docker buildx build --platform linux/amd64,linux/arm64 -t streamerbrainz:latest .
#   docker buildx build --platform linux/arm64 -t streamerbrainz:rpi4 .

# Stage 1: Builder
FROM golang:1.23.4-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git make

# Set working directory
WORKDIR /build

# Copy go module files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build arguments for cross-compilation (automatically set by buildx)
ARG TARGETOS=linux
ARG TARGETARCH

# Build all binaries with optimizations
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -v -ldflags "-s -w" -o /bin/streamerbrainz ./cmd/streamerbrainz && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -v -ldflags "-s -w" -o /bin/sbctl ./cmd/sbctl && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -v -ldflags "-s -w" -o /bin/ws_listen ./cmd/ws_listen

# Verify binaries were built
RUN ls -lh /bin/streamerbrainz /bin/sbctl /bin/ws_listen

# Stage 2: Runtime
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    tzdata

# Copy binaries from builder
COPY --from=builder /bin/streamerbrainz /usr/local/bin/
COPY --from=builder /bin/sbctl /usr/local/bin/
COPY --from=builder /bin/ws_listen /usr/local/bin/

# Create non-root user (optional - daemon needs root for IR device access)
RUN addgroup -g 1000 streamerbrainz && \
    adduser -D -u 1000 -G streamerbrainz streamerbrainz

# Set working directory
WORKDIR /home/streamerbrainz

# Default command shows help
CMD ["streamerbrainz", "-help"]

# Metadata
LABEL org.opencontainers.image.title="StreamerBrainz"
LABEL org.opencontainers.image.description="Multi-source volume controller for CamillaDSP"
LABEL org.opencontainers.image.version="1.0.0"
LABEL org.opencontainers.image.authors="StreamerBrainz Contributors"
LABEL org.opencontainers.image.source="https://github.com/yourusername/streamerbrainz"

# Expose webhooks port (Plex integration)
EXPOSE 3001

# Volume for IPC socket (optional)
VOLUME ["/tmp"]

# Health check (optional - checks if daemon is responsive)
# HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
#   CMD sbctl -ipc-socket /tmp/streamerbrainz.sock set-volume -30.0 || exit 1
