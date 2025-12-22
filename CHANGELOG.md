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
- `sbctl` CLI utility
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