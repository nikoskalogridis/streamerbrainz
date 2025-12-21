.PHONY: all clean install help

# Build output directory
BUILD_DIR := ./builds

# Binary names
DAEMON_BIN := argon-camilladsp-remote
CTL_BIN := argon-ctl
LISTEN_BIN := ws_listen

# Go build flags
GO := go
GOFLAGS := -v
LDFLAGS := -s -w

all: $(BUILD_DIR)/$(DAEMON_BIN) $(BUILD_DIR)/$(CTL_BIN) $(BUILD_DIR)/$(LISTEN_BIN)

# Create build directory
$(BUILD_DIR):
	mkdir -p $(BUILD_DIR)

# Build main daemon
$(BUILD_DIR)/$(DAEMON_BIN): $(BUILD_DIR)
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(DAEMON_BIN) .

# Build argon-ctl utility
$(BUILD_DIR)/$(CTL_BIN): $(BUILD_DIR)
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(CTL_BIN) ./cmd/argon-ctl

# Build ws_listen example
$(BUILD_DIR)/$(LISTEN_BIN): $(BUILD_DIR)
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(LISTEN_BIN) ./cmd/ws_listen

# Clean build artifacts
clean:
	rm -rf $(BUILD_DIR)
	rm -f $(DAEMON_BIN) $(CTL_BIN)
	rm -f cmd/argon-ctl/$(CTL_BIN)
	rm -f cmd/ws_listen/$(LISTEN_BIN)

# Install binaries to /usr/local/bin (requires sudo)
install: all
	install -m 755 $(BUILD_DIR)/$(DAEMON_BIN) /usr/local/bin/
	install -m 755 $(BUILD_DIR)/$(CTL_BIN) /usr/local/bin/

# Show help
help:
	@echo "Available targets:"
	@echo "  all      - Build all binaries (default)"
	@echo "  clean    - Remove build artifacts"
	@echo "  install  - Install binaries to /usr/local/bin"
	@echo "  help     - Show this help message"
	@echo ""
	@echo "Binaries are built to: $(BUILD_DIR)/"
