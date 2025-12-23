.PHONY: all clean install help build-binaries build-binaries-amd64 build-binaries-arm64 build-binaries-all clean-binaries

# Build output directory
BUILD_DIR := ./bin

# Binary name
DAEMON_BIN := streamerbrainz

# Go build flags
GO := go
GOFLAGS := -v
LDFLAGS := -s -w

all: $(BUILD_DIR)/$(DAEMON_BIN)

# Create build directory
$(BUILD_DIR):
	mkdir -p $(BUILD_DIR)

# Build main daemon
$(BUILD_DIR)/$(DAEMON_BIN): $(BUILD_DIR)
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(DAEMON_BIN) ./cmd/streamerbrainz



# Clean build artifacts
clean:
	rm -rf $(BUILD_DIR)
	rm -f $(DAEMON_BIN)

# Install binary to /usr/local/bin (requires sudo)
install: all
	install -m 755 $(BUILD_DIR)/$(DAEMON_BIN) /usr/local/bin/

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
	@echo "Native build targets:"
	@echo "  all                 - Build streamerbrainz (default)"
	@echo "  clean               - Remove build artifacts"
	@echo "  install             - Install streamerbrainz to /usr/local/bin"
	@echo ""
	@echo "Docker-based binary builds (for deployment without Docker):"
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
