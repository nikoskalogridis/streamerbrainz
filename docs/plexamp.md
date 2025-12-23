# Plex Integration (Webhooks)

This guide explains how to integrate StreamerBrainz with Plex Media Server using Plex webhooks.

It is primarily intended for Plexamp users, but the integration targets **a specific Plex player** by `machineIdentifier`.

This document covers user setup/configuration/troubleshooting. Implementation details live in the architecture docs.

---

## What this integration supports

### Current (implemented)
- Receives Plex webhooks from Plex Media Server
- Retrieves the selected player’s **playback state** and **track metadata** by querying Plex `/status/sessions`
- Logs state/metadata events in the StreamerBrainz daemon logs



---

## Requirements

- Plex Media Server reachable from the machine running StreamerBrainz
- A Plex token (stored in a file)
- The target player `machineIdentifier`
- Plex webhook configured to point to StreamerBrainz

---

## Configuration

| Flag | Default | Description |
|------|---------|-------------|
| `-webhooks-port` | `3001` | HTTP webhooks listener port |
| `-plex-server-url` | `""` | Plex server URL (e.g., `http://plex.home.arpa:32400`) |
| `-plex-token-file` | `""` | Path to file containing Plex authentication token |
| `-plex-machine-id` | `""` | Player `machineIdentifier` to select the target player |

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

### (Optional) Secure token storage with systemd encrypted credentials

If you run StreamerBrainz as a systemd `--user` service, you can store the Plex token encrypted at rest using systemd credentials.

1) Create an encrypted credential:
```bash
mkdir -p ~/.config/streamerbrainz
echo -n "YOUR_PLEX_TOKEN" | systemd-creds encrypt --name=plex-token - - > ~/.config/streamerbrainz/plex-token.cred
chmod 600 ~/.config/streamerbrainz/plex-token.cred
```

2) Install the example user service:
```bash
mkdir -p ~/.config/systemd/user
cp examples/streamerbrainz.service ~/.config/systemd/user/streamerbrainz.service
```

3) Ensure the service file loads the credential and passes it via `-plex-token-file` (it uses `%d/plex-token`).

4) Enable and start:
```bash
systemctl --user daemon-reload
systemctl --user enable --now streamerbrainz
```

The example service also includes the recommended `journalctl` command for monitoring logs.

### 2) Find the player machine identifier
Start playing something in Plexamp (or your target Plex player), then run:

```bash
./scripts/get-plex-machine-id.sh plex.home.arpa:32400 YOUR_PLEX_TOKEN
```

Copy the `machineIdentifier` for the player you want StreamerBrainz to track/control.

### 3) Start the daemon with Plex enabled
```bash
streamerbrainz \
  -plex-server-url http://plex.home.arpa:32400 \
  -plex-token-file ~/.config/streamerbrainz/plex-token \
  -plex-machine-id YOUR_MACHINE_IDENTIFIER \
  -webhooks-port 3001 \
  -log-level info
```

### 4) Configure Plex webhooks
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
- Confirm `-webhooks-port` matches the port in the webhook URL.

### No sessions found / wrong player
- Ensure the player is actively playing something.
- Re-check `-plex-machine-id` using:
  ```bash
  ./scripts/get-plex-machine-id.sh plex.home.arpa:32400 YOUR_PLEX_TOKEN
  ```

### Token problems
- Verify `-plex-token-file` points to the correct file and is readable.
- If the token was revoked, generate a new one.

---

## Security notes

- Treat your Plex token like a password.
- The webhook endpoint is unauthenticated by default; restrict network access appropriately.

---

## References

- [Plex Webhooks](https://support.plex.tv/articles/115002267687-webhooks/)
- [Finding Your Plex Token](https://support.plex.tv/articles/204059436-finding-an-authentication-token-x-plex-token/)