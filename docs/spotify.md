# Spotify integration (librespot)

This guide explains how to integrate StreamerBrainz with Spotify Connect using **librespot**.

StreamerBrainz uses librespot’s **onevent hook** mechanism: librespot runs `streamerbrainz librespot-hook` on player events, and the hook forwards those events to the running StreamerBrainz daemon.

## What this integration supports

### Current (implemented)
- Receives librespot events via environment variables (onevent hook)
- Forwards supported events into the StreamerBrainz daemon
- The daemon may log these events (depending on log level)

### Notes
- This is an integration mechanism, not a public API.
- The hook needs the daemon to be running, because it forwards events to the daemon over a local Unix socket.

## Requirements

- StreamerBrainz daemon installed and runnable on the same machine as librespot
- librespot configured with `onevent`
- Ability for librespot (the process) to execute `streamerbrainz` from `PATH` (or use an absolute path)
- The daemon must be able to create/use its local socket (default: `/tmp/streamerbrainz.sock`)

## Setup

### 1) Start StreamerBrainz daemon
Start the daemon normally (IR is optional for Spotify integration, but the daemon must be running):

```bash
streamerbrainz -log-level info
```

If you want to see hook activity and forwarded events:

```bash
streamerbrainz -log-level debug
```

### 2) Configure librespot onevent hook
Configure librespot to run StreamerBrainz’s hook subcommand on events.

Example (CLI-style):

```bash
librespot --onevent "streamerbrainz librespot-hook"
```

If librespot runs under a service manager, you may prefer an absolute path:

```bash
librespot --onevent "/usr/local/bin/streamerbrainz librespot-hook"
```

### 3) (Optional) Use a custom socket path
If your daemon is started with a non-default socket:

- daemon: `streamerbrainz -ipc-socket /path/to/socket ...`
- librespot hook: `streamerbrainz librespot-hook -ipc-socket /path/to/socket`

Example:

```bash
# daemon
streamerbrainz -ipc-socket /tmp/streamerbrainz.sock -log-level info

# librespot
librespot --onevent "streamerbrainz librespot-hook -ipc-socket /tmp/streamerbrainz.sock"
```

## Testing the hook (manual)

You can run the hook directly by setting the same environment variables librespot would set.

Show hook help:

```bash
streamerbrainz librespot-hook -help
```

Test a volume event:

```bash
PLAYER_EVENT=volume_changed VOLUME=32768 streamerbrainz librespot-hook -log-level debug
```

Test a playback state event:

```bash
PLAYER_EVENT=playing TRACK_ID=test POSITION_MS=1234 streamerbrainz librespot-hook -log-level debug
```

If the daemon is reachable and the event is supported, the hook should exit successfully.

## Troubleshooting

### Hook prints: `PLAYER_EVENT not set`
You ran `streamerbrainz librespot-hook` directly without librespot (or without setting env vars).

- Confirm librespot is configured with `onevent = streamerbrainz librespot-hook`
- Or use the manual testing commands above.

### librespot runs, but nothing appears in StreamerBrainz logs
- Start the daemon with `-log-level debug` to see more details.
- Confirm librespot is actually invoking the hook:
  - Use an absolute path in `--onevent`
  - Ensure the librespot service environment includes a suitable `PATH`

### Hook fails with IPC connection errors (connection refused / no such file)
The daemon isn’t running, or the socket path doesn’t match.

- Ensure the daemon is running.
- Ensure the daemon and hook agree on the socket path:
  - daemon flag: `-ipc-socket`
  - hook flag: `librespot-hook -ipc-socket`
- Check that the socket exists:
  ```bash
  ls -l /tmp/streamerbrainz.sock
  ```

### Permissions problems writing/connecting to the socket
- The daemon creates the socket at the configured path.
- Ensure the librespot process user can access that path (default `/tmp` is usually fine).

### Some librespot events seem ignored
Not all librespot event types are handled yet. Unsupported events are intentionally ignored by the hook.

If you want support for a specific librespot event, capture:
- the `PLAYER_EVENT` value
- any related env vars librespot provides for that event
and then add translation logic in the librespot hook implementation.

## Reference: librespot events StreamerBrainz currently understands

StreamerBrainz currently translates these `PLAYER_EVENT` values:

- `session_connected`
- `session_disconnected`
- `volume_changed`
- `track_changed`
- `playing`, `paused`, `stopped`, `seeked`, `position_correction`

Other librespot events may exist and may be ignored for now.

## See also
- Main README: `../README.md`
- Planned features: `PLANNED.md`
