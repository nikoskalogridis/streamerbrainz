# StreamerBrainz

**Multi-source volume controller for CamillaDSP with velocity-based control**

A modular Go daemon that controls [CamillaDSP](https://github.com/HEnquist/camilladsp) volume from multiple input sources including IR remotes, command-line tools, audio players (librespot, Plexamp), and custom scripts.

---

## Features

- 🎛️ **Velocity-based volume control** - Smooth, physics-based acceleration/deceleration
- 🔌 **Multi-source input** - IR remote, IPC, command-line, scripts, Spotify (librespot), Plex/Plexamp
- 🔒 **Safety limits** - Configurable min/max volume bounds
- 🚀 **High performance** - 100Hz update rate, minimal latency
- 🔧 **Production-ready** - Comprehensive error handling, logging, and testing
- 🔄 **WebSocket reconnection** - Automatic reconnection to CamillaDSP
- 🎯 **Zero dependencies** - Pure Go, standalone binaries

---

## Quick Start

### Build

```bash
# Build all binaries using Makefile (recommended)
make

# Or build manually
go build -o bin/streamerbrainz .
go build -o bin/sbctl ./cmd/sbctl
go build -o bin/ws_listen ./cmd/ws_listen

# Clean build artifacts
make clean
```

All binaries are built to the `./bin` directory.

### Run

```bash
# Show help and all available options
./bin/streamerbrainz -help

# Show version
./bin/streamerbrainz -version

# Start the daemon (requires root for IR input)
sudo ./bin/streamerbrainz

# Control volume via CLI
./bin/sbctl mute
./bin/sbctl set-volume -30.0
```

---

## Installation

### Prerequisites

- Go 1.16+ (for building)
- CamillaDSP running with WebSocket enabled (`-pPORT`)
- IR input device (e.g., `/dev/input/event6`) for remote control
- Linux kernel with evdev support

### From Source

```bash
git clone <repository-url>
cd streamerbrainz

# Build all binaries
make

# Install to /usr/local/bin (requires sudo)
sudo make install

# Or manually
sudo cp bin/streamerbrainz /usr/local/bin/
sudo cp bin/sbctl /usr/local/bin/
```

---

## Usage

### Daemon

```bash
# Show comprehensive help
streamerbrainz -help

# Show version
streamerbrainz -version

# Basic usage
sudo streamerbrainz

# Custom configuration
streamerbrainz \
  -ir-device /dev/input/event6 \
  -camilladsp-ws-url ws://127.0.0.1:1234 \
  -ipc-socket /tmp/streamerbrainz.sock \
  -camilladsp-min-db -65.0 \
  -camilladsp-max-db 0.0 \
  -log-level debug

# Run with debug logging to see all parameters
sudo streamerbrainz -log-level debug
```

**Command-line flags:**
- `-ir-device string` - Linux input event device for IR (default: `/dev/input/event6`)
- `-camilladsp-ws-url string` - CamillaDSP WebSocket URL (default: `ws://127.0.0.1:1234`)
- `-camilladsp-ws-timeout-ms int` - Websocket read timeout in ms (default: `500`)
- `-camilladsp-min-db float` - Minimum volume in dB (default: `-65.0`)
- `-camilladsp-max-db float` - Maximum volume in dB (default: `0.0`)
- `-camilladsp-update-hz int` - Update loop frequency in Hz (default: `30`)
- `-vel-max-db-per-sec float` - Maximum velocity in dB/s (default: `15.0`)
- `-vel-accel-time float` - Time to reach max velocity in seconds (default: `2.0`)
- `-vel-decay-tau float` - Velocity decay time constant in seconds (default: `0.2`)
- `-ipc-socket string` - Unix domain socket path for IPC (default: `/tmp/streamerbrainz.sock`)
- `-webhooks-port int` - Webhooks HTTP listener port (default: `3001`)
- `-plex-server-url string` - Plex server URL (enables Plex integration when set)
- `-plex-token-file string` - Path to file with Plex authentication token
- `-plex-machine-id string` - Plex player machine identifier
- `-log-level string` - Log level: error, warn, info, debug (default: `info`)
- `-version` - Print version and exit
- `-help` - Print comprehensive help message

Run `streamerbrainz -help` for detailed usage examples and notes.

### CLI Tool (sbctl)

```bash
# Toggle mute
sbctl mute

# Set absolute volume
sbctl set-volume -30.0
sbctl set -25.5

# Simulate IR button presses
sbctl volume-up
sbctl volume-down
sbctl release

# Use custom socket
sbctl -ipc-socket /tmp/streamerbrainz.sock mute
```

### Python API

```python
from examples.python_client import StreamerBrainzClient

client = StreamerBrainzClient()
client.toggle_mute()
client.set_volume(-30.0)
client.volume_up()
client.release()
```

### Bash Scripting

```bash
# Source the bash client
./examples/bash_client.sh mute
./examples/bash_client.sh set -30.0

# Or use raw JSON
echo '{"type":"toggle_mute"}' | nc -U /tmp/streamerbrainz.sock
```

### Librespot (Spotify Connect)

```bash
# Show librespot-hook help
streamerbrainz librespot-hook -help

# Configure librespot to use the hook
librespot --onevent streamerbrainz librespot-hook ...

# Test manually with environment variables
PLAYER_EVENT=volume_changed VOLUME=32768 ./streamerbrainz librespot-hook -log-level debug
PLAYER_EVENT=playing TRACK_ID=test ./streamerbrainz librespot-hook -log-level debug

# Use custom socket
PLAYER_EVENT=playing ./streamerbrainz librespot-hook -ipc-socket /tmp/custom.sock
```

**Librespot hook options:**
- `-ipc-socket string` - Unix domain socket path for IPC (default: `/tmp/streamerbrainz.sock`)
- `-log-level string` - Log level: error, warn, info, debug (default: `info`)
- `-help` - Print librespot-hook help message

### Plexamp/Plex Webhook

The main daemon includes a webhooks HTTP server that always runs. Plex integration is automatically enabled when you provide the required Plex configuration parameters.

```bash
# Start daemon with Plexamp webhook integration
streamerbrainz \
  -plex-server-url http://plex.home.arpa:32400 \
  -plex-token-file /path/to/plex-token \
  -plex-machine-id YOUR_MACHINE_IDENTIFIER

# With custom webhook port and debug logging
streamerbrainz \
  -webhooks-port 8080 \
  -plex-server-url http://192.168.1.100:32400 \
  -plex-token-file /path/to/plex-token \
  -plex-machine-id YOUR_MACHINE_ID \
  -log-level debug
```

**Plex webhook options:**
- `-webhooks-port int` - HTTP webhook listener port (default: `3001`)
- `-plex-server-url string` - Plex server URL (e.g., `http://plex.home.arpa:32400`) - enables Plex integration when set
- `-plex-token-file string` - Path to file containing Plex authentication token (required for Plex)
- `-plex-machine-id string` - Player machine identifier to filter sessions (required for Plex)

**Setup:**
1. Get your Plex token from [Plex support](https://support.plex.tv/articles/204059436-finding-an-authentication-token-x-plex-token/)
2. Find your player's machine identifier:
   ```bash
   # Use the helper script (requires curl)
   ./examples/get-plex-machine-id.sh plex.home.arpa:32400 YOUR_PLEX_TOKEN
   ```
3. Start the daemon with required Plex parameters (integration enables automatically)
4. Configure Plex webhooks in Settings > Webhooks to point to `http://your-server:3001/webhooks/plex`

**How it works:**
- The daemon runs an HTTP webhook server alongside IR input and IPC handlers
- When Plex sends a webhook, the server queries `/status/sessions?X-Plex-Token=XXX`
- The response is parsed (XML) and filtered by `machineIdentifier` to find the specific player
- A `PlexStateChanged` action is sent directly to the action channel with track info (title, artist, album, state, position)
- Currently logs the event; future versions can trigger actions based on playback state (e.g., pause fade-out)

---

## Architecture

```
┌─────────────┐
│  IR Remote  │──┐
└─────────────┘  │
                 │
┌─────────────┐  │     ┌─────────────┐     ┌──────────────┐
│    sbctl    │──┼────▶│   Actions   │────▶│    Daemon    │
└─────────────┘  │     │   Channel   │     │     Loop     │
                 │     └─────────────┘     └──────┬───────┘
┌─────────────┐  │                                │
│   Scripts   │──┘                                ▼
└─────────────┘                          ┌─────────────────┐
                                         │  CamillaDSP     │
                                         │  (WebSocket)    │
                                         └─────────────────┘
```

### Key Components

1. **Action-Based Architecture** - All inputs translated to typed actions
2. **Central Daemon Loop** - Single goroutine owns state and CamillaDSP communication
3. **Velocity Control** - Physics-based smooth volume changes
4. **IPC Server** - Unix domain socket for external control
5. **WebSocket Client** - Automatic reconnection to CamillaDSP

---

## IPC Protocol

The daemon accepts JSON commands via a Unix domain socket.

### Request Format

```json
{
  "type": "action_type",
  "data": { ... }
}
```

### Response Format

Success:
```json
{"status": "ok"}
```

Error:
```json
{"status": "error", "error": "message"}
```

### Action Types

| Action | Type | Data | Example |
|--------|------|------|---------|
| Toggle Mute | `toggle_mute` | - | `{"type":"toggle_mute"}` |
| Set Volume | `set_volume_absolute` | `db`, `origin` | `{"type":"set_volume_absolute","data":{"db":-30.0,"origin":"cli"}}` |
| Volume Up | `volume_held` | `direction`: 1 | `{"type":"volume_held","data":{"direction":1}}` |
| Volume Down | `volume_held` | `direction`: -1 | `{"type":"volume_held","data":{"direction":-1}}` |
| Release | `volume_release` | - | `{"type":"volume_release"}` |

---

## Documentation

- [Quick Start Guide](docs/QUICKSTART.md) - Detailed usage examples
- [Architecture](ARCHITECTURE.md) - System design and internals
- [Phase 2: IPC](docs/phase2-ipc.md) - IPC implementation details
- [File Structure](FILE_STRUCTURE.md) - Code organization
- [Protocol](protocol.md) - CamillaDSP WebSocket protocol

---

## Testing

```bash
# Run integration tests
./test-ipc.sh

# Run with verbose output
./test-ipc.sh -v

# Test with custom socket
./test-ipc.sh -s /tmp/custom.sock
```

---

## Development

### Project Structure

```
streamerbrainz/
├── main.go           # Entry point, main event loop, and help system
├── daemon.go         # Central daemon loop
├── actions.go        # Action types and JSON encoding
├── velocity.go       # Velocity-based control logic
├── camilladsp.go     # CamillaDSP commands
├── websocket.go      # WebSocket client
├── input.go          # IR input event handling
├── ipc.go            # IPC server
├── librespot.go      # Librespot integration
├── plexamp.go        # Plexamp/Plex webhook integration
├── constants.go      # Configuration constants
├── Makefile          # Build system
├── bin/              # Compiled binaries (created by make)
│   ├── streamerbrainz
│   ├── sbctl
│   └── ws_listen
├── cmd/
│   ├── sbctl/        # CLI tool
│   └── ws_listen/    # WebSocket listener example
├── examples/
│   ├── python_client.py
│   └── bash_client.sh
├── docs/
│   ├── QUICKSTART.md
│   └── phase2-ipc.md
└── test-ipc.sh       # Integration tests
```

### Adding New Action Types

1. Define action struct in `actions.go`:
```go
type MyAction struct {
    Value string `json:"value"`
}
```

2. Add to `MarshalAction()` and `UnmarshalAction()`:
```go
case "my_action":
    var a MyAction
    if err := json.Unmarshal(env.Data, &a); err != nil {
        return nil, err
    }
    return a, nil
```

3. Handle in `daemon.go`:
```go
case MyAction:
    // Handle action
```

---

## Troubleshooting

### Daemon won't start

```bash
# Show help to verify parameters
./bin/streamerbrainz -help

# Check if socket already exists
rm -f /tmp/streamerbrainz.sock

# Check IR device permissions
sudo chmod 666 /dev/input/event6
# Or add user to input group
sudo usermod -a -G input $USER

# Run with debug logging to see configuration
sudo ./bin/streamerbrainz -log-level debug
```

**Note:** The webhooks HTTP server always runs on the configured port (default 3001), regardless of whether Plex integration is enabled.

### IPC connection refused

```bash
# Check if daemon is running
ps aux | grep streamerbrainz

# Check IPC socket exists
ls -l /tmp/streamerbrainz.sock

# Enable debug logging
sudo ./streamerbrainz -log-level debug
```

### Volume not changing

```bash
# Check CamillaDSP is running
curl http://127.0.0.1:1234/api/v1/status

# Check WebSocket URL
sudo ./streamerbrainz -camilladsp-ws-url ws://127.0.0.1:1234 -log-level debug
```

---

## Roadmap

- [x] **Phase 1**: Core refactoring and modular architecture
- [x] **Phase 2**: IPC server and multi-source input
- [x] **Phase 3**: librespot integration
- [ ] **Phase 4**: Advanced features (fade, config switching, source priority)

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