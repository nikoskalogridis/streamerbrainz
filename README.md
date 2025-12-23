# StreamerBrainz

**Multi-source volume controller for CamillaDSP with velocity-based control**

StreamerBrainz sits between your inputs/players and CamillaDSP. It listens for volume/mute intent (IR remotes, Spotify Connect via librespot hooks, Plex webhooks) and applies it to CamillaDSP using a velocity-based engine for smooth “press-and-hold” control with safety limits.

**Security note:** StreamerBrainz is expected to run on a trusted local network and should not be exposed directly to the public internet (e.g., the webhook listener).

---

## Features

- 🎛️ **Velocity-based volume control** - Smooth, physics-based acceleration/deceleration
- 🔌 **Multi-source input** - IR remote + player integrations (librespot hook, Plex/Plexamp webhook)
- 🔒 **Safety limits** - Configurable min/max volume bounds
- 🔧 **Operationally friendly** - Works well as a systemd `--user` service (example unit included)

---

## Quick Start

### Build

#### Native Build

```bash
# Build all binaries using Makefile (recommended)
make

# Or build manually
go build -o bin/streamerbrainz ./cmd/streamerbrainz

# Clean build artifacts
make clean
```

All binaries are built to the `./bin` directory.

#### Cross-Compilation with Docker (For Deployment)

Build standalone binaries for multiple architectures without installing Go on target systems:

```bash
# Build for Raspberry Pi 4+ (ARM64)
make build-binaries-arm64

# Build for x86_64 servers
make build-binaries-amd64

# Build for all architectures
make build-binaries-all

# Or use the script directly
./build-binaries.sh --arm64
./build-binaries.sh --amd64
./build-binaries.sh --all
```

**Output structure:**
```
bin/
├── amd64/           # x86_64 binaries (compressed with UPX)
│   └── streamerbrainz
└── arm64/           # ARM64 binaries for Raspberry Pi 4+
    └── streamerbrainz
```

**Deploy to Raspberry Pi:**
```bash
# Copy binaries to Raspberry Pi
scp bin/arm64/* pi@raspberrypi:/usr/local/bin/

# Or use rsync
rsync -av bin/arm64/ pi@raspberrypi:/usr/local/bin/
```

**Features:**
- ✅ Fully static binaries (no dependencies)
- ✅ Compressed with UPX (~60% size reduction)
- ✅ Ready to run on target systems
- ✅ No Go installation required on targets



### Run

StreamerBrainz is typically run under systemd as a user service. An example unit file is provided:

- `examples/streamerbrainz.service`

For how to operate the daemon and where integration setup lives, see **Usage** below.

For ad-hoc debugging you can run it manually:

```bash
./bin/streamerbrainz -help
./bin/streamerbrainz -version
./bin/streamerbrainz -log-level debug
```

---

## Installation

### Prerequisites

#### For Native Build
- Go 1.23+ (for building from source)
- CamillaDSP running with WebSocket enabled (`-pPORT`)
- IR input device (e.g., `/dev/input/event6`) for remote control
- Linux kernel with evdev support

#### For Docker Builds
- Docker with buildx support
- For multi-arch container builds: `docker buildx create --name multiarch --use`
- For cross-compilation: Docker 20.10+ (buildx included)

### From Source

```bash
git clone https://github.com/nikoskalogridis/streamerbrainz
cd streamerbrainz

# Build all binaries
make

# Install to /usr/local/bin (requires sudo)
sudo make install

# Or manually
sudo cp bin/streamerbrainz /usr/local/bin/
```

---

## Usage

### Normal operation: systemd

StreamerBrainz is typically run under systemd as a user service. An example unit file is provided:

- `examples/streamerbrainz.service`

### Debugging: run manually

Manual execution is mainly useful for debugging or experimenting with flags:

```bash
# Show help / version
streamerbrainz -help
streamerbrainz -version

# Debug logging
streamerbrainz -log-level debug
```

### Integrations

- Spotify (librespot): see `docs/spotify.md`
- Plex/Plexamp webhooks: see `docs/plexamp.md`

### Flags

The daemon is configured via command-line flags. For the authoritative list, run:

```bash
streamerbrainz -help
```

Notes:
- `-ipc-socket` is an internal mechanism used by `streamerbrainz librespot-hook` to forward librespot events into the daemon.

---



## Documentation

- [Architecture](docs/ARCHITECTURE.md) - System design and internals

- [CamillaDSP integration](docs/camilladsp.md) - Setup/configuration/troubleshooting
- [IR integration (Linux evdev)](docs/ir.md) - Setup/configuration/troubleshooting
- [Plex Integration (Webhooks)](docs/plexamp.md) - User setup/configuration/troubleshooting
- [Spotify integration (librespot)](docs/spotify.md) - User setup/configuration/troubleshooting
- [Planned Features](docs/PLANNED.md) - Intended (not yet implemented) features
- [Development](docs/DEVELOPMENT.md) - Building, testing, and contributing

---



## Development

Developer notes (repo layout, build/test, adding new actions) live here:
- [Development](docs/DEVELOPMENT.md)

---

## Troubleshooting

### Daemon won't start

```bash
# Show help to verify parameters
./bin/streamerbrainz -help

# Run in foreground with debug logging to see configuration
./bin/streamerbrainz -log-level debug
```

**Note:** The webhooks HTTP server always runs on the configured port (default 3001), regardless of whether Plex integration is enabled.

### IR input / permissions issues

See: `docs/ir.md`

### CamillaDSP connection / volume not changing

See: `docs/camilladsp.md`

### Librespot hook / IPC issues

See: `docs/spotify.md`

### Plex hook / IPC issues

See: `docs/plexamp.md`

---

## Contributing

Contributions are welcome! Please:

1. Follow Go best practices and project code style
2. Add tests for new features
3. Update documentation
4. Ensure `go build` passes with no warnings

---

## License

[Your License Here]

---

## Credits

- Built for [CamillaDSP](https://github.com/HEnquist/camilladsp) by Henrik Enquist
- Inspired by the need for smooth, safe volume control in audiophile systems
- Uses velocity-based control for natural feel

---

## See Also

- [CamillaDSP](https://github.com/HEnquist/camilladsp) - The audio DSP engine
- [librespot](https://github.com/librespot-org/librespot) - Open-source Spotify Connect client
- [Plex Media Server](https://www.plex.tv/) - Media server with webhook support
