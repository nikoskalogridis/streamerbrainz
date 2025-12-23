# CamillaDSP integration

StreamerBrainz controls volume and mute by talking to **CamillaDSP over WebSocket**.

This page is user-facing: setup expectations, the relevant configuration keys, and common troubleshooting.

## Requirements

- CamillaDSP running with its **WebSocket server enabled** (CamillaDSP `-pPORT`).
- StreamerBrainz can reach the WebSocket URL (typically `ws://127.0.0.1:1234` when on the same host).

## What StreamerBrainz uses CamillaDSP for

- Read initial volume at startup (to synchronize internal state).
- Set volume (dB) during operation (velocity-based updates).
- Toggle mute immediately when requested.

## Configuration

StreamerBrainz is configured via YAML at `~/.config/streamerbrainz/config.yaml`.

### CamillaDSP section

```yaml
camilladsp:
  # CamillaDSP WebSocket endpoint (CamillaDSP must be started with -pPORT)
  ws_url: ws://127.0.0.1:1234

  # Read timeout for websocket responses (ms)
  timeout_ms: 500

  # Safety clamps (dB). The daemon will clamp target volume to [min_db, max_db].
  min_db: -65.0
  max_db: 0.0

  # Daemon update loop frequency (Hz). Higher = more responsive but more WS traffic.
  update_hz: 30
```

### Configuration keys

- **ws_url**: CamillaDSP WebSocket URL (default: `ws://127.0.0.1:1234`)
- **timeout_ms**: Read timeout for WebSocket responses in milliseconds (default: `500`)
- **min_db**: Lower clamp for volume in dB (default: `-65.0`)
- **max_db**: Upper clamp for volume in dB (default: `0.0`)
- **update_hz**: Frequency of the daemon update loop in Hz (default: `30`)

> StreamerBrainz enforces `min_db <= max_db`.

## Minimal example

Start CamillaDSP with WebSocket enabled (example port shown; adjust to your setup):

- CamillaDSP: enable WebSocket with `-p1234`
- StreamerBrainz: configure `camilladsp.ws_url` in your config to match

Example config (`~/.config/streamerbrainz/config.yaml`):

```yaml
camilladsp:
  ws_url: ws://127.0.0.1:1234
  min_db: -65.0
  max_db: 0.0
```

Then run:

```bash
streamerbrainz
```

## Troubleshooting

### StreamerBrainz can’t connect to CamillaDSP
Symptoms:
- Startup failure connecting to CamillaDSP
- Reconnection warnings/errors in logs

Checklist:
1. Verify CamillaDSP is running.
2. Verify CamillaDSP was started with WebSocket enabled (`-pPORT`).
3. Confirm `camilladsp.ws_url` in your config matches the CamillaDSP port and host:
   - `ws://127.0.0.1:1234` if local
   - `ws://<camilladsp-host>:<port>` if remote
4. If remote, confirm firewall/routing permits that connection.

### "Volume not changing"
Checklist:
1. Confirm StreamerBrainz is connected to the correct CamillaDSP instance (check `camilladsp.ws_url` in your config).
2. Ensure your `camilladsp.min_db` / `camilladsp.max_db` bounds are correct for your system.
3. Temporarily enable debug logging to confirm StreamerBrainz is sending volume updates and receiving "Ok" responses:
   ```bash
   streamerbrainz -log-level debug
   ```

### Mute toggling works, but volume ramping feels too slow/fast
This is usually update-rate/velocity tuning rather than a CamillaDSP issue.

Check these config keys:
- `camilladsp.update_hz`
- `velocity.max_db_per_sec`
- `velocity.accel_time_sec`
- `velocity.decay_tau_sec`

(These are StreamerBrainz control parameters; CamillaDSP is just the endpoint.)

## Notes
- StreamerBrainz expects to run in a trusted local network environment. Do not expose control surfaces unnecessarily.
- For a fully-documented configuration example, see: `examples/config.yaml`
- For configuration reference, run: `streamerbrainz -help`
