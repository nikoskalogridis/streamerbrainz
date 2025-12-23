# CamillaDSP integration

StreamerBrainz controls volume and mute by talking to **CamillaDSP over WebSocket**.

This page is user-facing: setup expectations, the relevant flags, and troubleshooting.

## Requirements

- CamillaDSP running with its **WebSocket server enabled** (CamillaDSP `-pPORT`).
- StreamerBrainz can reach the WebSocket URL (typically `ws://127.0.0.1:1234` when on the same host).

## What StreamerBrainz uses CamillaDSP for

- Read initial volume at startup (to synchronize internal state).
- Set volume (dB) during operation (velocity-based updates).
- Toggle mute immediately when requested.

## Relevant StreamerBrainz flags

### Connection
- `-camilladsp-ws-url`  
  CamillaDSP WebSocket URL. Default: `ws://127.0.0.1:1234`

- `-camilladsp-ws-timeout-ms`  
  Read timeout for WebSocket responses. Default: `500`

### Volume boundaries (safety/clamping)
- `-camilladsp-min-db`  
  Lower clamp for volume in dB. Default: `-65.0`

- `-camilladsp-max-db`  
  Upper clamp for volume in dB. Default: `0.0`

> StreamerBrainz enforces `min <= max`.

### Update loop
- `-camilladsp-update-hz`  
  Frequency of the daemon update loop. Default: `30`

## Minimal example

Start CamillaDSP with WebSocket enabled (example port shown; adjust to your setup):

- CamillaDSP: enable WebSocket with `-p1234`
- StreamerBrainz: point at that socket if you’re not using the default

Example StreamerBrainz invocation (useful for debugging; normally you’ll put these flags in your service unit):

- `streamerbrainz -camilladsp-ws-url ws://127.0.0.1:1234`

## Troubleshooting

### StreamerBrainz can’t connect to CamillaDSP
Symptoms:
- Startup failure connecting to CamillaDSP
- Reconnection warnings/errors in logs

Checklist:
1. Verify CamillaDSP is running.
2. Verify CamillaDSP was started with WebSocket enabled (`-pPORT`).
3. Confirm the URL matches the CamillaDSP port and host:
   - `ws://127.0.0.1:1234` if local
   - `ws://<camilladsp-host>:<port>` if remote
4. If remote, confirm firewall/routing permits that connection.

### “Volume not changing”
Checklist:
1. Confirm StreamerBrainz is connected to the correct CamillaDSP instance (`-camilladsp-ws-url`).
2. Ensure your `-camilladsp-min-db` / `-camilladsp-max-db` bounds are correct for your system.
3. Run StreamerBrainz with `-log-level debug` temporarily to confirm:
   - it is sending volume updates
   - it is receiving “Ok” responses from CamillaDSP

### Mute toggling works, but volume ramping feels too slow/fast
This is usually update-rate/velocity tuning rather than a CamillaDSP issue.

Check:
- `-camilladsp-update-hz`
- `-vel-max-db-per-sec`
- `-vel-accel-time`
- `-vel-decay-tau`

(Those are StreamerBrainz control parameters; CamillaDSP is just the endpoint.)

## Notes
- StreamerBrainz expects to run in a trusted local network environment. Do not expose control surfaces unnecessarily.
- For the authoritative list of flags and defaults, run: `streamerbrainz -help`
