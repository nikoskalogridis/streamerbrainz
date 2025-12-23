# Plex Integration (Webhooks)

This guide explains how to integrate StreamerBrainz with Plex Media Server using Plex webhooks.

It is primarily intended for Plexamp users, but the integration targets **a specific Plex player** by `machineIdentifier`.

This document covers user setup/configuration/troubleshooting using the YAML configuration file. Implementation details live in the architecture docs.

---

## What this integration supports

### Current (implemented)
- Receives Plex webhooks from Plex Media Server
- Retrieves the selected player’s **playback state** and **track metadata** by querying Plex `/status/sessions`
- Logs state/metadata events in the StreamerBrainz daemon logs



---

## Requirements

- Plex Media Server reachable from the machine running StreamerBrainz
- StreamerBrainz configured (at `~/.config/streamerbrainz/config.yaml`)
- A Plex token (stored in a file)
- The target player `machineIdentifier`
- Plex webhook configured to point to StreamerBrainz

---

## Configuration

StreamerBrainz is configured via YAML at `~/.config/streamerbrainz/config.yaml`.

### Webhooks section

```yaml
webhooks:
  # HTTP listener port for webhooks (Plex, etc.)
  port: 3001
```

### Plex section

```yaml
plex:
  # Enable Plex integration (webhooks + session polling)
  enabled: true

  # Plex Media Server base URL (e.g. http://plex.home.arpa:32400)
  server_url: http://plex.home.arpa:32400

  # Path to file containing Plex token (treat like a password)
  token_file: ~/.config/streamerbrainz/plex-token

  # Player machineIdentifier to target/filter sessions
  machine_id: YOUR_MACHINE_IDENTIFIER
```

### Configuration keys

**Webhooks:**
- **port**: HTTP webhooks listener port (default: `3001`)

**Plex:**
- **enabled**: Enable Plex integration (default: `false`)
- **server_url**: Plex server URL (e.g., `http://plex.home.arpa:32400`)
- **token_file**: Path to file containing Plex authentication token (supports `~` expansion)
- **machine_id**: Player `machineIdentifier` to select the target player

---

## Setup

### 1) Get your Plex token
Follow Plex’s guide:
https://support.plex.tv/articles/204059436-finding-an-authentication-token-x-plex-token/

Store the token in a file (example):
```bash
mkdir -p ~/.config/streamerbrainz
echo -n "YOUR_PLEX_TOKEN" > ~/.config/streamerbrainz/plex-token
chmod 600 ~/.config/streamerbrainz/plex-token
```



### 2) Find the player machine identifier
Start playing something in Plexamp (or your target Plex player), then run:

```bash
./scripts/get-plex-machine-id.sh plex.home.arpa:32400 YOUR_PLEX_TOKEN
```

Copy the `machineIdentifier` for the player you want StreamerBrainz to track/control.

### 3) Configure Plex integration

Edit `~/.config/streamerbrainz/config.yaml` and configure the Plex section:

```yaml
webhooks:
  port: 3001

plex:
  enabled: true
  server_url: http://plex.home.arpa:32400
  token_file: ~/.config/streamerbrainz/plex-token
  machine_id: YOUR_MACHINE_IDENTIFIER
```

Replace `YOUR_MACHINE_IDENTIFIER` with the value from step 2.

### 4) Start the daemon

```bash
streamerbrainz
```

Or if you want to see detailed Plex activity, enable debug logging:

```bash
streamerbrainz -log-level debug
```

### 5) Configure Plex webhooks
In Plex Web:
1. Settings → Webhooks
2. Add:
   ```
   http://YOUR_STREAMERBRAINZ_HOST:3001/webhooks/plex
   ```

---

## Verification

Trigger an event (play/pause/skip) on the target player and watch StreamerBrainz logs.

---

## Troubleshooting

### Webhook not received
- Confirm Plex can reach the StreamerBrainz host/port (routing/firewall).
- Confirm the webhook URL is configured in Plex Settings → Webhooks.
- Confirm `webhooks.port` in your config matches the port in the webhook URL.

### No sessions found / wrong player
- Ensure the player is actively playing something.
- Re-check `plex.machine_id` in your config using:
  ```bash
  ./scripts/get-plex-machine-id.sh plex.home.arpa:32400 YOUR_PLEX_TOKEN
  ```

### Token problems
- Verify `plex.token_file` in your config points to the correct file and is readable.
- If the token was revoked, generate a new one.

---

## Security notes

- Treat your Plex token like a password.
- The webhook endpoint is unauthenticated by default; restrict network access appropriately.

---

## References

- [Plex Webhooks](https://support.plex.tv/articles/115002267687-webhooks/)
- [Finding Your Plex Token](https://support.plex.tv/articles/204059436-finding-an-authentication-token-x-plex-token/)
- [Configuration example](../examples/config.yaml)