# Plexamp Integration - Quick Start

Get Plexamp webhook integration running in 5 minutes.

---

## Prerequisites

- StreamerBrainz daemon installed
- Plex Media Server running
- Plexamp player active
- `curl` for testing (optional)

---

## 1. Get Your Plex Token

**Quick method:**
1. Open Plex Web App → Play any track
2. Click "..." → "Get Info" → "View XML"
3. Copy token from URL: `X-Plex-Token=YOUR_TOKEN_HERE`

**Full guide:** https://support.plex.tv/articles/204059436-finding-an-authentication-token-x-plex-token/

---

## 2. Find Your Machine Identifier

Start playing music in Plexamp, then run:

```bash
./examples/get-plex-machine-id.sh plex.home.arpa:32400 YOUR_PLEX_TOKEN
```

**Output example:**
```
Player #1
  Title:              Office
  Product:            Plexamp
  Platform:           Linux
  State:              playing
  Machine ID:         bc97b983-8169-47e3-bbcc-54a12d662546
  Now Playing:        Talking Heads - Once in a Lifetime
```

Copy the **Machine ID**.

---

## 3. Start the Daemon

```bash
sudo streamerbrainz \
  -plex-server-url http://plex.home.arpa:32400 \
  -plex-token-file /path/to/plex-token \
  -plex-machine-id bc97b983-8169-47e3-bbcc-54a12d662546 \
  -log-level debug
```

**Replace:**
- `http://plex.home.arpa:32400` with your Plex server URL
- `/path/to/plex-token` with path to your token file
- `bc97b983-...` with machine ID from step 2

---

## 4. Configure Plex Webhook

1. Open Plex Web App
2. Settings (⚙️) → Webhooks
3. Add webhook URL: `http://YOUR_DAEMON_IP:3001/webhooks/plex`
   - Example: `http://192.168.1.100:3001/webhooks/plex`
   - For localhost: `http://127.0.0.1:3001/webhooks/plex`
4. Save

---

## 5. (Optional) Use Encrypted Credentials

For production use, store your token securely with systemd encrypted credentials:

```bash
# Follow the detailed guide
cat examples/SETUP_CREDENTIALS.md

# Quick version:
mkdir -p ~/.config/streamerbrainz
echo -n "YOUR_PLEX_TOKEN" | \
  systemd-creds encrypt --name=plex-token - - > \
  ~/.config/streamerbrainz/plex-token.cred
chmod 600 ~/.config/streamerbrainz/plex-token.cred

# Or use a plain text file (less secure)
echo -n "YOUR_PLEX_TOKEN" > ~/.config/streamerbrainz/plex-token
chmod 600 ~/.config/streamerbrainz/plex-token

# Then use the service file which references the credential
cp examples/streamerbrainz-plex.service ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now streamerbrainz-plex
```

See `examples/SETUP_CREDENTIALS.md` for full instructions.

---

## 6. Test It

Play, pause, or skip a track in Plexamp.

**Check daemon logs:**
```
[INFO] Plex session found title="Song Title" artist="Artist Name" state=playing
[INFO] Plex state changed state=playing title="Song Title" artist="Artist Name"
```

**Manual webhook test:**
```bash
curl -X POST http://localhost:3001/webhooks/plex
```

---

## Troubleshooting

### "No sessions found"
- Make sure music is **actively playing** in Plexamp
- Verify Plex server address and token are correct

### "Failed to fetch Plex sessions"
- Check network connectivity to Plex server
- Verify Plex server is running
- Check firewall rules

### Webhook not triggering
- Verify webhook URL in Plex settings
- Check that port 3001 is accessible from Plex server
- Look for errors in daemon logs (`-log-level debug` flag)

### Wrong player detected
- Multiple Plexamp instances? Each has unique machine ID
- Re-run `get-plex-machine-id.sh` to find correct one

---

## Running as User Service

The example service file uses systemd encrypted credentials for security.

**Quick setup:**

```bash
# 1. Encrypt your Plex token
mkdir -p ~/.config/streamerbrainz
echo -n "YOUR_PLEX_TOKEN" | \
  systemd-creds encrypt --name=plex-token - - > \
  ~/.config/streamerbrainz/plex-token.cred
chmod 600 ~/.config/streamerbrainz/plex-token.cred

# 2. Install user service
mkdir -p ~/.config/systemd/user
cp examples/streamerbrainz-plex.service ~/.config/systemd/user/

# 3. Edit service file (update machine ID, etc.)
nano ~/.config/systemd/user/streamerbrainz-plex.service

# 4. Enable and start
systemctl --user daemon-reload
systemctl --user enable --now streamerbrainz-plex

# 5. Check status
systemctl --user status streamerbrainz-plex

# 6. View logs
journalctl --user -u streamerbrainz-plex -f
```

**Detailed guide:** See `examples/SETUP_CREDENTIALS.md`

---

## Command-Line Reference

```bash
# Full example with all common options
streamerbrainz \
  -ir-device /dev/input/event6 \
  -camilladsp-ws-url ws://127.0.0.1:1234 \
  -ipc-socket /tmp/streamerbrainz.sock \
  -camilladsp-min-db -65.0 \
  -camilladsp-max-db 0.0 \
  -webhooks-port 3001 \
  -plex-server-url http://plex.home.arpa:32400 \
  -plex-token-file /path/to/token/file \
  -plex-machine-id YOUR_MACHINE_ID \
  -log-level debug
```

**Plex-specific flags:**
- `-webhooks-port 3001` - HTTP webhooks listener port (default: 3001)
- `-plex-server-url URL` - Plex server URL (e.g., http://plex.home.arpa:32400) - enables Plex when set
- `-plex-token-file PATH` - Path to file with Plex token (required for Plex)
- `-plex-machine-id ID` - Player machine identifier (required for Plex)

---

## Security Notes

⚠️ **Protect your Plex token:**
- Don't commit to git
- **Use systemd encrypted credentials** (see `SETUP_CREDENTIALS.md`)
- Or use `-plex-token-file` with restricted permissions (chmod 600)
- Always store tokens in files, never on command line

⚠️ **Webhook endpoint:**
- Currently unauthenticated
- Use firewall rules to restrict access
- Consider reverse proxy with authentication

---

## What Happens Next?

Currently, the daemon **logs** Plex playback events:
- Track changes
- Play/pause state
- Artist, album, title metadata

**Future enhancements may include:**
- Automatic fade-out on pause
- Volume normalization
- Source priority (Plexamp vs Spotify)

See `docs/plexamp-integration.md` for detailed documentation.

---

## Need Help?

- Full docs: `docs/plexamp-integration.md`
- Credential setup: `examples/SETUP_CREDENTIALS.md`
- General help: `streamerbrainz -help`
- Machine ID helper: `./examples/get-plex-machine-id.sh -h`

**Common issues:**
- Token expired? Generate a new one
- Multiple players? List all with helper script
- Firewall blocking? Check port 3001 accessibility