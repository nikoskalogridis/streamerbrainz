# Plexamp Integration Implementation Summary

This document summarizes the implementation of Plexamp/Plex Media Server webhook integration into StreamerBrainz.

---

## Overview

The Plexamp integration adds webhook support directly to the main daemon, allowing it to receive playback events from Plex Media Server and track what's playing on specific Plexamp players.

**Architecture:**
- HTTP webhook server runs as a goroutine inside the main daemon
- No IPC needed - actions sent directly to the action channel
- Token security via systemd encrypted credentials
- Filters sessions by player machine identifier

---

## Files Created/Modified

### New Files

1. **`plexamp.go`** - Core Plexamp integration module
   - HTTP webhook handler
   - Plex API client (`/status/sessions`)
   - XML response parser
   - Session filtering by machine identifier
   - Action generation

2. **`examples/get-plex-machine-id.sh`** - Helper script
   - Discovers active Plex players
   - Shows machine identifiers
   - Displays current playback info

3. **`examples/streamerbrainz-plex.service`** - Systemd user service
   - Uses `LoadCredentialEncrypted` for token security
   - Configured for user service (`~/.config/systemd/user/`)
   - PrivateMounts for credential isolation

4. **`examples/SETUP_CREDENTIALS.md`** - Credential setup guide
   - Complete guide for systemd encrypted credentials
   - Troubleshooting section
   - Security best practices

5. **`examples/PLEXAMP_QUICKSTART.md`** - Quick start guide
   - 5-minute setup process
   - Command-line examples
   - Testing procedures

6. **`docs/plexamp-integration.md`** - Detailed documentation
   - Architecture diagrams
   - API response examples
   - Configuration reference
   - Troubleshooting guide

### Modified Files

1. **`actions.go`**
   - Added `PlexStateChanged` action type
   - Added marshaling/unmarshaling support

2. **`daemon.go`**
   - Added `PlexStateChanged` case to `handleAction()`
   - Currently logs the event (placeholder for future features)

3. **`main.go`**
   - Refactored flags: `-plex-server-url`, `-plex-token-file`, `-plex-machine-id`, `-webhooks-port`
   - Removed `-plex-enabled` flag - Plex integration enabled automatically when params provided
   - Webhooks HTTP server always runs
   - Added token file reading support (for systemd credentials)
   - Added validation logic
   - Updated help text

4. **`README.md`**
   - Updated feature list
   - Added Plexamp/Plex webhook section
   - Added setup instructions
   - Added file structure reference

---

## API Flow

```
1. Plex sends webhook → http://daemon:3001/webhooks/plex
2. Daemon receives webhook trigger
3. Daemon queries Plex API → http://plex:32400/status/sessions?X-Plex-Token=XXX
4. Daemon parses XML response
5. Daemon filters by machineIdentifier
6. Daemon creates PlexStateChanged action
7. Action sent to daemon brain → handleAction()
8. Currently: logs the event
```

---

## Action Structure

```go
type PlexStateChanged struct {
    State         string  // "playing", "paused", "stopped"
    Title         string  // Track title
    Artist        string  // Artist name (grandparentTitle)
    Album         string  // Album name (parentTitle)
    DurationMs    int64   // Track duration in milliseconds
    PositionMs    int64   // Current position in milliseconds
    SessionKey    string  // Plex session key
    RatingKey     string  // Plex rating key
    PlayerTitle   string  // Player name
    PlayerProduct string  // "Plexamp", etc.
}
```

---

## Configuration Options

### Command-Line Flags

| Flag | Default | Required | Description |
|------|---------|----------|-------------|
| `-webhooks-port` | `3001` | No | HTTP webhooks listener port |
| `-plex-server-url` | `""` | Yes (for Plex) | Plex server URL (e.g., http://plex.home.arpa:32400) |
| `-plex-token-file` | `""` | Yes (for Plex) | Path to file containing Plex token |
| `-plex-machine-id` | `""` | Yes (for Plex) | Player machine identifier |

Note: Plex integration is automatically enabled when all three Plex parameters are provided.
The webhooks HTTP server always runs, regardless of Plex integration status.

### Systemd Service Configuration

```ini
# Encrypted credential storage
LoadCredentialEncrypted=plex-token:%h/.config/streamerbrainz/plex-token.cred

# Pass credential path to daemon
ExecStart=%h/.local/bin/streamerbrainz \
    -plex-server-url http://plex.home.arpa:32400 \
    -plex-token-file=%d/plex-token \
    -plex-machine-id YOUR_MACHINE_ID \
    ...
```

---

## Security Features

### 1. Systemd Encrypted Credentials
- Token encrypted at rest using `systemd-creds encrypt`
- Automatic decryption at service startup
- Stored in user's home directory (`~/.config/`)

### 2. Private Mounts
- `PrivateMounts=yes` isolates credential directory
- Other services can't access the credential

### 3. File-Based Token Loading
- Supports `-plex-token-file` for reading from systemd credentials
- Token trimmed and validated on load
- Only token files are supported (no command-line token option)

### 4. No Token in Service Files
- Service file references credential path, not actual token
- No plaintext tokens in config

---

## Setup Process

### Quick Setup (5 minutes)

1. Get Plex token from Plex Web App
2. Find machine ID: `./examples/get-plex-machine-id.sh plex.home.arpa:32400 TOKEN`
3. Encrypt token: `systemd-creds encrypt --name=plex-token - - > ~/.config/streamerbrainz/plex-token.cred`
4. Install service: `cp examples/streamerbrainz-plex.service ~/.config/systemd/user/`
5. Start: `systemctl --user enable --now streamerbrainz-plex`

### Production Setup

See `examples/SETUP_CREDENTIALS.md` for complete instructions.

---

## Testing

### Manual Testing

```bash
# Start daemon with Plex enabled
streamerbrainz \
  -plex-server-url http://plex.home.arpa:32400 \
  -plex-token-file /path/to/plex-token \
  -plex-machine-id YOUR_MACHINE_ID \
  -log-level debug

# Play music in Plexamp
# Watch logs for:
# [INFO] Plex session found title="..." artist="..."
# [INFO] Plex state changed state=playing ...
```

### Helper Script

```bash
# Discover active players and their machine IDs
./examples/get-plex-machine-id.sh plex.home.arpa:32400 YOUR_TOKEN
```

---

## Current Behavior

**When a webhook is received:**
1. Queries Plex API for active sessions
2. Filters by machine identifier
3. Logs track info and playback state
4. No CamillaDSP actions triggered (yet)

**Log output example:**
```
[INFO] Plex session found title="Once in a Lifetime" artist="Talking Heads" album="The Best Of" state=playing position_ms=30496 duration_ms=259560
[INFO] Plex state changed state=playing title="Once in a Lifetime" artist="Talking Heads" album="The Best Of"
```

---

## Future Enhancements

Potential features that could be added:

1. **Automatic fade-out on pause**
   - Detect pause event → trigger volume ramp down

2. **Source priority**
   - Prefer Plexamp over Spotify when both active
   - Automatic source switching

3. **Volume normalization**
   - Parse ReplayGain from Plex metadata
   - Adjust volume based on track loudness

4. **Playback state tracking**
   - Remember last played track/position
   - Display on web UI

5. **Webhook authentication**
   - Validate webhook signature
   - Prevent unauthorized requests

6. **Multi-player support**
   - Track multiple Plexamp instances
   - Room-based control

---

## Dependencies

- **Go standard library** - HTTP server, XML parsing
- **No external dependencies added** - Uses only stdlib

---

## Design Decisions

### Why Not a Subcommand?

Initially considered `plexamp-webhook` subcommand (like `librespot-hook`), but:
- ❌ Would require IPC communication
- ❌ Extra process to manage
- ❌ More complex deployment

Integrated into main daemon instead:
- ✅ Direct action channel access
- ✅ Single process
- ✅ Simpler configuration
- ✅ Better resource usage

### Why systemd Encrypted Credentials?

Alternatives considered:
- Environment variables: Not persistent, visible in `ps`
- Config files: Easy to accidentally commit
- HashiCorp Vault: Too heavy for this use case

Systemd credentials:
- ✅ Native to systemd
- ✅ Encrypted at rest
- ✅ No extra dependencies
- ✅ User-scoped security

### Token Security

- Only `-plex-token-file` is supported (no command-line token option)
- Use systemd encrypted credentials for production deployments
- Store tokens in files with restricted permissions (chmod 600)
- Mutual exclusivity enforced

---

## Files Overview

```
streamerbrainz/
├── plexamp.go                                 # NEW: Core integration
├── actions.go                                 # MODIFIED: Added PlexStateChanged
├── daemon.go                                  # MODIFIED: Handle PlexStateChanged
├── main.go                                    # MODIFIED: Flags + webhook startup
├── README.md                                  # MODIFIED: Documentation
├── docs/
│   └── plexamp-integration.md                # NEW: Detailed docs
└── examples/
    ├── get-plex-machine-id.sh                # NEW: Discovery helper
    ├── streamerbrainz-plex.service          # NEW: Systemd service
    ├── SETUP_CREDENTIALS.md                  # NEW: Credential setup guide
    └── PLEXAMP_QUICKSTART.md                 # NEW: Quick start guide
```

---

## Example Plex API Response

```xml
<?xml version="1.0" encoding="UTF-8"?>
<MediaContainer size="1">
  <Track 
    title="Once in a Lifetime"
    grandparentTitle="Talking Heads"
    parentTitle="Once in a Lifetime: The Best Of"
    duration="259560"
    viewOffset="30496"
    sessionKey="53"
    ratingKey="6260">
    <Player 
      machineIdentifier="bc97b983-8169-47e3-bbcc-54a12d662546"
      state="playing"
      title="Office"
      product="Plexamp"
      platform="Linux" />
  </Track>
</MediaContainer>
```

---

## Command-Line Examples

```bash
# Basic Plex integration
streamerbrainz \
  -plex-server-url http://plex.home.arpa:32400 \
  -plex-token-file /path/to/plex-token \
  -plex-machine-id bc97b983-8169-47e3-bbcc-54a12d662546

# With systemd credentials
streamerbrainz \
  -plex-server-url http://plex.home.arpa:32400 \
  -plex-token-file /run/credentials/plex-token \
  -plex-machine-id bc97b983-8169-47e3-bbcc-54a12d662546

# Custom webhook port
streamerbrainz \
  -webhooks-port 9000 \
  -plex-server-url http://192.168.1.100:32400 \
  -plex-token-file /path/to/token \
  -plex-machine-id YOUR_ID
```

---

## References

- [Plex Webhooks](https://support.plex.tv/articles/115002267687-webhooks/)
- [systemd Credentials](https://systemd.io/CREDENTIALS/)
- [systemd.exec - LoadCredential](https://www.freedesktop.org/software/systemd/man/systemd.exec.html#LoadCredential=)

---

**Status:** ✅ Complete and tested
**Version:** 1.0.0
**Date:** 2024