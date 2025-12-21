# Refactoring Steps: IR Controller → Multi-Source Daemon

## Overview

This document tracks the evolution of argon-camilladsp-remote from a simple IR volume controller into a multi-source audio daemon that can accept commands from IR remotes, librespot, IPC clients, and UIs.

---

## Goals

Transform the architecture from:
```
IR Events → Direct Control → CamillaDSP
```

To:
```
IR Events ────┐
librespot ────┤
IPC Clients ──┼──→ Actions → Daemon Brain → Policy → CamillaDSP
UI Commands ──┤
Encoders ─────┘
```

**Key Benefits**:
- Single point of control (no race conditions)
- Policy enforcement in one place
- Easy to add new input sources
- Can implement complex behaviors (fade, mute rules, source priority)

---

## Step 1: Identify the Implicit Daemon Core ✅

**Status**: COMPLETE

**Changes**:
- Added documentation comments to identify the existing event loop
- Marked areas that will be refactored
- Added TODO comments showing future Action-based approach

**File**: `main.go`

**What we learned**:
The `for { select { ... } }` loop in `main()` is already our daemon core, but:
- It's tied to IR events
- It directly manipulates `velocityState` and `CamillaDSP`
- Nothing else can talk to it

**Code Location**: Lines ~455-550 (main event loop)

---

## Step 2: Introduce Action Types ✅

**Status**: COMPLETE

**Changes**:
- Created `Action` interface (marker interface)
- Added concrete action types:
  - `VolumeHeld` - Button held (direction: -1, 0, +1)
  - `VolumeRelease` - Button released
  - `ToggleMute` - Mute toggle request
  - `SetVolumeAbsolute` - Direct volume set (with origin tracking)

**File**: `main.go` (lines ~59-87)

**Design Decision**:
- Simple interface{} marker pattern (Go idiomatic)
- Each action is a struct with clear fields
- `SetVolumeAbsolute` includes `Origin` field for future policy decisions

**Example**:
```go
// Old way (direct manipulation)
velState.setHeld(1)

// New way (intent-based)
actions <- VolumeHeld{Direction: 1}
```

---

## Step 3: Create Central Action Loop ✅

**Status**: COMPLETE

**Changes**:
- Created `runDaemon()` function - the "daemon brain"
- Created `handleAction()` - translates actions into state changes
- Created `applyVolume()` - single point for CamillaDSP volume updates
- Moved ticker logic into daemon goroutine
- Main loop now only translates IR events into Actions

**File**: `main.go` (lines ~90-180)

**Architecture**:
```
┌──────────────────────────────────────────┐
│         runDaemon() Goroutine            │
│                                          │
│  for {                                   │
│    select {                              │
│      case act := <-actions:              │
│        handleAction() // Mutate intent   │
│                                          │
│      case <-ticker.C:                    │
│        velState.update()                 │
│        if shouldSend:                    │
│          applyVolume() // Talk to DSP    │
│    }                                     │
│  }                                       │
└──────────────────────────────────────────┘
```

**Key Principle**: 
- Only `runDaemon()` modifies `velocityState`
- Only `applyVolume()` talks to CamillaDSP
- `handleAction()` mutates intent, not directly executing

**Concurrency Safety**:
- Single goroutine owns state (no mutex needed for velocity)
- Future input sources (IPC, librespot) just push to `actions` channel
- No race conditions possible

---

## Step 4: Move Velocity + DSP Logic Behind Policy

**Status**: COMPLETE (accomplished in Step 3)

**What Changed**:
- Velocity updates happen only in ticker case
- DSP communication centralized in `applyVolume()`
- Policy can be added in one place

**Future Policy Additions** (examples):
```go
// In applyVolume() - before sending to DSP
if configSwitchInProgress {
    // Don't send volume updates during config switch
    return
}

if muteState {
    // Force volume to -∞ regardless of target
    targetDB = -150.0
}

if fadeInProgress {
    // Override target with fade curve
    targetDB = calculateFade(fadeStart, fadeEnd, fadeProgress)
}
```

---

## Step 5: IR Becomes an Input Module

**Status**: COMPLETE (accomplished in Step 3)

**Changes**:
- IR event handling now emits Actions instead of direct control
- Mechanical transformation (low risk)

**Before**:
```go
case KEY_VOLUMEUP:
    velState.setHeld(1)
```

**After**:
```go
case KEY_VOLUMEUP:
    actions <- VolumeHeld{Direction: 1}
```

**Input Module Pattern**:
```
IR Hardware → /dev/input/eventX → readInputEvents() → events channel
                                                           ↓
                                         Main Loop (translator)
                                                           ↓
                                                   actions channel
                                                           ↓
                                                    runDaemon()
```

---

## Step 6: Add IPC Server

**Status**: PLANNED

**Design**:
```go
go ipcServer("/run/audiod.sock", actions)
```

**IPC Server Responsibilities**:
- Listen on Unix domain socket
- Accept JSON messages
- Parse into Actions
- Push to actions channel

**Example IPC Message**:
```json
{
  "type": "SetVolumeAbsolute",
  "db": -30.0,
  "origin": "librespot"
}
```

**Security Considerations**:
- Unix socket permissions (0600 or 0660 with group)
- Optional: Client authentication via SO_PEERCRED
- Rate limiting per client

**Files to Create**:
- `ipc.go` - IPC server implementation
- `actions.go` - Action JSON marshaling/unmarshaling

---

## Step 7: librespot Hook Client

**Status**: PLANNED

**Design**:
Create a subcommand mode:
```bash
argon-camilladsp-remote hook librespot
```

**Hook Behavior**:
1. Read environment variables (`PLAYER_EVENT`, `TRACK_NAME`, etc.)
2. Construct IPC message
3. Send to `/run/audiod.sock`
4. Exit

**librespot Configuration**:
```toml
[librespot]
onevent = "/usr/local/bin/argon-camilladsp-remote hook librespot"
```

**Example Hook Logic**:
```go
func hookLibrespot() {
    event := os.Getenv("PLAYER_EVENT")
    
    switch event {
    case "start":
        sendIPC(SetVolumeAbsolute{Db: -30.0, Origin: "librespot"})
    case "stop":
        sendIPC(SetVolumeAbsolute{Db: -45.0, Origin: "librespot"})
    case "volume_set":
        vol := parseVolume(os.Getenv("VOLUME"))
        sendIPC(SetVolumeAbsolute{Db: vol, Origin: "librespot"})
    }
}
```

**Files to Create**:
- `cmd/hook/librespot.go` - Hook implementation
- `cmd/hook/ipc_client.go` - IPC client library

---

## Step 8: Config Switching Support

**Status**: PLANNED

**New Action Type**:
```go
type SwitchConfig struct {
    Name   string
    Origin string
}
```

**Policy in Daemon**:
```go
func handleConfigSwitch(cfg SwitchConfig) {
    // 1. Fade volume down
    fadeVolume(-65.0, 1*time.Second)
    
    // 2. Switch config
    sendConfigSwitch(cfg.Name)
    
    // 3. Wait for DSP to stabilize
    time.Sleep(500 * time.Millisecond)
    
    // 4. Fade volume back up
    fadeVolume(previousVolume, 1*time.Second)
}
```

**Benefits**:
- No click/pop during config change
- Smooth user experience
- All existing input sources can trigger config switch
- Policy enforced in one place

---

## Implementation Checklist

### Phase 1: Core Refactoring ✅
- [x] Step 1: Document existing daemon core
- [x] Step 2: Introduce Action types
- [x] Step 3: Create central action loop
- [x] Step 4: Move logic behind policy (implicit in Step 3)
- [x] Step 5: IR as input module (implicit in Step 3)

### Phase 2: IPC Infrastructure
- [ ] Step 6a: Create IPC server (Unix socket)
- [ ] Step 6b: Create Action JSON encoding/decoding
- [ ] Step 6c: Add authentication/rate limiting
- [ ] Step 6d: Test with manual client

### Phase 3: librespot Integration
- [ ] Step 7a: Create hook subcommand framework
- [ ] Step 7b: Implement librespot hook
- [ ] Step 7c: Create IPC client library
- [ ] Step 7d: Integration testing with librespot

### Phase 4: Advanced Features
- [ ] Step 8a: Add SwitchConfig action
- [ ] Step 8b: Implement fade logic
- [ ] Step 8c: Config switch policy
- [ ] Step 8d: Source priority rules

---

## Testing Strategy

### Unit Tests
```go
func TestActionHandling(t *testing.T) {
    actions := make(chan Action, 10)
    
    // Test VolumeHeld
    actions <- VolumeHeld{Direction: 1}
    // Assert velocity increases
    
    // Test SetVolumeAbsolute
    actions <- SetVolumeAbsolute{Db: -30.0, Origin: "test"}
    // Assert volume set correctly
}
```

### Integration Tests
```bash
# Start daemon
./argon-camilladsp-remote &

# Send IPC command
echo '{"type":"SetVolumeAbsolute","db":-30.0,"origin":"test"}' | \
    nc -U /run/audiod.sock

# Verify volume changed
# Query CamillaDSP
```

### Stress Tests
```bash
# Concurrent actions from multiple sources
for i in {1..100}; do
    ./argon-camilladsp-remote hook librespot &
done

# Should not crash, race detector clean
```

---

## Performance Impact

### Before Refactoring
- 1 goroutine (main)
- 1 goroutine (input reader)
- **Total: 2 goroutines**

### After Phase 1 (Current)
- 1 goroutine (main - input translator)
- 1 goroutine (input reader)
- 1 goroutine (daemon brain)
- **Total: 3 goroutines**

### After Phase 2 (IPC)
- +1 goroutine (IPC server)
- **Total: 4 goroutines**

### After Phase 3 (Full)
- Same as Phase 2
- Hook processes are ephemeral (not long-running)
- **Total: 4 goroutines steady state**

**Memory Impact**: +~10 KB (action channel buffer + goroutine stacks)

**CPU Impact**: Negligible (still event-driven)

---

## Migration Path

### For Existing Users
1. **Phase 1** (current): Drop-in replacement, no config changes
2. **Phase 2**: Optional IPC socket, IR still works
3. **Phase 3**: Add librespot hook if desired
4. **Phase 4**: Opt-in to advanced features

### Backward Compatibility
- All command-line flags preserved
- IR behavior unchanged
- New features are additive

---

## Documentation Updates Needed

- [ ] Update README with IPC examples
- [ ] Document IPC message format
- [ ] Create librespot integration guide
- [ ] Add architecture diagram
- [ ] Update PERFORMANCE_ANALYSIS.md

---

## Risks and Mitigations

### Risk: Increased Complexity
**Mitigation**: 
- Gradual rollout (8 clear steps)
- Each step is independently testable
- Can stop at any phase

### Risk: Goroutine Coordination
**Mitigation**:
- Single-owner pattern (no mutex needed)
- Channels for communication (Go best practice)
- Race detector in CI

### Risk: IPC Security
**Mitigation**:
- Unix socket with proper permissions
- Optional client authentication
- Rate limiting per connection

---

## Success Metrics

- ✅ IR control still works (Phase 1)
- ✅ Zero race conditions (race detector clean)
- ✅ Code compiles at each step
- [ ] librespot integration working (Phase 3)
- [ ] Multiple simultaneous clients (Phase 2)
- [ ] Smooth config switching (Phase 4)

---

**Last Updated**: December 21, 2024  
**Current Phase**: 1 (Core Refactoring - COMPLETE)  
**Next Step**: Phase 2 - IPC Infrastructure