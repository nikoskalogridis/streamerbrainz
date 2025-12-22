# Architecture Documentation

## System Overview

```
┌─────────────────────────────────────────────────────────────────────┐
│                       StreamerBrainz                                │
│                     (Go Event-Driven Daemon)                        │
└─────────────────────────────────────────────────────────────────────┘

Input Layer          Processing Layer         Output Layer
─────────────        ────────────────         ────────────

┌─────────────┐      ┌──────────────┐       ┌──────────────┐
│ IR Remote   │      │ Velocity     │       │ CamillaDSP   │
│   (evdev)   │─────▶│ Controller   │──────▶│  WebSocket   │
└─────────────┘      └──────────────┘       └──────────────┘
      │                      │                      │
      │                      │                      │
   /dev/input/event6    Physics Model         ws://localhost
      │                  (30-1000 Hz)              :1234
      │                      │                      │
      ▼                      ▼                      ▼
  Input Events         Volume Target         Audio Output
  (KEY_VOL*)           (-65 to 0 dB)        (DSP Processing)
```

## Component Architecture

### 1. Main Event Loop

```
┌──────────────────────────────────────────────────────────────┐
│                      main() Goroutine                        │
│                                                              │
│  select {                                                    │
│    case <-sigc:           ──▶ Shutdown & Cleanup            │
│    case err := <-readErr: ──▶ Error Handling & Exit         │
│    case <-updateTicker.C: ──▶ Velocity Update & WS Send     │
│    case ev := <-events:   ──▶ Input Event Processing        │
│  }                                                           │
└──────────────────────────────────────────────────────────────┘
                              │
                              │ Multiplexes 4 event sources
                              ▼
        ┌────────────┬─────────────┬─────────────┬────────────┐
        │  Signals   │   Errors    │   Ticker    │   Input    │
        │  (sigc)    │ (readErr)   │  (30 Hz)    │  (events)  │
        └────────────┴─────────────┴─────────────┴────────────┘
```

### 2. Concurrent Goroutines

```
Goroutine 1: main()
─────────────────────
• Event loop coordination
• Volume state updates (30 Hz)
• WebSocket communication
• Signal handling
• Lifecycle: Program duration

Goroutine 2: readInputEvents()
───────────────────────────────
• Blocking read on /dev/input/eventX
• Binary parsing of input_event structs
• Channel send to main loop
• Lifecycle: Until read error or EOF
```

### 3. Data Flow (Input Event → Volume Change)

```
Time  │ Component           │ Action
──────┼────────────────────┼─────────────────────────────────────
0 ms  │ Linux Kernel       │ IR receiver generates KEY_VOLUMEUP
      │                    │
1 ms  │ readInputEvents()  │ read() syscall returns event
      │                    │ Parses binary struct (24 bytes)
      │                    │ events <- ev (channel send)
      │                    │
2 ms  │ main() event loop  │ Receives event from channel
      │                    │ Switch on ev.Code (KEY_VOLUMEUP)
      │                    │ velState.setHeld(1) [mutex lock]
      │                    │
3 ms  │                    │ Waits for next update tick...
      │                    │
33 ms │ main() ticker      │ Receives <-updateTicker.C
      │                    │ velState.update() [mutex lock]
      │                    │   - Calculates dt
      │                    │   - Updates velocity (accel)
      │                    │   - Updates target position
      │                    │ velState.shouldSendUpdate()
      │                    │   - Checks |target - current| > 0.1
      │                    │
34 ms │ wsClient           │ sendAndRead() [mutex lock]
      │                    │   - json.Marshal({"SetVolume": X})
      │                    │   - websocket.WriteMessage()
      │                    │   - websocket.ReadMessage() [blocks]
      │                    │
35 ms │ CamillaDSP Server  │ Receives SetVolume command
      │                    │ Applies volume to audio pipeline
      │                    │ Returns {"SetVolume": {"result":"Ok"}}
      │                    │
36 ms │ wsClient           │ Parses JSON response
      │                    │ velState.updateVolume(X)
      │                    │ Returns to event loop
```

### 4. State Management

```
┌─────────────────────────────────────────────────────────────┐
│                     velocityState                           │
│                   (Mutex-Protected)                         │
├─────────────────────────────────────────────────────────────┤
│ State Variables:                                            │
│   • targetDB       float64   ─┐                            │
│   • velocityDBPerS float64    │ Physics Model              │
│   • heldDirection  int        │ (Position, Velocity)       │
│   • lastUpdate     time.Time ─┘                            │
│   • currentVolume  float64   ── Server Sync                │
│   • volumeKnown    bool      ── Safety Flag                │
├─────────────────────────────────────────────────────────────┤
│ Configuration:                                              │
│   • velMaxDBPerS   float64   ── Max speed (15 dB/s)        │
│   • accelDBPerS2   float64   ── Acceleration (7.5 dB/s²)   │
│   • decayTau       float64   ── Decay time (0.2 s)         │
│   • minDB / maxDB  float64   ── Clamping limits            │
├─────────────────────────────────────────────────────────────┤
│ Methods (all mutex-protected):                              │
│   • setHeld(dir)      ── Set held direction               │
│   • release()         ── Release button                   │
│   • update()          ── Physics update (30 Hz)           │
│   • updateVolume(v)   ── Sync with server                 │
│   • getTarget()       ── Read target value                │
│   • shouldSendUpdate()── Check if send needed             │
└─────────────────────────────────────────────────────────────┘
```

### 5. WebSocket Client

```
┌─────────────────────────────────────────────────────────────┐
│                        wsClient                             │
│                   (Mutex-Protected)                         │
├─────────────────────────────────────────────────────────────┤
│ State:                                                      │
│   • conn   *websocket.Conn  ── Active connection          │
│   • wsURL  string           ── Server address             │
│   • mu     sync.Mutex       ── Serializes all ops         │
├─────────────────────────────────────────────────────────────┤
│ Methods:                                                    │
│   • connect()              ── Dial WebSocket              │
│   • send(v)                ── One-way message             │
│   • sendAndRead(v, timeout)── Request-response            │
│   • close()                ── Cleanup connection          │
├─────────────────────────────────────────────────────────────┤
│ Error Handling:                                             │
│   • Auto-reconnect on failure (connectWithRetry)          │
│   • 500ms read timeout (configurable)                     │
│   • Graceful degradation                                  │
└─────────────────────────────────────────────────────────────┘
```

### 6. Channel Architecture

```
┌──────────────────────────────────────────────────────────┐
│              Channel Communication                       │
├──────────────────────────────────────────────────────────┤
│                                                          │
│  events chan inputEvent (buffered: 64)                  │
│  ┌─────────────────────────────────────────────┐       │
│  │ [ev0][ev1][ev2][...][empty][empty]...[empty]│       │
│  └─────────────────────────────────────────────┘       │
│      ▲                                    │             │
│      │                                    │             │
│  readInputEvents()                   main() loop        │
│  (producer)                          (consumer)         │
│                                                          │
│  ─────────────────────────────────────────────────      │
│                                                          │
│  readErr chan error (buffered: 1)                       │
│  ┌───────┐                                              │
│  │ [err] │                                              │
│  └───────┘                                              │
│      ▲                                    │             │
│      │                                    │             │
│  readInputEvents()                   main() loop        │
│  (error path)                        (error handler)    │
│                                                          │
│  ─────────────────────────────────────────────────      │
│                                                          │
│  sigc chan os.Signal (buffered: 1)                      │
│  ┌────────┐                                             │
│  │ [SIGINT/TERM]                                        │
│  └────────┘                                             │
│      ▲                                    │             │
│      │                                    │             │
│  OS signal.Notify()                  main() loop        │
│                                      (shutdown)          │
└──────────────────────────────────────────────────────────┘
```

## Physics Model (Velocity-Based Control)

### Velocity Update Algorithm

```
At each tick (dt = 1/30 second):

1. Read held state:
   if heldDirection == UP:
     velocity += acceleration × dt
     velocity = min(velocity, velMax)
   
   if heldDirection == DOWN:
     velocity -= acceleration × dt
     velocity = max(velocity, -velMax)
   
   if heldDirection == NONE:
     velocity *= (1 - dt/decayTau)  // Exponential decay

2. Update position:
   target += velocity × dt
   target = clamp(target, minDB, maxDB)

3. Safety zone check:
   if target > -12 dB:
     velMax = 3 dB/s  // Slow down near 0 dB
   else:
     velMax = 15 dB/s  // Normal speed

4. Boundary handling:
   if target hits limit:
     velocity = 0  // Stop at boundary
```

### State Diagram

```
┌─────────────┐
│   IDLE      │  velocity = 0, no button held
│ (at rest)   │
└──────┬──────┘
       │
       │ KEY_VOLUMEUP pressed
       ▼
┌─────────────┐
│ ACCELERATING│  velocity increasing
│   (up)      │  target moving upward
└──────┬──────┘
       │
       │ KEY_VOLUMEUP released
       ▼
┌─────────────┐
│  DECAYING   │  velocity decreasing exponentially
│   (coast)   │  target still moving but slowing
└──────┬──────┘
       │
       │ velocity → 0
       ▼
┌─────────────┐
│   IDLE      │  Back to rest
│ (at rest)   │
└─────────────┘
```

## Memory Layout

### Static Allocations (Program Lifetime)

```
┌────────────────────────────────────────────────────────┐
│                    Heap Memory                         │
├────────────────────────────────────────────────────────┤
│                                                        │
│  wsClient                      ~500 bytes             │
│  ┌──────────────────────────────────────┐            │
│  │ mutex | conn ptr | wsURL string      │            │
│  └──────────────────────────────────────┘            │
│                                                        │
│  velocityState                 ~120 bytes             │
│  ┌──────────────────────────────────────┐            │
│  │ mutex | floats×9 | int | time | bool│            │
│  └──────────────────────────────────────┘            │
│                                                        │
│  events channel               ~1,632 bytes            │
│  ┌──────────────────────────────────────┐            │
│  │ hchan (96) + buffer (64×24)          │            │
│  └──────────────────────────────────────┘            │
│                                                        │
│  readErr channel                ~112 bytes            │
│  sigc channel                   ~112 bytes            │
│  updateTicker                   ~200 bytes            │
│  Input buffer + reader          ~104 bytes            │
│                                                        │
│  Total Persistent:             ~2,780 bytes ≈ 3 KB   │
└────────────────────────────────────────────────────────┘
```

### Stack Layout (Per Goroutine)

```
┌────────────────────────────────────────────────────────┐
│              Stack Memory (2 goroutines)               │
├────────────────────────────────────────────────────────┤
│                                                        │
│  Goroutine 1: main()                2 KB              │
│  ┌──────────────────────────────────────┐            │
│  │ Flags, locals, select temps          │            │
│  │ (actual usage: ~500 bytes)           │            │
│  └──────────────────────────────────────┘            │
│                                                        │
│  Goroutine 2: readInputEvents()     2 KB              │
│  ┌──────────────────────────────────────┐            │
│  │ Local vars, event struct             │            │
│  │ (actual usage: ~300 bytes)           │            │
│  └──────────────────────────────────────┘            │
│                                                        │
│  Total Stack:                           4 KB          │
└────────────────────────────────────────────────────────┘
```

## Synchronization Primitives

### Mutex Lock Hierarchy

```
Level 1: velocityState.mu
  ├─ Held by: main() goroutine only
  ├─ Frequency: ~70 Hz (30 Hz updates + events)
  ├─ Hold time: ~1 μs (arithmetic only)
  └─ No nesting: Cannot cause deadlock

Level 2: wsClient.mu
  ├─ Held by: main() goroutine only
  ├─ Frequency: ~6 Hz (volume + mute commands)
  ├─ Hold time: ~50 μs (send) to 500 ms (timeout)
  └─ No nesting: Cannot cause deadlock

No mutex dependencies → Deadlock impossible
```

### Lock Contention Map

```
Time      CPU 0              CPU 1
──────────────────────────────────────
0.000 ms  velocityState.mu ┐
0.001 ms  (update)         │ [no contention - single goroutine]
0.001 ms  └─ unlock        ┘

0.100 ms  wsClient.mu ┐
0.150 ms  (send+read) │     [no contention - single goroutine]
0.600 ms  └─ unlock   ┘

All synchronization is uncontended (single-threaded access)
```

## Initialization Sequence

```
1. Parse command-line flags
   └─ Input device, WS URL, tuning parameters

2. Open /dev/input/eventX
   ├─ Validate file descriptor
   └─ Register for cleanup (defer)

3. Connect to WebSocket
   ├─ Retry loop until success
   └─ Log connection status

4. *** SAFETY: Query initial volume ***
   ├─ Try getCurrentVolume()
   │  ├─ Success → Use actual server volume
   │  └─ Failure → SET server to -45 dB (safeDefaultDB)
   └─ Initialize velocityState with known value

5. Setup signal handling
   └─ Register SIGINT, SIGTERM handlers

6. Start input reader goroutine
   ├─ Launch readInputEvents()
   └─ Blocking read loop

7. Start update ticker
   └─ 30 Hz (or configured frequency)

8. Enter main event loop
   └─ select {} multiplexing
```

## Shutdown Sequence

```
Signal received (SIGINT or SIGTERM)
       │
       ▼
┌─────────────────┐
│ Stop ticker     │ ── Prevents new update events
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Close WebSocket │ ── Releases network resources
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Close input FD  │ ── Unblocks readInputEvents()
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ main() returns  │ ── Program exits
└─────────────────┘
         │
         ▼
┌─────────────────┐
│ OS cleanup      │ ── Goroutines terminated
│                 │    Channels garbage collected
│                 │    Memory freed
└─────────────────┘
```

## Error Handling Strategy

### Input Errors

```
readInputEvents() error
       │
       ▼
readErr <- err
       │
       ▼
main() receives error
       │
       ▼
Log error message
       │
       ▼
Clean shutdown
```

### WebSocket Errors

```
send/sendAndRead() error
       │
       ▼
Return error to caller
       │
       ▼
sendWithRetry() catches error
       │
       ▼
Log "ws send failed"
       │
       ▼
connectWithRetry()
   ├─ Retry loop (500ms intervals)
   └─ Blocks until reconnected
       │
       ▼
Resume normal operation
```

### Volume Query Failure (Initialization)

```
getCurrentVolume() error
       │
       ▼
Log warning
       │
       ▼
*** SAFETY FIX ***
setVolumeCommand(-45.0)
   ├─ Success → Server at -45 dB
   └─ Failure → Log error, proceed with caution
       │
       ▼
Initialize velocityState
       │
       ▼
Continue with safe default
```

## Performance Characteristics

### Hot Path (Update Loop)

```
Function Call Stack:              Cost (μs)   Frequency
─────────────────────────────────────────────────────────
main() select                        0.1       Blocked
  └─ <-updateTicker.C                0.1       30 Hz
      └─ velocityState.update()
          ├─ time.Now()               0.02     VDSO
          ├─ float arithmetic (×10)   0.2      CPU
          ├─ conditional (×5)         0.1      CPU
          └─ mutex unlock             0.01     Uncontended
      └─ shouldSendUpdate()
          ├─ mutex lock/unlock        0.02     Uncontended
          └─ float comparison         0.01     CPU
      └─ (conditional) setVolumeCommand()
          ├─ json.Marshal()           5.0      Alloc
          ├─ websocket.Write()       20.0      Syscall
          ├─ websocket.Read()       100.0      Block
          └─ json.Unmarshal()         5.0      Alloc

Total per update (no send):          0.5 μs   Low cost
Total per update (with send):      130.0 μs   When needed
```

### Cold Path (Input Event)

```
IR key press
  └─ Kernel input subsystem          ~100 μs
      └─ /dev/input ready
          └─ readInputEvents() wakes  ~1 μs
              └─ io.ReadFull()        ~2 μs
                  └─ binary.Read()    ~1 μs
                      └─ events <- ev ~0.5 μs
                          └─ main() select wakes ~1 μs
                              └─ setHeld() ~0.1 μs

Total input event latency:           ~105 μs
```

## Security Considerations

### Input Validation

```
✓ Input device path: User-provided (trust required)
✓ WebSocket URL: User-provided (trust required)
✓ Volume range: Validated (-min <= -max)
✓ Update Hz: Clamped (1-1000)
✓ Binary input: Fixed size (24 bytes, kernel-validated)
✓ JSON parsing: Go standard library (safe)
```

### Resource Limits

```
✓ Channel buffers: Fixed size (bounded memory)
✓ Goroutines: Fixed count (2, never grows)
✓ File descriptors: 2 (input + websocket, properly closed)
✓ Memory: ~8 MB RSS (no unbounded growth)
```

### Privilege Requirements

```
⚠ Requires read access to /dev/input/eventX
  └─ Options:
      1. Run as root (not recommended)
      2. Add user to 'input' group (recommended)
      3. Use udev rules for specific device
```

## Design Rationale

### Why Event-Driven?

```
Alternative: Polling input at 100 Hz
  Cost: 100 × (syscall + overhead) = ~1% CPU idle

Current: Blocking read (event-driven)
  Cost: 0% CPU when idle

Savings: ~1% CPU continuously
```

### Why Velocity-Based?

```
Alternative: Direct command per IR repeat event
  Issues:
    - IR repeat rate varies (200-500ms)
    - Jerky volume changes
    - Server spam (50+ commands/s possible)

Current: Physics-based velocity model
  Benefits:
    - Smooth, predictable control
    - IR-independent behavior
    - Server-friendly (throttled to 30/s max)
    - Tunable feel (accel, decay, max speed)
```

### Why Go?

```
Language Comparison:
┌──────────┬──────────┬────────┬───────────┬─────────┐
│ Language │ RSS      │ Safety │ Dev Speed │ Runtime │
├──────────┼──────────┼────────┼───────────┼─────────┤
│ C        │ ~3 MB    │ Low    │ Slow      │ None    │
│ Go       │ ~8 MB    │ High   │ Fast      │ Small   │
│ Python   │ ~40 MB   │ High   │ Faster    │ Large   │
│ Rust     │ ~5 MB    │ High   │ Slow      │ None    │
└──────────┴──────────┴────────┴───────────┴─────────┘

Go provides best balance of:
  • Memory efficiency (vs Python)
  • Safety (vs C)
  • Productivity (vs C/Rust)
  • Acceptable overhead (+5 MB vs C)
```

---

**Document Version**: 1.0  
**Last Updated**: December 21, 2024  
**Architecture Stability**: Stable