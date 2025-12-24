# Rotary Encoder Support

StreamerBrainz supports rotary encoders for precise volume control, with intelligent velocity detection for "fast spinning" behavior.

## Overview

Rotary encoders provide a different interaction model than keyboards or IR remotes:

- **Step-based**: Each detent (click) is a discrete volume change
- **Immediate**: No press/hold/release semantics
- **Bidirectional**: Clockwise increases, counter-clockwise decreases
- **Velocity-aware**: Fast spinning can trigger larger steps

This implementation keeps rotary encoder logic completely separate from the keyboard/IR velocity engine, avoiding conflicts and maintaining predictable behavior.

## Configuration

### Basic Setup

```yaml
ir:
  input_devices:
    - path: /dev/input/by-id/usb-Griffin_PowerMate-event-if00
      type: rotary

rotary:
  db_per_step: 0.5              # Volume change per detent
  velocity_window_ms: 200        # Time window for velocity detection
  velocity_multiplier: 2.0       # Multiplier when spinning fast
  velocity_threshold: 3          # Steps needed to trigger velocity mode
```

### Device Types

StreamerBrainz supports two input device types:

1. **`key`** - For keyboards, IR remotes, and key-based devices
   - Sends `EV_KEY` events (press/release)
   - Uses velocity/hold/repeat system
   - Examples: FLIRC IR receivers, keyboards, media keypads

2. **`rotary`** - For rotary encoders and dials
   - Sends `EV_REL` events (relative movement)
   - Step-based control with optional velocity detection
   - Examples: Griffin PowerMate, ShuttleXpress, custom encoders

### Mixed Device Setup

You can use multiple devices simultaneously:

```yaml
ir:
  input_devices:
    # IR remote for couch control
    - path: /dev/input/event6
      type: key
    
    # Rotary encoder for desk
    - path: /dev/input/event7
      type: rotary
    
    # Keyboard media keys
    - path: /dev/input/event3
      type: key

rotary:
  db_per_step: 0.5
  velocity_multiplier: 2.0
  velocity_threshold: 3
```

## How It Works

### Event Types

Rotary encoders can send events in different ways:

#### EV_REL Events (Most Common)
```
Event: type 2 (EV_REL), code 7 (REL_DIAL), value 1     # Clockwise
Event: type 2 (EV_REL), code 7 (REL_DIAL), value -1    # Counter-clockwise
```

Supported codes:
- `REL_DIAL` (0x07) - Standard dial/knob
- `REL_WHEEL` (0x08) - Scroll wheel
- `REL_MISC` (0x09) - Miscellaneous rotary input

For these devices, set `type: rotary`.

#### EV_KEY Events (Less Common)
```
Event: type 1 (EV_KEY), code 115 (KEY_VOLUMEUP), value 1    # Press
Event: type 1 (EV_KEY), code 115 (KEY_VOLUMEUP), value 0    # Release
```

Some encoders emulate keyboard keys. For these, set `type: key` to use the hold/repeat system.

### Velocity Detection

The velocity detection system measures how fast you're turning the encoder:

```
Slow turn:  1 step every 500ms → 0.5 dB/step (normal)
Fast spin:  5 steps in 200ms   → 1.0 dB/step (velocity mode)
```

**Algorithm**:
1. Track recent steps in a sliding time window
2. Count steps in the same direction
3. If count ≥ threshold, apply velocity multiplier
4. Otherwise, use normal step size

**Benefits**:
- Fine control for small adjustments (slow turns)
- Fast changes for large sweeps (fast spins)
- No mode switching - automatic based on behavior

### Comparison with Hold/Repeat System

| Feature | Rotary Encoders | Keyboard/IR |
|---------|----------------|-------------|
| Event type | EV_REL | EV_KEY |
| Control model | Step-based | Hold-based |
| Velocity | Time between steps | Acceleration while held |
| Release behavior | N/A | Decay to zero |
| Action type | `VolumeStep` | `VolumeHeld`/`VolumeRelease` |
| Code path | `handleRelEvent()` | `handleKeyEvent()` |

These are completely independent systems that don't interfere with each other.

## Identifying Your Encoder

Use `evtest` to see what events your encoder sends:

```bash
# List all input devices
sudo evtest

# Test a specific device
sudo evtest /dev/input/event7
```

Then turn the encoder and look for output:

### EV_REL Encoder (use `type: rotary`)
```
Event: time 1234.567890, type 2 (EV_REL), code 7 (REL_DIAL), value 1
Event: time 1234.567890, -------------- SYN_REPORT ------------
Event: time 1234.678901, type 2 (EV_REL), code 7 (REL_DIAL), value -1
```

### EV_KEY Encoder (use `type: key`)
```
Event: time 1234.567890, type 1 (EV_KEY), code 115 (KEY_VOLUMEUP), value 1
Event: time 1234.567890, -------------- SYN_REPORT ------------
Event: time 1234.567900, type 1 (EV_KEY), code 115 (KEY_VOLUMEUP), value 0
```

## Tuning Guide

### Fine Control (Mixing, Critical Listening)
```yaml
rotary:
  db_per_step: 0.25              # Very precise
  velocity_multiplier: 1.5       # Modest boost when fast
  velocity_threshold: 4          # Harder to trigger
  velocity_window_ms: 200
```

### Balanced Control (General Use)
```yaml
rotary:
  db_per_step: 0.5               # Default
  velocity_multiplier: 2.0       # 2x when fast
  velocity_threshold: 3          # Moderate trigger
  velocity_window_ms: 200
```

### Coarse Control (Quick Adjustments)
```yaml
rotary:
  db_per_step: 1.0               # Large steps
  velocity_multiplier: 2.5       # Aggressive boost
  velocity_threshold: 2          # Easy to trigger
  velocity_window_ms: 250
```

### Disable Velocity Detection
```yaml
rotary:
  db_per_step: 0.5
  velocity_multiplier: 1.0       # No multiplier
  # threshold and window become irrelevant
```

## Common Devices

### Griffin PowerMate
- **Type**: EV_REL (REL_DIAL)
- **Features**: Push button, LED control
- **Recommendation**: `db_per_step: 0.5`, `velocity_multiplier: 2.0`

### Contour Design ShuttleXpress
- **Type**: EV_REL (REL_DIAL for jog wheel)
- **Features**: Shuttle ring (different codes), buttons
- **Recommendation**: `db_per_step: 0.5`, `velocity_multiplier: 2.0`

### Custom Arduino/Raspberry Pi Encoders
- **Type**: Usually EV_REL (configure in firmware)
- **Detents**: Varies (12-24 per rotation typical)
- **Recommendation**: Start with defaults, tune to taste

### USB "Volume Knob" Devices
- **Type**: Varies - check with `evtest`
- **Note**: Some send EV_KEY (use `type: key`), some send EV_REL (use `type: rotary`)

## Troubleshooting

### Encoder Does Nothing

**Symptoms**: Turning the encoder has no effect on volume.

**Checks**:
1. Verify device type is `rotary` for EV_REL encoders:
   ```bash
   sudo evtest /dev/input/event7
   ```
2. Check device sends REL_DIAL, REL_WHEEL, or REL_MISC events
3. Enable debug logging to see if events are received:
   ```yaml
   logging:
     level: debug
   ```
4. Verify permissions:
   ```bash
   ls -la /dev/input/event7
   # Should show group 'input' with read permission
   groups $USER
   # Should include 'input' group
   ```

### Volume Changes Too Much

**Symptoms**: Each encoder click changes volume too much.

**Solutions**:
- Reduce `db_per_step`: try `0.25` or `0.3`
- Reduce or disable velocity: set `velocity_multiplier: 1.0`
- Increase velocity threshold: try `velocity_threshold: 5`

### Volume Changes Too Little

**Symptoms**: Need many clicks to make noticeable volume change.

**Solutions**:
- Increase `db_per_step`: try `0.75` or `1.0`
- Increase velocity multiplier: try `velocity_multiplier: 2.5` or `3.0`
- Decrease velocity threshold: try `velocity_threshold: 2`

### Velocity Mode Not Triggering

**Symptoms**: Fast spinning doesn't increase step size.

**Checks**:
1. Enable debug logging and watch for "rotary velocity mode" messages
2. Check your actual spinning speed - threshold might be too high
3. Increase window: `velocity_window_ms: 300` or `400`
4. Decrease threshold: `velocity_threshold: 2`

Example debug output when velocity triggers:
```
[DEBUG] rotary velocity mode steps_in_window=4 multiplier=2.0 db_per_step=1.0
[DEBUG] volume step applied steps=1 delta_db=1.0 new_volume=-28.5
```

### Encoder Feels Inconsistent

**Symptoms**: Sometimes large jumps, sometimes small, feels "lumpy".

**Causes**: Some encoders send multiple events per physical detent or have inconsistent timing.

**Solutions**:
- Increase `velocity_window_ms` to smooth out bursts: try `300` or `400`
- Experiment with `db_per_step` - sometimes larger steps feel more consistent
- Check encoder quality - cheap encoders can have poor detent mechanics

### Wrong Direction

**Symptoms**: Clockwise decreases volume instead of increasing (or vice versa).

**Cause**: Hardware-specific (encoder wiring, driver implementation).

**Solutions**:
- **Hardware**: Rewire the encoder (swap A/B phases)
- **Firmware**: Reverse polarity in device firmware/driver
- **Workaround**: Adapt muscle memory (not ideal but sometimes necessary)

Note: There's currently no software configuration to reverse direction. This is intentional to avoid confusion - the fix should be at the hardware/driver level.

## Technical Details

### Architecture

```
Input Device (rotary encoder)
    ↓
readInputEvents() - reads EV_REL events
    ↓
handleRelEvent() - translates to VolumeStep action
    ↓  (uses rotaryState for velocity tracking)
Action Channel
    ↓
handleAction() - processes VolumeStep
    ↓  (bypasses velocity engine)
CamillaDSP Client - applies volume change immediately
```

### Code Flow

1. **Input Layer** (`handleRelEvent` in `main.go`):
   - Filters for REL_DIAL/REL_WHEEL/REL_MISC codes
   - Tracks recent steps via `rotaryState.addStep()`
   - Calculates step size with optional velocity multiplier
   - Creates `VolumeStep` action

2. **Action Processing** (`handleAction` in `daemon.go`):
   - Receives `VolumeStep` action
   - Calculates volume delta: `steps × db_per_step`
   - Queries current volume if unknown
   - Applies delta with clamping to min/max bounds
   - Updates CamillaDSP immediately (no velocity engine)

3. **Velocity Tracking** (`rotaryState` in `rotary.go`):
   - Thread-safe step tracking
   - Sliding time window (expires old steps)
   - Counts steps in same direction
   - Returns count for velocity decision

### Action Types

```go
// Rotary encoder action (discrete steps)
type VolumeStep struct {
    Steps     int     // +/- number of steps
    DbPerStep float64 // dB change per step (optional override)
}

// Keyboard/IR action (continuous hold)
type VolumeHeld struct {
    Direction int // -1, 0, or 1
}

type VolumeRelease struct{}
```

These are processed by different code paths and never interfere with each other.

### Thread Safety

- `rotaryState` uses mutex for concurrent access (multiple encoders)
- Action channel serializes all volume changes
- Daemon goroutine is the single owner of velocity state
- No race conditions between rotary and hold-based volume control

## Testing

The implementation includes comprehensive tests:

### Rotary State Tests (`rotary_test.go`)
- Basic step tracking
- Direction changes
- Window expiry (partial and full)
- Concurrent access (thread safety)
- Edge cases (zero window, negative direction)

### Daemon Integration Tests (`daemon_test.go`)
- VolumeStep action processing
- Clamping to min/max bounds
- Default step size handling
- Multiple sequential steps
- Integration with VolumeHeld (no interference)

Run tests:
```bash
go test -v ./cmd/streamerbrainz -run TestRotary
go test -v ./cmd/streamerbrainz -run TestHandleAction_VolumeStep
```

## Migration from Old Config

If you have an existing config using the old `devices` array:

### Before (still works, defaults to `type: key`)
```yaml
ir:
  devices:
    - /dev/input/event6
```

### After (explicit device types)
```yaml
ir:
  input_devices:
    - path: /dev/input/event6
      type: key
    - path: /dev/input/event7
      type: rotary

rotary:
  db_per_step: 0.5
  velocity_multiplier: 2.0
  velocity_threshold: 3
```

The old format is automatically migrated on startup, but using `input_devices` is recommended for clarity.

## Examples

See [`examples/config-rotary.yaml`](../examples/config-rotary.yaml) for a complete annotated configuration file with:
- Mixed device setup (IR + rotary)
- Tuning presets for different use cases
- Troubleshooting guide
- Device identification tips

## See Also

- [Configuration Reference](./configuration.md)
- [Multi-Device Input Setup](../examples/config-multi-device.yaml)
- [Velocity Engine Documentation](./velocity-engine.md)