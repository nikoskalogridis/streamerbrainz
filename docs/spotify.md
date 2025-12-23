# Spotify integration (librespot)

This guide explains how to integrate StreamerBrainz with Spotify Connect using **librespot**.

StreamerBrainz uses librespot's **onevent hook** mechanism: librespot runs `streamerbrainz librespot-hook` on player events, and the hook forwards those events to the running StreamerBrainz daemon via a Unix socket.

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
- StreamerBrainz configured (at `~/.config/streamerbrainz/config.yaml`)
- librespot configured with `onevent`
- Ability for librespot (the process) to execute `streamerbrainz` from `PATH` (or use an absolute path)
- The daemon must be able to create/use its IPC socket (default: `/tmp/streamerbrainz.sock`)

## Configuration

StreamerBrainz is configured via YAML at `~/.config/streamerbrainz/config.yaml`.

### IPC socket configuration

The librespot hook communicates with the daemon via a Unix socket:

```yaml
ipc:
  # Unix socket used by librespot-hook -> daemon IPC
  socket_path: /tmp/streamerbrainz.sock
```

**socket_path**: Path to the Unix socket (default: `/tmp/streamerbrainz.sock`)

## Setup

### 1) Configure StreamerBrainz

Create your config at `~/.config/streamerbrainz/config.yaml`:

```bash
mkdir -p ~/.config/streamerbrainz
cp examples/config.yaml ~/.config/streamerbrainz/config.yaml
```

The default IPC socket path (`/tmp/streamerbrainz.sock`) works for most setups.

### 2) Start StreamerBrainz daemon

Start the daemon:

```bash
streamerbrainz
```

If you want to see hook activity and forwarded events, enable debug logging:

```yaml
logging:
  level: debug
```

Or temporarily override via flag:

```bash
streamerbrainz -log-level debug
```

### 3) Configure librespot onevent hook
Configure librespot to run StreamerBrainz’s hook subcommand on events.

Example (CLI-style):

```bash
librespot --onevent "streamerbrainz librespot-hook"
```

If librespot runs under a service manager, you may prefer an absolute path:

```bash
librespot --onevent "/usr/local/bin/streamerbrainz librespot-hook"
```

### 4) (Optional) Use a custom socket path

If you need a non-default socket path, configure it in `~/.config/streamerbrainz/config.yaml`:

```yaml
ipc:
  socket_path: /path/to/custom/streamerbrainz.sock
```

Then configure librespot to use the same path:

```bash
librespot --onevent "streamerbrainz librespot-hook -ipc-socket /path/to/custom/streamerbrainz.sock"
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
- Enable debug logging to see more details (set `logging.level: debug` in config or run with `-log-level debug`).
- Confirm librespot is actually invoking the hook:
  - Use an absolute path in `--onevent`
  - Ensure the librespot service environment includes a suitable `PATH`

### Hook fails with IPC connection errors (connection refused / no such file)
The daemon isn't running, or the socket path doesn't match.

- Ensure the daemon is running.
- Ensure the daemon and hook agree on the socket path:
  - daemon config: `ipc.socket_path` in `~/.config/streamerbrainz/config.yaml`
  - hook flag: `librespot-hook -ipc-socket` (must match the config)
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
- Configuration example: `../examples/config.yaml`
- Planned features: `PLANNED.md`
