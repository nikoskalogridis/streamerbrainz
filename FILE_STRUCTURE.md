# File Structure Documentation

## Overview

The argon-camilladsp-remote project is now organized into modular files for better maintainability and clarity. Each file has a specific responsibility in the architecture.

---

## File Organization

### Core Application Files

#### `main.go` (4.7 KB)
**Purpose**: Application entry point and main event loop

**Responsibilities**:
- Command-line flag parsing
- Application initialization
- Main event loop (input translation)
- Shutdown handling

**Key Functions**:
- `main()` - Entry point, coordinates all components

**Flow**:
```
1. Parse flags
2. Open input device
3. Connect to WebSocket
4. Initialize velocity state with safe volume
5. Start daemon goroutine
6. Start input reader goroutine
7. Run main loop (translate IR → Actions)
```

---

#### `actions.go` (1.0 KB)
**Purpose**: Action type definitions for command-based architecture

**Responsibilities**:
- Define the `Action` interface
- Define concrete action types

**Types**:
- `Action` - Marker interface for all commands
- `VolumeHeld` - Button held (direction: -1, 0, +1)
- `VolumeRelease` - Button released
- `ToggleMute` - Mute toggle request
- `SetVolumeAbsolute` - Direct volume set with origin tracking

**Usage**:
```go
actions <- VolumeHeld{Direction: 1}
actions <- SetVolumeAbsolute{Db: -30.0, Origin: "librespot"}
```

---

#### `daemon.go` (2.8 KB)
**Purpose**: Central daemon loop ("daemon brain")

**Responsibilities**:
- Process Actions from all sources
- Update velocity state periodically
- Apply policy decisions
- Coordinate CamillaDSP communication

**Key Functions**:
- `runDaemon()` - Main daemon event loop
- `handleAction()` - Action dispatcher (mutates intent)
- `applyVolume()` - Single point for volume updates to CamillaDSP

**Architecture**:
```
runDaemon() runs in dedicated goroutine
  ├─ Receives Actions from channel
  ├─ Updates velocity state (30 Hz ticker)
  └─ Sends volume to CamillaDSP when needed
```

**Concurrency Safety**:
- Only this goroutine modifies velocityState
- Only this goroutine talks to CamillaDSP
- Prevents race conditions

---

#### `velocity.go` (3.6 KB)
**Purpose**: Velocity-based volume control physics

**Responsibilities**:
- Manage velocity state
- Physics calculations (acceleration, decay)
- Safety zone enforcement

**Key Type**:
- `velocityState` - State machine for smooth volume control

**Key Functions**:
- `newVelocityState()` - Constructor
- `setHeld()` / `release()` - Button state
- `update()` - Physics update (called at configured Hz)
- `updateVolume()` - Sync with server
- `getTarget()` - Read target volume
- `shouldSendUpdate()` - Decide if update needed

**Physics Model**:
```
velocity += acceleration × dt  (when held)
velocity *= decay_factor       (when released)
target += velocity × dt
```

---

#### `websocket.go` (2.7 KB)
**Purpose**: WebSocket client for CamillaDSP communication

**Responsibilities**:
- Manage WebSocket connection lifecycle
- Send/receive JSON messages
- Auto-reconnect on failure

**Key Type**:
- `wsClient` - WebSocket connection wrapper

**Key Functions**:
- `newWSClient()` - Constructor
- `connect()` - Establish connection
- `send()` - One-way message
- `sendAndRead()` - Request-response with timeout
- `close()` - Clean shutdown
- `connectWithRetry()` - Retry loop
- `sendJSONText()` - JSON encoding
- `sendWithRetry()` - Command with auto-reconnect

**Usage**:
```go
ws := newWSClient("ws://localhost:1234")
connectWithRetry(ws, wsURL, verbose)
response, err := ws.sendAndRead(cmd, timeout)
```

---

#### `camilladsp.go` (1.9 KB)
**Purpose**: CamillaDSP command implementations

**Responsibilities**:
- Implement specific CamillaDSP commands
- Parse responses
- Handle protocol details

**Key Functions**:
- `setVolumeCommand()` - Set volume to specific value
- `getCurrentVolume()` - Query current volume
- `handleMuteCommand()` - Toggle mute

**Protocol**:
```go
// Set volume
cmd := map[string]any{"SetVolume": -30.0}
response := ws.sendAndRead(cmd, timeout)

// Get volume
cmd := "GetVolume"
response := ws.sendAndRead(cmd, timeout)
```

---

#### `input.go` (978 B)
**Purpose**: Linux input event reading

**Responsibilities**:
- Read from /dev/input/eventX
- Parse binary input_event structs
- Send events to channel

**Key Type**:
- `inputEvent` - Linux input_event structure

**Key Function**:
- `readInputEvents()` - Blocking read loop (runs in goroutine)

**Implementation**:
- Uses buffer reuse optimization
- Handles malformed events gracefully
- Sends errors to dedicated channel

---

#### `constants.go` (861 B)
**Purpose**: Global constants and configuration defaults

**Responsibilities**:
- Define Linux input event codes
- Define default configuration values

**Constant Groups**:
- Input event types (EV_KEY)
- Key codes (KEY_VOLUMEUP, KEY_VOLUMEDOWN, KEY_MUTE)
- Event values (press, release, repeat)
- Physics defaults (velocity, acceleration, decay)
- Safety defaults (safe volume, safety zone)

---

## File Dependency Graph

```
main.go
  ├─ imports: actions.go
  ├─ imports: daemon.go
  ├─ imports: velocity.go
  ├─ imports: websocket.go
  ├─ imports: camilladsp.go
  ├─ imports: input.go
  └─ imports: constants.go

daemon.go
  ├─ uses: actions.go (Action types)
  ├─ uses: velocity.go (velocityState)
  ├─ uses: websocket.go (wsClient)
  └─ uses: camilladsp.go (commands)

velocity.go
  └─ uses: constants.go (safety zone constants)

websocket.go
  └─ standalone (only external deps)

camilladsp.go
  └─ uses: websocket.go (wsClient)

input.go
  └─ standalone (only stdlib)

actions.go
  └─ standalone (just type definitions)

constants.go
  └─ standalone (just constants)
```

---

## Module Responsibilities Summary

| File | Lines | Primary Responsibility | Goroutines |
|------|-------|------------------------|------------|
| `main.go` | ~150 | Entry point, coordination | Main |
| `daemon.go` | ~100 | Central action loop | Daemon brain |
| `velocity.go` | ~140 | Physics/state management | None (owned by daemon) |
| `websocket.go` | ~135 | Network communication | None (synchronous) |
| `camilladsp.go` | ~80 | Protocol implementation | None |
| `input.go` | ~45 | Event reading | Input reader |
| `actions.go` | ~30 | Type definitions | N/A |
| `constants.go` | ~30 | Configuration | N/A |

---

## Design Principles

### 1. Single Responsibility
Each file has one clear purpose. Easy to understand and modify.

### 2. Dependency Direction
Dependencies flow downward. No circular dependencies.

### 3. Testability
Each file can be tested independently:
- `velocity.go` - Unit test physics
- `daemon.go` - Test action handling
- `camilladsp.go` - Mock WebSocket responses
- `input.go` - Mock file descriptor

### 4. Extensibility
New features can be added without touching core files:
- Add new Action type → `actions.go`
- Add new CamillaDSP command → `camilladsp.go`
- Add new input source → New file + emit Actions

---

## Build Information

**Compilation**:
```bash
go build -o argon-camilladsp-remote
```

**Total Size**: ~18 KB source code (8 files)
**Binary Size**: ~7.4 MB (includes gorilla/websocket)

---

## Future File Additions

### Phase 2: IPC Infrastructure
```
ipc_server.go       - Unix socket server
actions_json.go     - JSON encoding/decoding for Actions
```

### Phase 3: librespot Integration
```
cmd/hook/main.go         - Hook subcommand entry
cmd/hook/librespot.go    - librespot event handler
cmd/hook/ipc_client.go   - IPC client library
```

### Phase 4: Advanced Features
```
policy.go           - Policy engine (fade, priority, etc.)
config_switch.go    - Config switching logic
```

---

## Migration Notes

### From Monolithic (Before)
- Single `main.go` file: ~650 lines
- Hard to navigate
- Mixed concerns

### To Modular (After)
- 8 focused files: ~150 lines each average
- Clear separation of concerns
- Easy to find relevant code
- Better for team development

### Backward Compatibility
- 100% compatible
- Same command-line interface
- Same behavior
- Just better organized

---

## Quick Navigation Guide

**To understand...**
- **Application flow** → Start with `main.go`
- **Action handling** → `daemon.go`
- **Volume physics** → `velocity.go`
- **Network protocol** → `websocket.go` + `camilladsp.go`
- **Input reading** → `input.go`
- **Command types** → `actions.go`
- **Configuration** → `constants.go`

**To modify...**
- **Add new action** → `actions.go` + handler in `daemon.go`
- **Change physics** → `velocity.go`
- **Add CamillaDSP command** → `camilladsp.go`
- **Change defaults** → `constants.go`
- **Add input source** → New file, emit to `actions` channel

---

**Created**: December 22, 2024  
**Structure**: Modular (8 files)  
**Status**: ✅ Production Ready