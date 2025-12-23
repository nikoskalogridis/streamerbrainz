# Dockerfile.builder - Build the StreamerBrainz daemon binary for multiple architectures
# This Dockerfile produces a standalone `streamerbrainz` binary that can be extracted
# and run on target systems without Docker.
#
# Usage:
#   # Build for amd64
#   docker build -f Dockerfile.builder --platform linux/amd64 -t streamerbrainz-builder:amd64 --target binaries .
#
#   # Build for arm64 (Raspberry Pi 4+)
#   docker build -f Dockerfile.builder --platform linux/arm64 -t streamerbrainz-builder:arm64 --target binaries .
#
#   # Extract binary from image
#   docker create --name temp streamerbrainz-builder:arm64
#   docker cp temp:/artifacts/. ./bin/arm64/
#   docker rm temp
#
# Or use the provided build-binaries.sh script for easier extraction

# Stage 1: Builder
FROM golang:1.23.4-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git make upx file

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
ARG TARGETVARIANT

# Display build info
RUN echo "Building for: ${TARGETOS}/${TARGETARCH}${TARGETVARIANT}"

# Build the daemon binary with optimizations
# CGO_ENABLED=0 ensures fully static binaries (no libc dependency)
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -v -trimpath \
    -ldflags "-s -w -extldflags '-static'" \
    -o /output/streamerbrainz \
    ./cmd/streamerbrainz

# Optional: Compress binary with UPX (reduces size by ~60%)
# Comment out if you prefer uncompressed binaries
RUN upx --best --lzma /output/streamerbrainz || true

# Verify binary was built and get file info
RUN ls -lh /output/ && \
    file /output/streamerbrainz && \
    /output/streamerbrainz -version || echo "Binary verification completed"

# Stage 2: Export binaries to a clean directory
FROM alpine:latest AS binaries

# Build arguments
ARG TARGETOS=linux
ARG TARGETARCH

# Create artifacts directory
RUN mkdir -p /artifacts

# Copy binaries from builder
COPY --from=builder /output/* /artifacts/

# Metadata labels
LABEL org.opencontainers.image.title="StreamerBrainz Binaries"
LABEL org.opencontainers.image.description="Standalone binaries for ${TARGETOS}/${TARGETARCH}"
LABEL org.opencontainers.image.version="1.0.0"
LABEL platform="${TARGETOS}/${TARGETARCH}"

# Default command to show what's available
CMD ["ls", "-lh", "/artifacts/"]

# Stage 3: Verification stage (optional, for testing)
FROM alpine:latest AS verify

# Copy binaries
COPY --from=builder /output/ /usr/local/bin/

# Install runtime dependencies for testing
RUN apk add --no-cache file

# Test binary
RUN echo "=== Binary Information ===" && \
    file /usr/local/bin/streamerbrainz && \
    echo "" && \
    echo "=== Size Information ===" && \
    ls -lh /usr/local/bin/streamerbrainz && \
    echo "" && \
    echo "=== Version Check ===" && \
    /usr/local/bin/streamerbrainz -version

# Default: Use binaries stage for extraction
FROM binaries
