.PHONY: all clean install help docker-build docker-build-all docker-build-amd64 docker-build-arm64 docker-push docker-clean build-binaries build-binaries-amd64 build-binaries-arm64 build-binaries-all clean-binaries

# Build output directory
BUILD_DIR := ./bin

# Binary names
DAEMON_BIN := streamerbrainz
CTL_BIN := sbctl
LISTEN_BIN := ws_listen

# Go build flags
GO := go
GOFLAGS := -v
LDFLAGS := -s -w

# Docker configuration
DOCKER_IMAGE := streamerbrainz
DOCKER_TAG := latest
DOCKER_REGISTRY :=

all: $(BUILD_DIR)/$(DAEMON_BIN) $(BUILD_DIR)/$(CTL_BIN) $(BUILD_DIR)/$(LISTEN_BIN)

# Create build directory
$(BUILD_DIR):
	mkdir -p $(BUILD_DIR)

# Build main daemon
$(BUILD_DIR)/$(DAEMON_BIN): $(BUILD_DIR)
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(DAEMON_BIN) ./cmd/streamerbrainz

# Build sbctl utility
$(BUILD_DIR)/$(CTL_BIN): $(BUILD_DIR)
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(CTL_BIN) ./cmd/sbctl

# Build ws_listen example
$(BUILD_DIR)/$(LISTEN_BIN): $(BUILD_DIR)
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(LISTEN_BIN) ./cmd/ws_listen

# Clean build artifacts
clean:
	rm -rf $(BUILD_DIR)
	rm -f $(DAEMON_BIN) $(CTL_BIN)
	rm -f cmd/sbctl/$(CTL_BIN)
	rm -f cmd/ws_listen/$(LISTEN_BIN)

# Install binaries to /usr/local/bin (requires sudo)
install: all
	install -m 755 $(BUILD_DIR)/$(DAEMON_BIN) /usr/local/bin/
	install -m 755 $(BUILD_DIR)/$(CTL_BIN) /usr/local/bin/

# Docker build for current architecture
docker-build:
	docker build -t $(DOCKER_IMAGE):$(DOCKER_TAG) .

# Docker build for all architectures (amd64 + arm64)
docker-build-all:
	./docker-build.sh --all --tag $(DOCKER_TAG)

# Docker build for amd64 only
docker-build-amd64:
	./docker-build.sh --amd64 --tag $(DOCKER_TAG)

# Docker build for arm64 (Raspberry Pi 4+) only
docker-build-arm64:
	./docker-build.sh --arm64 --tag $(DOCKER_TAG)

# Docker build and push to registry
docker-push:
	./docker-build.sh --all --push --tag $(DOCKER_TAG) --registry $(DOCKER_REGISTRY)

# Clean Docker images
docker-clean:
	docker rmi $(DOCKER_IMAGE):$(DOCKER_TAG) 2>/dev/null || true

# Build standalone binaries for all architectures
build-binaries-all:
	./build-binaries.sh --all

# Build standalone binaries for amd64
build-binaries-amd64:
	./build-binaries.sh --amd64

# Build standalone binaries for arm64 (Raspberry Pi 4+)
build-binaries-arm64:
	./build-binaries.sh --arm64

# Build standalone binaries (alias for build-binaries-all)
build-binaries: build-binaries-all

# Clean built binaries
clean-binaries:
	./build-binaries.sh --clean

# Show help
help:
	@echo "Available targets:"
	@echo ""
	@echo "Build targets:"
	@echo "  all               - Build all binaries (default)"
	@echo "  clean             - Remove build artifacts"
	@echo "  install           - Install binaries to /usr/local/bin"
	@echo ""
	@echo "Docker targets:"
	@echo "  docker-build       - Build Docker image for current architecture"
	@echo "  docker-build-all   - Build Docker image for all architectures (amd64, arm64)"
	@echo "  docker-build-amd64 - Build Docker image for amd64 only"
	@echo "  docker-build-arm64 - Build Docker image for arm64 (Raspberry Pi 4+) only"
	@echo "  docker-push        - Build and push multi-arch images to registry"
	@echo "  docker-clean       - Remove Docker images"
	@echo ""
	@echo "Binary build targets (for deployment without Docker):"
	@echo "  build-binaries-all   - Build standalone binaries for all architectures"
	@echo "  build-binaries-amd64 - Build standalone binaries for amd64 only"
	@echo "  build-binaries-arm64 - Build standalone binaries for arm64 (Raspberry Pi 4+)"
	@echo "  build-binaries       - Alias for build-binaries-all"
	@echo "  clean-binaries       - Clean all built binaries"
	@echo ""
	@echo "Other targets:"
	@echo "  help                 - Show this help message"
	@echo ""
	@echo "Binaries are built to: $(BUILD_DIR)/"
	@echo ""
	@echo "Docker configuration:"
	@echo "  DOCKER_IMAGE=$(DOCKER_IMAGE)"
	@echo "  DOCKER_TAG=$(DOCKER_TAG)"
	@echo "  DOCKER_REGISTRY=$(DOCKER_REGISTRY)"
	@echo ""
	@echo "Examples:"
	@echo "  make docker-build"
	@echo "  make docker-build-arm64 DOCKER_TAG=rpi4"
	@echo "  make docker-push DOCKER_REGISTRY=ghcr.io/username"
	@echo "  make build-binaries-arm64  # Build binaries for Raspberry Pi"
	@echo "  make build-binaries-all    # Build for all platforms"
