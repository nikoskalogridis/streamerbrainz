# Plexamp/Plex Integration Guide

This guide explains how to integrate StreamerBrainz with Plex Media Server and Plexamp to track playback events and potentially trigger actions based on playback state.

---

## Overview

The Plexamp integration adds webhook support to the StreamerBrainz daemon. When enabled, it:

1. Runs an HTTP server that receives webhooks from Plex Media Server
2. Queries the Plex API `/status/sessions` endpoint for detailed session information
3. Filters sessions by `machineIdentifier` to find the specific player
4. Emits `PlexStateChanged` actions with track metadata to the daemon

Currently, the integration logs playback events. Future versions may trigger actions like:
- Fade-out on pause
- Volume adjustment based on playback state
- Source switching between Spotify (librespot) and Plexamp

---

## Architecture

```
┌─────────────────┐      Webhook           ┌──────────────────────┐
│  Plex Media     │─────────────────────────▶│  StreamerBrainz     │
│  Server         │  (on playback events)    │  daemon             │
└─────────────────┘                          │                      │
                                             │  ┌────────────────┐  │
                                             │  │ HTTP Webhook   │  │
                                             │  │ Handler        │  │
                                             │  └────────┬───────┘  │
                                             │           │          │
                                             │           ▼          │
┌─────────────────┐      API Query          │  ┌────────────────┐  │
│  Plex API       │◀─────────────────────────│  │ Plex Session   │  │
│  /status/       │  (get session details)   │  │ Fetcher        │  │
│  sessions       │──────────────────────────▶│  └────────┬───────┘  │
└─────────────────┘      XML Response        │           │          │
                                             │           ▼          │
                                             │  ┌────────────────┐  │
                                             │  │ XML Parser +   │  │
                                             │  │ Filter by      │  │
                                             │  │ Machine ID     │  │
                                             │  └────────┬───────┘  │
                                             │           │          │
                                             │           ▼          │
                                             │  ┌────────────────┐  │
                                             │  │ Action:        │  │
                                             │  │ PlexState      │  │
                                             │  │ Changed        │  │
                                             │  └────────┬───────┘  │
                                             │           │          │
                                             │           ▼          │
                                             │  ┌────────────────┐  │
                                             │  │ Daemon Brain   │  │
                                             │  │ (handleAction) │  │
                                             │  └────────────────┘  │
                                             └──────────────────────┘
```

---

## Setup Instructions

### Step 1: Get Your Plex Token

Your Plex token is required to authenticate API requests.

**Option A: Official Documentation**
- Follow Plex's guide: https://support.plex.tv/articles/204059436-finding-an-authentication-token-x-plex-token/

**Option B: Quick Method (Web Browser)**
1. Log into your Plex Web App
2. Open any media item
3. Click the "..." menu and select "Get Info"
4. Click "View XML"
5. Look for `X-Plex-Token=` in the URL
6. Copy the token value

### Step 2: Find Your Player's Machine Identifier

The machine identifier uniquely identifies your Plexamp player instance.

**Option A: Use the Helper Script (Recommended)**

```bash
# Start playing something in Plexamp
# Then run:
./examples/get-plex-machine-id.sh plex.home.arpa:32400 YOUR_PLEX_TOKEN
```

This will display all active players with their machine identifiers.

**Option B: Manual API Query**

```bash
# Query the Plex API
curl "http://plex.home.arpa:32400/status/sessions?X-Plex-Token=YOUR_TOKEN"

# Look for the Player element with machineIdentifier attribute:
# <Player machineIdentifier="bc97b983-8169-47e3-bbcc-54a12d662546" ...>
```

### Step 3: Start the Daemon with Plexamp Integration

```bash
streamerbrainz \
  -plex-server-url http://plex.home.arpa:32400 \
  -plex-token-file /path/to/plex-token \
  -plex-machine-id bc97b983-8169-47e3-bbcc-54a12d662546 \
  -webhooks-port 3001 \
  -log-level debug
```

### Step 4: Configure Plex Webhooks

1. Open Plex Web App
2. Go to Settings (wrench icon) > Webhooks
3. Add a new webhook URL:
   ```
   http://your-server-ip:3001/webhooks/plex
   ```
4. Save the webhook

### Step 5: Test the Integration

Play, pause, or skip tracks in Plexamp and watch the daemon logs:

```bash
# You should see log entries like:
[INFO] Plex session found title="Once in a Lifetime" artist="Talking Heads" ...
[INFO] Plex state changed state=playing title="Once in a Lifetime" artist="Talking Heads" ...
```

---

## Configuration Options
## Configuration Reference

| Flag | Default | Description |
|------|---------|-------------|
| `-webhooks-port` | `3001` | HTTP webhooks listener port |
| `-plex-server-url` | `""` | Plex server URL (e.g., http://plex.home.arpa:32400) - enables Plex when set |
| `-plex-token-file` | `""` | Path to file containing Plex authentication token |
| `-plex-machine-id` | `""` | Player machine identifier to filter |

---

## Example Plex API Response

When a webhook is received, the daemon queries:

```
http://plex.home.arpa:32400/status/sessions?X-Plex-Token=XXX
```

Example response (simplified):

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

The daemon:
1. Parses the XML
2. Finds the `<Track>` with matching `Player.machineIdentifier`
3. Extracts metadata (title, artist, album, duration, position, state)
4. Emits a `PlexStateChanged` action

---

## PlexStateChanged Action

The action sent to the daemon contains:

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

Currently, the daemon logs this action. Future enhancements may include:
- Triggering fade-out on pause
- Volume normalization based on track metadata
- Source priority (prefer Plexamp over Spotify, etc.)

---

## Troubleshooting

### Webhook Not Received

**Check network connectivity:**
```bash
# From Plex server, test if daemon is reachable:
curl -X POST http://your-daemon-ip:3001/webhooks/plex

# Check if webhooks server is running:
curl http://your-daemon-ip:3001/
```

**Check firewall:**
- Ensure port 3001 (or your custom port) is open
- Check iptables/firewall rules

**Verify webhook configuration:**
- Plex Settings > Webhooks should show your URL
- Test the webhook from Plex web interface

### No Sessions Found

**Ensure music is playing:**
- Start playing a track in Plexamp
- Check that the player shows up in Plex Web App

**Verify machine identifier:**
```bash
# List all active players
./examples/get-plex-machine-id.sh plex.home.arpa:32400 YOUR_TOKEN
```

**Check Plex token:**
- Tokens can expire or be revoked
- Generate a new token if needed

### Wrong Player Detected

**Multiple Plexamp instances:**
- Each player has a unique machine identifier
- Make sure you're using the correct one
- Use the helper script to list all active players

---

## Security Considerations

### Plex Token Security

⚠️ **Your Plex token is sensitive** - it grants access to your Plex server.

- **Don't commit tokens to version control**
- Store in environment variables or secure configuration
- Consider using systemd credentials or secret management

### Webhook Endpoint Security

The webhook endpoint is unauthenticated by default.

**Mitigations:**
- Bind to localhost and use reverse proxy with auth
- Use firewall rules to restrict access
- Consider adding webhook secret validation (future enhancement)

---

## Running as a Service

### Systemd Service Example

See `examples/streamerbrainz-plex.service`:

```bash
# Install the service
sudo cp examples/streamerbrainz-plex.service \
  /etc/systemd/system/streamerbrainz.service

# Edit the service file with your tokens
sudo nano /etc/systemd/system/streamerbrainz.service

# Enable and start
sudo systemctl daemon-reload
sudo systemctl enable streamerbrainz
sudo systemctl start streamerbrainz

# Check status
sudo systemctl status streamerbrainz

# View logs
sudo journalctl -u streamerbrainz -f
```

---

## Future Enhancements

Potential features for future versions:

- **Automatic fade-out on pause** - Smoothly fade volume when pausing
- **Source priority** - Prefer Plexamp over Spotify when both are active
- **Playback state tracking** - Remember last played track/position
- **Volume normalization** - Adjust volume based on ReplayGain metadata
- **Webhook authentication** - Validate webhooks with shared secret
- **Multi-player support** - Track multiple Plexamp instances simultaneously

---

## References

- [Plex Webhooks Documentation](https://support.plex.tv/articles/115002267687-webhooks/)
- [Plex API Documentation (Unofficial)](https://github.com/Arcanemagus/plex-api/wiki)
- [Finding Your Plex Token](https://support.plex.tv/articles/204059436-finding-an-authentication-token-x-plex-token/)