# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- **Rotary Encoder Support**: Full support for rotary encoders with step-based volume control
  - New device type system: `key` (keyboards/IR) vs `rotary` (encoders)
  - EV_REL event handling (REL_DIAL, REL_WHEEL, REL_MISC)
  - Intelligent velocity detection for "fast spinning" behavior
  - Configurable step size, velocity window, multiplier, and threshold
  - Complete separation from keyboard/IR velocity engine (no interference)
  - Thread-safe rotary state tracking for concurrent input devices
- **New Action Type**: `VolumeStep` for discrete encoder steps (bypasses hold/repeat system)
- **Device Configuration**: New `input_devices` format with explicit device types
  - Backward compatible: old `devices` array still works (defaults to `type: key`)
  - Auto-migration in config validation
- **Comprehensive Testing**: 29 new tests covering rotary state and action handling
  - Unit tests for velocity detection, window expiry, thread safety
  - Integration tests for VolumeStep action processing, clamping, defaults
- **Documentation**: 
  - User guide: `docs/rotary-encoders.md` with troubleshooting and tuning
  - Example config: `examples/config-rotary.yaml` with detailed annotations
  - Implementation summary: `docs/ROTARY_ENCODER_IMPLEMENTATION.md`
- **Interface Extraction**: `CamillaDSPClientInterface` for improved testability

### Changed
- Main event loop now routes events by type (EV_KEY vs EV_REL)
- Device opening tracks device type alongside file handle
- `handleAction()` and `applyVolume()` now use interface instead of concrete client
- `Close()` method now returns error for interface consistency

### Technical Details

#### Rotary Encoder Architecture
- **Separate Code Path**: Rotary encoders bypass the velocity/hold system entirely
- **Step-Based Control**: Each detent is a discrete `VolumeStep` action
- **Velocity Detection**: Sliding time window tracks recent steps to detect fast spinning
  - Slow turn: 1 step/500ms → normal step size (e.g., 0.5 dB)
  - Fast spin: 5 steps/200ms → velocity mode (e.g., 1.0 dB with 2x multiplier)
- **Thread Safety**: `rotaryState` uses mutex for concurrent access from multiple devices
- **No Interference**: Keyboard/IR hold-based control continues working unchanged

#### Configuration Example
```yaml
ir:
  input_devices:
    - path: /dev/input/event6
      type: key      # IR remote
    - path: /dev/input/event7
      type: rotary   # Rotary encoder

rotary:
  db_per_step: 0.5
  velocity_window_ms: 200
  velocity_multiplier: 2.0
  velocity_threshold: 3
```

## [1.0.0] - 2024-12-22

### Added
- Comprehensive help system with `-help` and `-version` flags
- Detailed usage examples and parameter documentation in help output
- Separate help for `librespot-hook` subcommand
- Verbose logging mode (`-v`) with configuration printout on startup
- Makefile for building all binaries to `./bin` directory
- Build targets: `make`, `make clean`, `make install`, `make help`
- CLI reference documentation (`docs/CLI-REFERENCE.md`)
- Updated `.gitignore` to ignore `./bin` directory
- Version constant and version display functionality

### Fixed
- **Removed excessive verbose logging**: Velocity state logging has been completely removed
  - Velocity updates run at 30Hz but no longer produce any log output
  - Eliminates log spam that was flooding output with `[VEL]` messages
  - Verbose mode (`-v`) now only logs meaningful events, not internal state updates

### Changed
- All binaries now build to `./bin/` directory instead of project root
- Velocity state updates are now silent (no logging even in verbose mode)
- Improved documentation throughout codebase with detailed comments
- Updated README with new build system and help documentation

### Technical Details

#### Verbose Logging Fix
The daemon runs at 30 Hz (configurable with `-update-hz`), which caused verbose mode to log 30 times per second when velocity logging was enabled. This flooded logs with internal state information that wasn't useful for troubleshooting.

**Solution implemented:**
- Removed all logging from `velocity.go` update loop
- Removed unused `verbose` parameter from `velocityState.update()` method
- Velocity physics continue to work exactly as before, just silently

This keeps the logs clean while preserving all volume control functionality.

### Documentation
- Enhanced README with build instructions and help system documentation
- Created comprehensive CLI reference guide
- Added inline code documentation for velocity thresholds
- Updated troubleshooting sections

---

## [0.9.0] - Previous releases

### Features
- IR remote control integration via Linux input devices
- Velocity-based volume control with physics simulation
- WebSocket communication with CamillaDSP
- IPC server for multi-source control
- Librespot integration for Spotify Connect
- Automatic WebSocket reconnection
- Volume safety limits and safety zone
- Mute toggle functionality

---

## Future Releases

### Planned Features
- [ ] Configuration file support
- [ ] Multiple profile/preset switching
- [ ] Advanced fade controls
- [ ] Source priority management
- [ ] systemd service template
- [ ] Improved error recovery
- [ ] Volume curve customization

---

## Version History

- **v1.0.0** - First stable release with comprehensive help and logging fixes
- **v0.9.0** - Pre-release with core functionality