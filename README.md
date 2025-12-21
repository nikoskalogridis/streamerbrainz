# argon-camilladsp-remote

**Multi-source volume controller for CamillaDSP with velocity-based control**

A modular Go daemon that controls [CamillaDSP](https://github.com/HEnquist/camilladsp) volume from multiple input sources including IR remotes, command-line tools, audio players (librespot), and custom scripts.

---

## Features

- 🎛️ **Velocity-based volume control** - Smooth, physics-based acceleration/deceleration
- 🔌 **Multi-source input** - IR remote, IPC, command-line, scripts, Spotify (librespot)
- 🔒 **Safety limits** - Configurable min/max volume bounds
- 🚀 **High performance** - 100Hz update rate, minimal latency
- 🔧 **Production-ready** - Comprehensive error handling, logging, and testing
- 🔄 **WebSocket reconnection** - Automatic reconnection to CamillaDSP
- 🎯 **Zero dependencies** - Pure Go, standalone binaries

---

## Quick Start

### Build

```bash
# Build the daemon
go build -o argon-camilladsp-remote .

# Build the CLI tool
go build -o argon-ctl cmd/argon-ctl/main.go
```

### Run

```bash
# Start the daemon (requires root for IR input)
sudo ./argon-camilladsp-remote

# Control volume via CLI
./argon-ctl mute
./argon-ctl set-volume -30.0
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
cd argon-camilladsp-remote
go build -o argon-camilladsp-remote .
go build -o argon-ctl cmd/argon-ctl/main.go
sudo cp argon-camilladsp-remote /usr/local/bin/
sudo cp argon-ctl /usr/local/bin/
```

---

## Usage

### Daemon

```bash
# Basic usage
sudo argon-camilladsp-remote

# Custom configuration
sudo argon-camilladsp-remote \
  -input /dev/input/event6 \
  -ws ws://127.0.0.1:1234 \
  -socket /tmp/argon.sock \
  -min -65.0 \
  -max 0.0 \
  -v
```

**Command-line flags:**
- `-input` - Linux input event device for IR (default: `/dev/input/event6`)
- `-ws` - CamillaDSP WebSocket URL (default: `ws://127.0.0.1:1234`)
- `-socket` - Unix domain socket path for IPC (default: `/tmp/argon-camilladsp.sock`)
- `-min` - Minimum volume in dB (default: `-65.0`)
- `-max` - Maximum volume in dB (default: `0.0`)
- `-update-hz` - Update loop frequency (default: `100`)
- `-vel-max` - Maximum velocity in dB/s (default: `50.0`)
- `-accel-time` - Acceleration time in seconds (default: `0.3`)
- `-decay-tau` - Velocity decay time constant (default: `0.1`)
- `-read-timeout-ms` - WebSocket read timeout (default: `200`)
- `-v` - Verbose logging

### CLI Tool (argon-ctl)

```bash
# Toggle mute
argon-ctl mute

# Set absolute volume
argon-ctl set-volume -30.0
argon-ctl set -25.5

# Simulate IR button presses
argon-ctl volume-up
argon-ctl volume-down
argon-ctl release

# Use custom socket
argon-ctl -socket /tmp/argon.sock mute
```

### Python API

```python
from examples.python_client import ArgonClient

client = ArgonClient()
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
echo '{"type":"toggle_mute"}' | nc -U /tmp/argon-camilladsp.sock
```

### Librespot (Spotify Connect)

```bash
# Configure librespot with the hook script
librespot --onevent /path/to/librespot-hook.sh ...

# Test manually with environment variables
PLAYER_EVENT=volume_changed VOLUME=32768 ./argon-camilladsp-remote librespot-hook -v
PLAYER_EVENT=playing TRACK_ID=test ./argon-camilladsp-remote librespot-hook -v
```

---

## Architecture

```
┌─────────────┐
│  IR Remote  │──┐
└─────────────┘  │
                 │
┌─────────────┐  │     ┌─────────────┐     ┌──────────────┐
│  argon-ctl  │──┼────▶│   Actions   │────▶│    Daemon    │
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
argon-camilladsp-remote/
├── main.go           # Entry point and main event loop
├── daemon.go         # Central daemon loop
├── actions.go        # Action types and JSON encoding
├── velocity.go       # Velocity-based control logic
├── camilladsp.go     # CamillaDSP commands
├── websocket.go      # WebSocket client
├── input.go          # IR input event handling
├── ipc.go            # IPC server
├── constants.go      # Configuration constants
├── cmd/
│   └── argon-ctl/    # CLI tool
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
# Check if socket already exists
rm -f /tmp/argon-camilladsp.sock

# Check IR device permissions
sudo chmod 666 /dev/input/event6
# Or add user to input group
sudo usermod -a -G input $USER
```

### IPC connection refused

```bash
# Check if daemon is running
ps aux | grep argon

# Check socket exists
ls -l /tmp/argon-camilladsp.sock

# Enable verbose logging
sudo ./argon-camilladsp-remote -v
```

### Volume not changing

```bash
# Check CamillaDSP is running
curl http://127.0.0.1:1234/api/v1/status

# Check WebSocket URL
sudo ./argon-camilladsp-remote -ws ws://127.0.0.1:1234 -v
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