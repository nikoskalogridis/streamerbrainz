# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] - 2024-12-22

### Added
- Comprehensive help system with `-help` and `-version` flags
- Detailed usage examples and parameter documentation in help output
- Separate help for `librespot-hook` subcommand
- Verbose logging mode (`-v`) with configuration printout on startup
- Makefile for building all binaries to `./builds` directory
- Build targets: `make`, `make clean`, `make install`, `make help`
- CLI reference documentation (`docs/CLI-REFERENCE.md`)
- Updated `.gitignore` to ignore `./builds` directory
- Version constant and version display functionality

### Fixed
- **Excessive verbose logging spam**: Velocity state was logging every update tick (30Hz) even when idle
  - Added `minVelocityThreshold` constant (0.01 dB/s) to snap negligible velocities to zero
  - Modified logging to only show when velocity exceeds threshold or button is held
  - Prevents flooding logs with `[VEL] held=0 vel=-0.00 dB/s` messages
- Improved `shouldSendUpdate()` threshold from 0.1 dB to 0.05 dB for more precise control
- Added velocity decay snap-to-zero to prevent infinite tiny updates

### Changed
- All binaries now build to `./builds/` directory instead of project root
- Velocity logging is now conditional on meaningful activity (velocity > 0.01 dB/s)
- Improved documentation throughout codebase with detailed comments
- Updated README with new build system and help documentation

### Technical Details

#### Verbose Logging Fix
The daemon runs at 30 Hz (configurable with `-update-hz`), which caused verbose mode to log 30 times per second even when no volume changes were occurring. The issue was that velocity decay produced very small floating-point values (e.g., -0.00001) which were technically non-zero.

**Solution implemented:**
1. Added `minVelocityThreshold = 0.01` constant in `constants.go`
2. Snap velocity to zero when `|velocity| < minVelocityThreshold` during decay
3. Only log when `held != 0` OR `|velocity| >= minVelocityThreshold`

This eliminates log spam while preserving all meaningful velocity state information.

#### Constants Added
```go
const (
    minVelocityThreshold = 0.01 // Snap velocity to zero below this (dB/s)
    minVolumeDiffDB      = 0.05 // Only send updates when volume differs by this (dB)
)
```

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
- `argon-ctl` CLI utility
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